package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	cron "github.com/robfig/cron/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	// Yahoo Finance client
	"github.com/piquette/finance-go/chart"
	"github.com/piquette/finance-go/datetime"
)

// === Constants & Env Vars ===
const (
	spreadsheetID  = "1KyAGZwzW7spZeBf6Tmz-xKzNLLwOBruNMDsAzMvpwaA" // your Google Sheet ID
	symbolsRange   = "Nifty50_Data!A2:A"
	orderBookRange = "Order_Book!A2:F"
)

// Env-derived configuration
var (
	dhanClientID       = os.Getenv("DHAN_CLIENT_ID")
	dhanAccessToken    = os.Getenv("DHAN_ACCESS_TOKEN")
	tgBotToken         = os.Getenv("TELEGRAM_BOT_TOKEN")
	tgChatID, _        = strconv.ParseInt(os.Getenv("TELEGRAM_CHAT_ID"), 10, 64)
	maxNewPositions, _ = strconv.Atoi(os.Getenv("MAX_NEW_POSITIONS"))
	avgThreshold, _    = strconv.ParseFloat(os.Getenv("AVERAGING_THRESHOLD"), 64)
)

// Action represents a trading decision
type Action struct {
	Type        string // "new_position" or "average_down"
	Symbol      string
	Lots        int
	Price       float64
	Reason      string
	AfterMarket bool
}

// Broker defines methods for brokerage operations
type Broker interface {
	GetHoldings(ctx context.Context) (map[string]float64, error)
	PlaceOrder(ctx context.Context, a Action) (string, error)
}

// DhanClient implements Broker for Dhan API
type DhanClient struct {
	ClientID string
	Token    string
	HTTP     *http.Client
	HoldURL  string
	OrderURL string
}

// NewDhanClient constructs a Dhan broker client
func NewDhanClient() *DhanClient {
	return &DhanClient{
		ClientID: dhanClientID,
		Token:    dhanAccessToken,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
		HoldURL:  "https://api.dhan.co/v2/holdings",
		OrderURL: "https://api.dhan.co/v2/orders",
	}
}

// GetHoldings loads current holdings and avg cost
func (d *DhanClient) GetHoldings(ctx context.Context) (map[string]float64, error) {
	body := map[string]string{"dhanClientId": d.ClientID}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", d.HoldURL, bytes.NewBuffer(b))
	req.Header.Set("access-token", d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Data []struct {
			TradingSymbol string  `json:"tradingSymbol"`
			AvgCostPrice  float64 `json:"avgCostPrice"`
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	holdMap := make(map[string]float64)
	for _, h := range data.Data {
		holdMap[h.TradingSymbol] = h.AvgCostPrice
	}
	return holdMap, nil
}

// PlaceOrder sends a BUY order and returns status string
func (d *DhanClient) PlaceOrder(ctx context.Context, a Action) (string, error) {
	ord := map[string]interface{}{
		"dhanClientId":     d.ClientID,
		"transactionType":  "BUY",
		"exchangeSegment":  "NSE_EQ",
		"productType":      "CNC",
		"orderType":        "MARKET",
		"validity":         "DAY",
		"symbol":           a.Symbol,
		"quantity":         a.Lots,
		"afterMarketOrder": a.AfterMarket,
	}
	b, _ := json.Marshal(ord)
	req, _ := http.NewRequestWithContext(ctx, "POST", d.OrderURL, bytes.NewBuffer(b))
	req.Header.Set("access-token", d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Status, nil
}

var (
	srv                *sheets.Service
	bot                *tgbotapi.BotAPI
	loc                *time.Location
	awaitingAcceptance bool
	pendingActions     []Action
	broker             Broker
)

func main() {
	// Load IST timezone
	loc, _ = time.LoadLocation("Asia/Kolkata")

	// Initialize Google Sheets client
	var err error
	srv, err = sheets.NewService(context.Background(), option.WithScopes(sheets.SpreadsheetsScope))
	if err != nil {
		log.Fatalf("Sheets init error: %v", err)
	}

	// Initialize Telegram bot
	bot, err = tgbotapi.NewBotAPI(tgBotToken)
	if err != nil {
		log.Fatalf("Telegram init error: %v", err)
	}
	go listenTelegram()

	// Set Dhan as broker
	broker = NewDhanClient()

	// Schedule daily run at 15:20 IST, Mon–Fri
	c := cron.New(cron.WithLocation(loc))
	c.AddFunc("20 15 * * 1-5", runStrategy)
	c.Start()

	// Block forever
	select {}
}

// runStrategy executes the workflow
func runStrategy() {
	ctx := context.Background()
	if !isTradingDay() {
		log.Println("Market closed; skipping")
		return
	}

	// 1) Load symbols
	syms, err := loadSymbols()
	if err != nil {
		log.Println("loadSymbols error:", err)
		return
	}

	// 2) Compute deviations and select top-5
	type cand struct {
		sym        string
		dev, price float64
	}
	var cands []cand
	for _, s := range syms {
		dev, price, err := fetchDeviation(s)
		if err != nil {
			log.Printf("skip %s: %v", s, err)
			continue
		}
		cands = append(cands, cand{s, dev, price})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].dev < cands[j].dev })
	if len(cands) > 5 {
		cands = cands[:5]
	}

	// 3) Load holdings
	holdMap, err := broker.GetHoldings(ctx)
	if err != nil {
		log.Println("GetHoldings error:", err)
		return
	}

	// 4) Determine actions
	// Convert cands to the expected type
	var candsConverted []struct {
		sym        string
		dev, price float64
	}
	for _, c := range cands {
		candsConverted = append(candsConverted, struct {
			sym        string
			dev, price float64
		}{c.sym, c.dev, c.price})
	}
	actions := determineActions(candsConverted, holdMap)
	if len(actions) == 0 {
		log.Println("No actions needed")
		return
	}

	// 5) Notify & await accept
	pendingActions = actions
	awaitingAcceptance = true
	notifyTelegram(actions)
}

// isTradingDay returns false on Sat/Sun and fixed holidays
func isTradingDay() bool {
	t := time.Now().In(loc)
	wd := t.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	hol := map[string]bool{"2025-01-26": true, "2025-08-15": true}
	return !hol[t.Format("2006-01-02")]
}

// loadSymbols reads the list of symbols from sheet
func loadSymbols() ([]string, error) {
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, symbolsRange).Do()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range resp.Values {
		if len(row) > 0 {
			out = append(out, fmt.Sprint(row[0]))
		}
	}
	return out, nil
}

