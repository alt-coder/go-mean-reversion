package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alt-coder/go-mean-reversion/pkg/models"
	"github.com/alt-coder/go-mean-reversion/pkg/utils/csv"
	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	cron "github.com/robfig/cron/v3"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// === Constants & Env Vars ===
const (
	spreadsheetID   = "1KyAGZwzW7spZeBf6Tmz-xKzNLLwOBruNMDsAzMvpwaA" // your Google Sheet ID
	symbolsRange    = "Nifty50_Data!A2:A"
	orderBookRange  = "Order_Book!A2:F"
	topBuyRange     = "Dashboard!E2:G6" // Top 5 candidates with symbol, % drop, and live price
	portfolioRange  = "Portfolio!A2:F"  // Portfolio data: Symbol, Quantity, Avg Price, Current Price, P&L, % Change
	profitThreshold = 5.0               // Sell if profit > 5%
)

// Env-derived configuration
var (
	dhanClientID       = os.Getenv("DHAN_CLIENT_ID")
	dhanAccessToken    = os.Getenv("DHAN_ACCESS_TOKEN")
	tgBotToken         = os.Getenv("TELEGRAM_BOT_TOKEN")
	tgChatID, _        = strconv.ParseInt(os.Getenv("TELEGRAM_CHAT_ID"), 10, 64)
	maxNewPositions, _ = strconv.Atoi(os.Getenv("MAX_NEW_POSITIONS"))
	avgThreshold, _    = strconv.ParseFloat(os.Getenv("AVERAGING_THRESHOLD"), 64)

	// Google Service Account credentials from env
	googleClientEmail = os.Getenv("GOOGLE_CLIENT_EMAIL")
	googlePrivateKey  = os.Getenv("GOOGLE_PRIVATE_KEY")
	googleProjectID   = os.Getenv("GOOGLE_PROJECT_ID")

	// Dhan sheet Path
	csvPath = os.Getenv("CSV_PATH")

	// nifty50 Candidates - will be loaded dynamically from Google Sheets
	nifty50 = make(map[string]struct{}, 50)
)

var (
	cache        map[string]string
	lastLoadtime time.Time
)

// GetSecurityID returns the SEM_SMST_SECURITY_ID for the given trading symbol.
// csvPath should point to your TSV/CSV file (tab-delimited in your example).
func GetSecurityID(symbol string) (string, error) {
	// Load cache if not yet loaded or if more than 5 minutes have passed
	if lastLoadtime.IsZero() || time.Since(lastLoadtime) > 5*time.Minute {
		var err error
		cache, err = csv.LoadCache(csvPath, nifty50)
		if err != nil {
			return "", fmt.Errorf("failed to load cache: %v", err)
		}
		lastLoadtime = time.Now()
		log.Printf("Cache loaded/refreshed at %s", lastLoadtime.Format("15:04:05"))
	}

	if id, ok := cache[symbol]; ok {
		return id, nil
	}
	return "", fmt.Errorf("symbol %q not found in cache", symbol)
}