// fetchDeviation uses piquette/finance-go to compute SMA20 deviation
func fetchDeviation(sym string) (float64, float64, error) {
	et := time.Now()
	st := et.AddDate(0, -1, 0)
	end := datetime.New(&et)
	start := datetime.New(&st) // 1 month ago

	params := &chart.Params{
		Symbol:   sym + ".NS",
		Start:    start,
		End:      end,
		Interval: datetime.OneDay,
	}
	iter := chart.Get(params)
	var closes []float64
	for iter.Next() {
		bar := iter.Bar()
		closeFloat, _ := bar.Close.Float64()
		closes = append(closes, closeFloat)
	}
	if err := iter.Err(); err != nil {
		return 0, 0, err
	}
	if len(closes) < 20 {
		return 0, 0, fmt.Errorf("insufficient data for %s", sym)
	}
	last := closes[len(closes)-1]
	sum := 0.0
	for _, v := range closes[len(closes)-20:] {
		sum += v
	}
	sma := sum / 20.0
	dev := (last - sma) / sma
	return dev, last, nil
}

// determineActions applies entry/averaging logic
func determineActions(cands []struct {
	sym        string
	dev, price float64
}, hold map[string]float64) []Action {
	held := 0
	for _, c := range cands {
		if _, ok := hold[c.sym]; ok {
			held++
		}
	}
	var acts []Action
	// Entry mode
	if held != len(cands) {
		slots := maxNewPositions
		for _, c := range cands {
			if slots == 0 {
				break
			}
			if _, ok := hold[c.sym]; ok {
				continue
			}
			acts = append(acts, Action{"new_position", c.sym, 1, c.price,
				fmt.Sprintf("%.2f%% below 20DMA", c.dev*100), false})
			slots--
		}
	} else {
		// Averaging mode
		worstSym := ""
		worstDrop := avgThreshold
		for _, c := range cands {
			avg := hold[c.sym]
			drop := (c.price - avg) / avg
			if drop <= worstDrop {
				worstDrop = drop
				worstSym = c.sym
			}
		}
		if worstSym != "" {
			acts = append(acts, Action{"average_down", worstSym, 1, hold[worstSym],
				fmt.Sprintf("down %.2f%%", worstDrop*100), false})
		}
	}
	return acts
}

// notifyTelegram sends proposed actions with Accept button
func notifyTelegram(actions []Action) {
	text := "Proposed actions:\n"
	for _, a := range actions {
		text += fmt.Sprintf("%s %d @ %.2f: %s\n", a.Symbol, a.Lots, a.Price, a.Reason)
	}
	msg := tgbotapi.NewMessage(tgChatID, text)
	btn := tgbotapi.NewInlineKeyboardButtonData("Accept & Place", "accept")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btn))
	bot.Send(msg)
}

// listenTelegram handles the "accept" callback and triggers order placement
func listenTelegram() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	for upd := range bot.GetUpdatesChan(u) {
		if upd.CallbackQuery != nil && upd.CallbackQuery.Data == "accept" && awaitingAcceptance {
			awaitingAcceptance = false
			bot.Request(tgbotapi.NewCallback(upd.CallbackQuery.ID, "Placing orders..."))
			placeOrders()
		}
	}
}

// placeOrders executes each Action via Broker and logs to sheet
func placeOrders() {
	ctx := context.Background()
	isAMO := func() bool {
		n := time.Now().In(loc)
		return n.Hour() > 15 || (n.Hour() == 15 && n.Minute() >= 30)
	}()

	for _, a := range pendingActions {
		a.AfterMarket = isAMO
		// send to broker
		status, err := broker.PlaceOrder(ctx, a)
		if err != nil {
			status = err.Error()
		}
		// notify user
		bot.Send(tgbotapi.NewMessage(tgChatID, fmt.Sprintf("Order %s: %s", a.Symbol, status)))

		// append to sheet
		row := []interface{}{time.Now().Format("2006-01-02T15:04:05"), a.Symbol, strings.ToUpper(a.Type), a.Lots, a.Price, status}
		r := &sheets.ValueRange{Values: [][]interface{}{row}}
		srv.Spreadsheets.Values.Append(spreadsheetID, orderBookRange, r).
			ValueInputOption("RAW").Do()
	}
}