// YahooFinanceResponse represents the Yahoo Finance API response
type YahooFinanceResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol             string  `json:"symbol"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Close []float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
	} `json:"chart"`
}

// Broker defines methods for brokerage operations
type Broker interface {
	GetHoldings(ctx context.Context) (map[string]float64, error)
	PlaceOrder(ctx context.Context, a models.Action) (string, error)
}

// DhanClient implements Broker for Dhan API
type DhanClient struct {
	ClientID string
	Token    string
	Client   *resty.Client
	HoldURL  string
	OrderURL string
}

// NewDhanClient constructs a Dhan broker client
func NewDhanClient() *DhanClient {
	client := resty.New()
	client.SetTimeout(10 * time.Second)

	return &DhanClient{
		ClientID: dhanClientID,
		Token:    dhanAccessToken,
		Client:   client,
		HoldURL:  "https://api.dhan.co/v2/holdings",
		OrderURL: "https://api.dhan.co/v2/orders",
	}
}

// GetHoldings loads current holdings and avg cost
func (d *DhanClient) GetHoldings(ctx context.Context) (map[string]float64, error) {
	var holdings []struct {
		Exchange      string  `json:"exchange"`
		TradingSymbol string  `json:"tradingSymbol"`
		SecurityID    string  `json:"securityId"`
		ISIN          string  `json:"isin"`
		TotalQty      int     `json:"totalQty"`
		DpQty         int     `json:"dpQty"`
		T1Qty         int     `json:"t1Qty"`
		AvailableQty  int     `json:"availableQty"`
		CollateralQty int     `json:"collateralQty"`
		AvgCostPrice  float64 `json:"avgCostPrice"`
	}

	resp, err := d.Client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("access-token", d.Token).
		SetResult(&holdings).
		Get(d.HoldURL)

	if err != nil {
		return nil, fmt.Errorf("failed to get holdings: %v", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	holdMap := make(map[string]float64)
	for _, h := range holdings {
		holdMap[h.TradingSymbol] = h.AvgCostPrice
	}
	return holdMap, nil
}

// PlaceOrder sends a BUY order and returns status string
func (d *DhanClient) PlaceOrder(ctx context.Context, a models.Action) (string, error) {
	// Get Dhan security ID for the trading symbol
	securityId, err := GetSecurityID(a.Symbol)
	if err != nil {
		log.Print(err)
		return "", fmt.Errorf("failed to get security ID: %v", err)
	}

	orderPayload := map[string]interface{}{
		"dhanClientId":      d.ClientID,
		"correlationId":     fmt.Sprintf("bot_%d", time.Now().Unix()),
		"transactionType":   "BUY",
		"exchangeSegment":   "NSE_EQ",
		"productType":       "CNC",
		"orderType":         "MARKET",
		"validity":          "DAY",
		"securityId":        securityId,
		"quantity":          fmt.Sprintf("%d", a.Lots),
		"disclosedQuantity": "",
		"price":             "",
		"triggerPrice":      "",
		"afterMarketOrder":  a.AfterMarket,
		"amoTime":           "",
		"boProfitValue":     "",
		"boStopLossValue":   "",
	}

	var orderResp struct {
		OrderID     string `json:"orderId"`
		OrderStatus string `json:"orderStatus"`
	}

	resp, err := d.Client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("access-token", d.Token).
		SetBody(orderPayload).
		SetResult(&orderResp).
		Post(d.OrderURL)

	if err != nil {
		return "", fmt.Errorf("failed to place order: %v", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return fmt.Sprintf("OrderID: %s, Status: %s", orderResp.OrderID, orderResp.OrderStatus), nil
}

var (
	srv             *sheets.Service
	bot             *tgbotapi.BotAPI
	loc             *time.Location
	pendingActions  map[string]models.Action // Map of action ID to action
	acceptedActions []models.Action          // Actions accepted by user
	broker          Broker
)

func main() {
	// Load IST timezone
	loc, _ = time.LoadLocation("Asia/Kolkata")

	// Initialize pending actions map
	pendingActions = make(map[string]models.Action)

	// Initialize Google Sheets client
	var err error
	srv, err = createSheetsService()
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
	_ = cron.New(cron.WithLocation(loc))
	// c.AddFunc("20 15 * * 1-5", runStrategy)
	// c.Start()
	runStrategy()

	// Block forever
	select {}
}

// runStrategy executes the workflow
func runStrategy() {
	if !isTradingDay() {
		log.Println("Market closed; skipping")
		return
	}

	// 1) Try to load top 5 candidates from Yahoo Finance first
	candidates, err := loadTopCandidatesFromYahoo()
	if err != nil {
		log.Printf("Yahoo Finance failed: %v, falling back to Google Sheets", err)
		// Fallback to Google Sheets
		candidates, err = loadTopCandidatesFromSheet()
		if err != nil {
			log.Println("Both Yahoo Finance and Google Sheets failed:", err)
			return
		}
		log.Printf("Loaded %d candidates from Google Sheets (fallback)", len(candidates))
	} else {
		log.Printf("Loaded %d candidates from Yahoo Finance", len(candidates))
	}

	for i, c := range candidates {
		log.Printf("  %d. %s: %.2f%% deviation from SMA20 at ₹%.2f", i+1, c.Symbol, c.Deviation*100, c.Price)
	}

	// 2) Load current holdings from Dhan
	holdMap, err := broker.GetHoldings(context.Background())
	if err != nil {
		log.Println("GetHoldings error:", err)
		return
	}

	// 3) Load portfolio for sell signals
	portfolio, err := loadPortfolioFromSheet()
	if err != nil {
		log.Println("Failed to load portfolio:", err)
		// Continue without sell signals
	}

	// 4) Determine buy actions based on sheet data and holdings
	buyActions := determineActionsFromSheet(candidates, holdMap)

	// 5) Determine sell actions based on portfolio
	sellActions := determineSellActions(portfolio)

	// Combine all actions
	allActions := append(buyActions, sellActions...)

	if len(allActions) == 0 {
		log.Println("No actions needed based on current analysis")
		return
	}

	log.Printf("Strategy determined %d actions:", len(allActions))
	for i, action := range allActions {
		log.Printf("  %d. %s: %s %d lots - %s", i+1, action.Symbol, action.Type, action.Lots, action.Reason)
	}

	// 6) Store actions and notify via Telegram
	for _, action := range allActions {
		pendingActions[action.ID] = action
	}
	notifyTelegram(allActions)
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

// loadNifty50SymbolsFromSheet loads the Nifty 50 stock symbols from Google Sheets
func loadNifty50SymbolsFromSheet() error {
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, symbolsRange).Do()
	if err != nil {
		return fmt.Errorf("failed to read Nifty 50 symbols: %v", err)
	}

	// Clear existing symbols
	nifty50 = make(map[string]struct{})

	// Load symbols from sheet
	for _, row := range resp.Values {
		if len(row) > 0 {
			symbol := strings.TrimSpace(fmt.Sprint(row[0]))
			if symbol != "" {
				nifty50[symbol] = struct{}{}
			}
		}
	}

	log.Printf("Loaded %d Nifty 50 symbols from Google Sheets", len(nifty50))
	return nil
}

// loadPortfolioFromSheet loads portfolio data from Google Sheets
func loadPortfolioFromSheet() ([]models.PortfolioStock, error) {
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, portfolioRange).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read portfolio: %v", err)
	}

	var portfolio []models.PortfolioStock
	for _, row := range resp.Values {
		if len(row) >= 6 {
			symbol := strings.TrimSpace(fmt.Sprint(row[0]))
			if symbol == "" {
				continue
			}

			quantity, _ := strconv.Atoi(fmt.Sprint(row[1]))
			avgPrice, _ := strconv.ParseFloat(fmt.Sprint(row[2]), 64)
			currentPrice, _ := strconv.ParseFloat(fmt.Sprint(row[3]), 64)
			profitLoss, _ := strconv.ParseFloat(fmt.Sprint(row[4]), 64)
			percentChange, _ := strconv.ParseFloat(fmt.Sprint(row[5]), 64)

			portfolio = append(portfolio, models.PortfolioStock{
				Symbol:        symbol,
				Quantity:      quantity,
				AvgPrice:      avgPrice,
				CurrentPrice:  currentPrice,
				ProfitLoss:    profitLoss,
				PercentChange: percentChange,
			})
		}
	}

	log.Printf("Loaded %d stocks from portfolio", len(portfolio))
	return portfolio, nil
}

// loadTopCandidatesFromYahoo fetches data from Yahoo Finance and finds top 5 most fallen stocks from 20 SMA
func loadTopCandidatesFromYahoo() ([]models.Candidate, error) {
	// First, load the Nifty 50 symbols from Google Sheets
	if err := loadNifty50SymbolsFromSheet(); err != nil {
		return nil, fmt.Errorf("failed to load Nifty 50 symbols: %v", err)
	}

	log.Printf("Fetching Yahoo Finance data for %d stocks...", len(nifty50))
	client := resty.New()
	client.SetTimeout(15 * time.Second)
	client.SetRetryCount(2)
	client.SetRetryWaitTime(1 * time.Second)
	client.SetRetryMaxWaitTime(3 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	client.SetHeader("Accept", "application/json")
	client.SetHeader("Accept-Language", "en-US,en;q=0.9")

	var allCandidates []models.Candidate
	now := time.Now()
	period1 := now.AddDate(0, 0, -30).Unix() // 30 days ago
	period2 := now.Unix()

	successCount := 0
	errorCount := 0

	// Process each Nifty 50 stock
	for symbol := range nifty50 {
		url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s.NS?period1=%d&period2=%d&interval=1d",
			symbol, period1, period2)

		var response YahooFinanceResponse
		resp, err := client.R().
			SetResult(&response).
			Get(url)

		if err != nil {
			log.Printf("Network error for %s: %v", symbol, err)
			errorCount++
			continue
		}

		if resp.StatusCode() != 200 {
			log.Printf("HTTP error for %s: status %d, body: %s", symbol, resp.StatusCode(), string(resp.Body()))
			errorCount++
			continue
		}

		successCount++

		if len(response.Chart.Result) == 0 || len(response.Chart.Result[0].Indicators.Quote) == 0 {
			log.Printf("No data available for %s", symbol)
			continue
		}

		result := response.Chart.Result[0]
		closes := result.Indicators.Quote[0].Close
		currentPrice := result.Meta.RegularMarketPrice

		// Calculate 20-day SMA
		if len(closes) < 20 {
			log.Printf("Insufficient data for %s (only %d days)", symbol, len(closes))
			continue
		}

		// Get last 20 closes (excluding today if current price is different)
		var smaCloses []float64
		for i := len(closes) - 20; i < len(closes); i++ {
			if !math.IsNaN(closes[i]) {
				smaCloses = append(smaCloses, closes[i])
			}
		}

		if len(smaCloses) < 20 {
			log.Printf("Insufficient valid data for %s", symbol)
			continue
		}

		// Calculate SMA20
		sum := 0.0
		for _, price := range smaCloses {
			sum += price
		}
		sma20 := sum / float64(len(smaCloses))

		// Calculate deviation from SMA20
		deviation := (currentPrice - sma20) / sma20

		// Only consider stocks that are below SMA20 (negative deviation)
		if deviation < 0 {
			allCandidates = append(allCandidates, models.Candidate{
				Symbol:    symbol,
				Deviation: deviation,
				Price:     currentPrice,
				SMA20:     sma20,
			})
		}

		// Delay to avoid rate limiting and reduce server load
		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("Yahoo Finance API calls completed: %d successful, %d failed", successCount, errorCount)

	if len(allCandidates) == 0 {
		return nil, fmt.Errorf("no stocks found below their 20 SMA (processed %d stocks successfully)", successCount)
	}

	// Sort by deviation (most negative first - most fallen)
	sort.Slice(allCandidates, func(i, j int) bool {
		return allCandidates[i].Deviation < allCandidates[j].Deviation
	})

	// Return top 5 most fallen stocks
	maxCandidates := 5
	if len(allCandidates) < maxCandidates {
		maxCandidates = len(allCandidates)
	}

	return allCandidates[:maxCandidates], nil
}

// loadTopCandidatesFromSheet loads the top 5 candidates directly from Google Sheets (fallback)
func loadTopCandidatesFromSheet() ([]models.Candidate, error) {
	// Read from the "Dashboard" sheet - columns E (Symbol), F (% Drop), G (Live Price)
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, topBuyRange).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to read top candidates: %v", err)
	}

	var candidates []models.Candidate
	for _, row := range resp.Values {
		if len(row) >= 3 {
			symbol := fmt.Sprint(row[0])

			// Parse the deviation percentage
			devStr := fmt.Sprint(row[1])
			deviation, err := strconv.ParseFloat(devStr, 64)
			if err != nil {
				log.Printf("Warning: could not parse deviation for %s: %v", symbol, err)
				continue
			}

			// Parse the live price
			priceStr := fmt.Sprint(row[2])
			price, err := strconv.ParseFloat(priceStr, 64)
			if err != nil {
				log.Printf("Warning: could not parse price for %s: %v", symbol, err)
				continue
			}

			// Convert percentage to decimal (e.g., -6.78 -> -0.0678)
			deviation = deviation / 100.0

			candidates = append(candidates, models.Candidate{
				Symbol:    symbol,
				Deviation: deviation,
				Price:     price,
				SMA20:     0, // Not available from sheets
			})
		}
	}

	return candidates, nil
}

// determineSellActions determines sell actions for profitable stocks
func determineSellActions(portfolio []models.PortfolioStock) []models.Action {
	var sellActions []models.Action

	for _, stock := range portfolio {
		// Only suggest sell if profit percentage > threshold
		if stock.PercentChange > profitThreshold {
			actionID := fmt.Sprintf("sell_%s_%d", stock.Symbol, time.Now().Unix())
			sellActions = append(sellActions, models.Action{
				ID:          actionID,
				Type:        "sell",
				Symbol:      stock.Symbol,
				Lots:        stock.Quantity,
				Price:       stock.CurrentPrice,
				Reason:      fmt.Sprintf("%.2f%% profit (₹%.2f → ₹%.2f)", stock.PercentChange, stock.AvgPrice, stock.CurrentPrice),
				AfterMarket: false,
			})
		}
	}

	log.Printf("Found %d sell opportunities", len(sellActions))
	return sellActions
}

// determineActionsFromSheet determines trading actions based on sheet data and current holdings
func determineActionsFromSheet(candidates []models.Candidate, holdMap map[string]float64) []models.Action {
	var actions []models.Action
	maxActions := 2 // Maximum 2 actions per day (can be mix of entry and averaging)

	// First, collect potential entry actions (stocks we don't hold)
	var entryActions []models.Action
	for _, c := range candidates {
		if _, exists := holdMap[c.Symbol]; !exists {
			reason := fmt.Sprintf("%.2f%% below SMA20 at ₹%.2f", c.Deviation*100, c.Price)
			if c.SMA20 > 0 {
				reason = fmt.Sprintf("%.2f%% below SMA20 (₹%.2f) at ₹%.2f", c.Deviation*100, c.SMA20, c.Price)
			}

			actionID := fmt.Sprintf("buy_%s_%d", c.Symbol, time.Now().Unix())
			entryActions = append(entryActions, models.Action{
				ID:          actionID,
				Type:        "new_position",
				Symbol:      c.Symbol,
				Lots:        1,
				Price:       c.Price,
				Reason:      reason,
				AfterMarket: false,
			})
		}
	}

	// Second, find potential averaging action (worst performer we hold)
	var avgAction *models.Action
	worstSymbol := ""
	worstDrop := avgThreshold // Only average if drop is worse than threshold
	worstPrice := 0.0

	for _, c := range candidates {
		avgPrice, exists := holdMap[c.Symbol]
		if !exists {
			continue
		}

		// Calculate actual drop from our average price to current live price
		actualDrop := (c.Price - avgPrice) / avgPrice

		// Use the worse of deviation or actual drop for averaging decision
		dropToUse := c.Deviation
		if actualDrop < c.Deviation {
			dropToUse = actualDrop
		}

		if dropToUse <= worstDrop {
			worstDrop = dropToUse
			worstSymbol = c.Symbol
			worstPrice = c.Price
		}
	}

	if worstSymbol != "" {
		actionID := fmt.Sprintf("avg_%s_%d", worstSymbol, time.Now().Unix())
		avgAction = &models.Action{
			ID:          actionID,
			Type:        "average_down",
			Symbol:      worstSymbol,
			Lots:        1,
			Price:       worstPrice,
			Reason:      fmt.Sprintf("averaging down at ₹%.2f (%.2f%% drop)", worstPrice, worstDrop*100),
			AfterMarket: false,
		}
	}

	// Now prioritize actions up to maxActions limit
	// Priority: Entry actions first, then averaging
	for i := 0; i < len(entryActions) && len(actions) < maxActions; i++ {
		actions = append(actions, entryActions[i])
	}

	// Add averaging action if we still have slots and it exists
	if len(actions) < maxActions && avgAction != nil {
		actions = append(actions, *avgAction)
	}

	log.Printf("Determined %d actions (max %d allowed)", len(actions), maxActions)
	return actions
}

// getEnvWithDefault returns environment variable value or default if not set
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// createSheetsService creates Google Sheets service from environment variables
func createSheetsService() (*sheets.Service, error) {
	ctx := context.Background()

	// Try environment variables first
	if googleClientEmail != "" && googlePrivateKey != "" {
		return createSheetsServiceFromEnv(ctx)
	}

	// Fallback to credentials file or default credentials
	return sheets.NewService(ctx, option.WithScopes(sheets.SpreadsheetsScope))
}

// createSheetsServiceFromEnv creates service using env variables
func createSheetsServiceFromEnv(ctx context.Context) (*sheets.Service, error) {
	// Parse the private key
	privateKeyData := strings.ReplaceAll(googlePrivateKey, "\\n", "\n")

	// Create JWT config with raw private key bytes
	config := &jwt.Config{
		Email:      googleClientEmail,
		PrivateKey: []byte(privateKeyData),
		Scopes:     []string{sheets.SpreadsheetsScope},
		TokenURL:   google.JWTTokenURL,
	}

	if googleProjectID != "" {
		config.Subject = googleProjectID
	}

	// Create HTTP client
	client := config.Client(ctx)

	// Create Sheets service
	return sheets.NewService(ctx, option.WithHTTPClient(client))
}

// notifyTelegram sends individual messages for each action with separate Accept/Reject buttons
func notifyTelegram(actions []models.Action) {
	if len(actions) == 0 {
		return
	}

	// Send summary message first
	summaryText := fmt.Sprintf("📊 Trading Signals Summary: %d actions found\n\n", len(actions))

	buyCount := 0
	sellCount := 0
	avgCount := 0

	for _, a := range actions {
		switch a.Type {
		case "new_position":
			buyCount++
		case "sell":
			sellCount++
		case "average_down":
			avgCount++
		}
	}

	if buyCount > 0 {
		summaryText += fmt.Sprintf("🟢 Buy Signals: %d\n", buyCount)
	}
	if sellCount > 0 {
		summaryText += fmt.Sprintf("🔴 Sell Signals: %d\n", sellCount)
	}
	if avgCount > 0 {
		summaryText += fmt.Sprintf("🟡 Average Down: %d\n", avgCount)
	}

	summaryMsg := tgbotapi.NewMessage(tgChatID, summaryText)
	bot.Send(summaryMsg)

	// Send individual messages for each action
	for i, a := range actions {
		var emoji, actionType string
		switch a.Type {
		case "new_position":
			emoji = "🟢"
			actionType = "BUY"
		case "sell":
			emoji = "🔴"
			actionType = "SELL"
		case "average_down":
			emoji = "🟡"
			actionType = "AVERAGE"
		}

		text := fmt.Sprintf("%s %s Signal #%d\n\n", emoji, actionType, i+1)
		text += fmt.Sprintf("🏷️ Symbol: %s\n", a.Symbol)
		text += fmt.Sprintf("💰 Price: ₹%.2f\n", a.Price)
		text += fmt.Sprintf("📦 Quantity: %d lots\n", a.Lots)
		text += fmt.Sprintf("📝 Reason: %s\n\n", a.Reason)
		text += "⚠️ Please review and decide:"

		msg := tgbotapi.NewMessage(tgChatID, text)
		acceptBtn := tgbotapi.NewInlineKeyboardButtonData("✅ Accept", fmt.Sprintf("accept_%s", a.ID))
		rejectBtn := tgbotapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("reject_%s", a.ID))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(acceptBtn, rejectBtn),
		)
		bot.Send(msg)

		// Small delay between messages to avoid flooding
		time.Sleep(100 * time.Millisecond)
	}
}

// listenTelegram handles individual accept/reject callbacks for each action
func listenTelegram() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	for upd := range bot.GetUpdatesChan(u) {
		if upd.CallbackQuery != nil {
			data := upd.CallbackQuery.Data

			if strings.HasPrefix(data, "accept_") {
				actionID := strings.TrimPrefix(data, "accept_")
				if action, exists := pendingActions[actionID]; exists {
					// Add to accepted actions
					acceptedActions = append(acceptedActions, action)
					delete(pendingActions, actionID)

					bot.Request(tgbotapi.NewCallback(upd.CallbackQuery.ID, "✅ Accepted! Placing order..."))

					// Place this individual order
					placeIndividualOrder(action)
				}
			} else if strings.HasPrefix(data, "reject_") {
				actionID := strings.TrimPrefix(data, "reject_")
				if action, exists := pendingActions[actionID]; exists {
					delete(pendingActions, actionID)

					bot.Request(tgbotapi.NewCallback(upd.CallbackQuery.ID, "❌ Rejected"))

					rejectMsg := fmt.Sprintf("🚫 %s %s order rejected", action.Symbol, strings.ToUpper(string(action.Type)))
					bot.Send(tgbotapi.NewMessage(tgChatID, rejectMsg))
					log.Printf("models.Action rejected by user: %s %s", action.Symbol, action.Type)
				}
			}
		}
	}
}

// placeIndividualOrder executes a single models.Action via Broker and logs to sheet
func placeIndividualOrder(action models.Action) {
	ctx := context.Background()
	isAMO := func() bool {
		n := time.Now().In(loc)
		return n.Hour() > 15 || (n.Hour() == 15 && n.Minute() >= 30)
	}()

	action.AfterMarket = isAMO

	// Send to broker (only for buy/average actions, sell needs different handling)
	var status string
	var err error

	if action.Type == "sell" {
		// For sell orders, we need to modify the order type
		status, err = placeSellOrder(ctx, action)
	} else {
		// For buy/average orders
		status, err = broker.PlaceOrder(ctx, action)
	}

	if err != nil {
		status = fmt.Sprintf("ERROR: %s", err.Error())
	}

	// Notify user with detailed status
	var orderType, statusEmoji string
	switch action.Type {
	case "new_position":
		orderType = "📈 New Position"
	case "average_down":
		orderType = "📊 Average Down"
	case "sell":
		orderType = "📉 Sell"
	}

	if err != nil {
		statusEmoji = "❌"
	} else {
		statusEmoji = "✅"
	}

	msg := fmt.Sprintf("%s %s Order:\n🏷️ %s - %d lot @ ₹%.2f\n%s %s",
		statusEmoji, orderType, action.Symbol, action.Lots, action.Price, statusEmoji, status)
	bot.Send(tgbotapi.NewMessage(tgChatID, msg))

	// Append to sheet
	row := []interface{}{
		time.Now().Format("2006-01-02T15:04:05"),
		action.Symbol,
		strings.ToUpper(string(action.Type)),
		action.Lots,
		action.Price,
		status,
	}
	r := &sheets.ValueRange{Values: [][]interface{}{row}}
	srv.Spreadsheets.Values.Append(spreadsheetID, orderBookRange, r).
		ValueInputOption("RAW").Do()

	log.Printf("Order executed: %s %s - %s", action.Symbol, action.Type, status)
}

// placeSellOrder handles sell orders (different from buy orders)
func placeSellOrder(ctx context.Context, action models.Action) (string, error) {
	// Get Dhan security ID for the trading symbol
	securityId, err := GetSecurityID(action.Symbol)
	if err != nil {
		return "", fmt.Errorf("failed to get security ID: %v", err)
	}

	// Create sell order payload
	orderPayload := map[string]interface{}{
		"dhanClientId":      dhanClientID,
		"correlationId":     fmt.Sprintf("sell_%d", time.Now().Unix()),
		"transactionType":   "SELL",
		"exchangeSegment":   "NSE_EQ",
		"productType":       "CNC",
		"orderType":         "MARKET",
		"validity":          "DAY",
		"securityId":        securityId,
		"quantity":          fmt.Sprintf("%d", action.Lots),
		"disclosedQuantity": "",
		"price":             "",
		"triggerPrice":      "",
		"afterMarketOrder":  action.AfterMarket,
		"amoTime":           "",
		"boProfitValue":     "",
		"boStopLossValue":   "",
	}

	var orderResp struct {
		OrderID     string `json:"orderId"`
		OrderStatus string `json:"orderStatus"`
	}

	client := resty.New()
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("access-token", dhanAccessToken).
		SetBody(orderPayload).
		SetResult(&orderResp).
		Post("https://api.dhan.co/v2/orders")

	if err != nil {
		return "", fmt.Errorf("failed to place sell order: %v", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return fmt.Sprintf("OrderID: %s, Status: %s", orderResp.OrderID, orderResp.OrderStatus), nil
}
