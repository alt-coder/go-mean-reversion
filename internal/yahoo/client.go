package yahoo

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/alt-coder/go-mean-reversion/pkg/data"
	"github.com/alt-coder/go-mean-reversion/pkg/models"
	"github.com/alt-coder/go-mean-reversion/pkg/sheets"
)

// Client implements the data.Source interface using Yahoo Finance API.
type Client struct {
	http   *resty.Client
	sheets sheets.Client
}

// New creates a Yahoo Finance data source. It requires a Sheets client
// to load the universe of symbols.
func New(sh sheets.Client) *Client {
	c := resty.New()
	c.SetTimeout(15 * time.Second)
	c.SetRetryCount(2)
	c.SetRetryWaitTime(1 * time.Second)
	c.SetRetryMaxWaitTime(3 * time.Second)
	c.SetHeader("User-Agent", "Mozilla/5.0")
	c.SetHeader("Accept", "application/json")
	c.SetHeader("Accept-Language", "en-US,en;q=0.9")
	return &Client{http: c, sheets: sh}
}

var _ data.Source = (*Client)(nil)

// internal/yahoo/client.go

type yfResponse struct {
    Chart struct {
        Result []struct {
            Meta struct {
                RegularMarketPrice float64 `json:"regularMarketPrice"`
            } `json:"meta"`
            Indicators struct {
                Quote []struct {
                    Close []float64 `json:"close"`
                    High  []float64 `json:"high"`  // ← NEW
                    Low   []float64 `json:"low"`   // ← NEW
                } `json:"quote"`
            } `json:"indicators"`
        } `json:"result"`
    } `json:"chart"`
}

// atrWilder returns the latest ATR(n) using Wilder’s smoothing.
// It tolerates NaNs in the input; returns (atr, ok).
func atrWilder(high, low, close []float64, n int) (float64, bool) {
    if len(high) == 0 || len(low) == 0 || len(close) == 0 {
        return 0, false
    }
    // all series must have the same length
    L := len(close)
    if len(high) != L || len(low) != L {
        return 0, false
    }
    if L < n+1 { // need at least n TRs (thus n+1 closes)
        return 0, false
    }

    // True Range per day
    tr := make([]float64, L)
    prevClose := close[0]
    for i := 0; i < L; i++ {
        h, l, cprev := high[i], low[i], prevClose
        // Skip if today’s high/low is NaN
        if math.IsNaN(h) || math.IsNaN(l) {
            tr[i] = math.NaN()
        } else {
            v1 := h - l
            v2 := math.Abs(h - cprev)
            v3 := math.Abs(l - cprev)
            tr[i] = math.Max(v1, math.Max(v2, v3))
        }
        if !math.IsNaN(close[i]) {
            prevClose = close[i]
        }
    }

    // Seed ATR with SMA of first n TRs (skipping NaNs)
    count := 0
    sum := 0.0
    for i := 1; i <= n; i++ { // TR starts at index 1 effectively
        if i >= L || math.IsNaN(tr[i]) {
            continue
        }
        sum += tr[i]
        count++
    }
    if count < n {
        return 0, false
    }
    atr := sum / float64(n)
    alpha := 1.0 / float64(n)

    // Wilder smoothing for the rest
    for i := n + 1; i < L; i++ {
        if math.IsNaN(tr[i]) {
            continue
        }
        atr = atr + alpha*(tr[i]-atr)
    }
    if atr <= 0 || math.IsNaN(atr) || math.IsInf(atr, 0) {
        return 0, false
    }
    return atr, true
}

// TopCandidates queries Yahoo Finance for Nifty 50 stocks and returns
// the top five that are furthest below their 20-day SMA.
// Now also attaches ATR(14) to each candidate.
func (c *Client) TopCandidates(ctx context.Context) ([]models.Candidate, error) {
    symbols, err := c.sheets.GetNifty50Symbols(ctx)
    if err != nil {
        return nil, fmt.Errorf("load symbols: %w", err)
    }

    now := time.Now()
    period1 := now.AddDate(0, 0, -30).Unix() // ~ last 30 daily bars
    period2 := now.Unix()

    cands := make([]models.Candidate, 0, len(symbols))
    for _, sym := range symbols {
        url := fmt.Sprintf(
            "https://query1.finance.yahoo.com/v8/finance/chart/%s.NS?period1=%d&period2=%d&interval=1d",
            sym, period1, period2,
        )

        var resp yfResponse
        r, err := c.http.R().SetContext(ctx).SetResult(&resp).Get(url)
        if err != nil {
            log.Printf("yahoo request %s: %v", sym, err)
            continue
        }
        if r.StatusCode() != 200 || len(resp.Chart.Result) == 0 || len(resp.Chart.Result[0].Indicators.Quote) == 0 {
            log.Printf("yahoo bad response for %s: status %d", sym, r.StatusCode())
            continue
        }

        result := resp.Chart.Result[0]
        quote := result.Indicators.Quote[0]
        closes := quote.Close
        highs  := quote.High
        lows   := quote.Low
        price  := result.Meta.RegularMarketPrice

        if len(closes) < 20 {
            continue
        }

        // SMA20 over the last 20 valid closes
        smaCloses := make([]float64, 0, 20)
        for i := len(closes) - 20; i < len(closes); i++ {
            if !math.IsNaN(closes[i]) {
                smaCloses = append(smaCloses, closes[i])
            }
        }
        if len(smaCloses) < 20 {
            continue
        }
        var sum float64
        for _, p := range smaCloses {
            sum += p
        }
        sma20 := sum / float64(len(smaCloses))
        dev := (price - sma20) / sma20
        if dev >= 0 {
            // We only want names below SMA20
            continue
        }

        // --- NEW: ATR(14) ---
        atr, ok := atrWilder(highs, lows, closes, 14)
        if !ok {
            // If ATR is missing/invalid, still return the candidate (optional),
            // but with ATR=0.0. If you prefer to skip, just `continue`.
            atr = 0.0
        }

        cands = append(cands, models.Candidate{
            Symbol:    sym,
            Deviation: dev,
            Price:     price,
            SMA20:     sma20,
            Source:    "yahoo",
            ATR:       atr,    // ← attach ATR(14)
        })

        // being polite to Yahoo
        time.Sleep(100 * time.Millisecond)
    }

    if len(cands) == 0 {
        return nil, fmt.Errorf("no stocks below SMA20")
    }

    sort.Slice(cands, func(i, j int) bool {
        return cands[i].Deviation < cands[j].Deviation
    })
    if len(cands) > 5 {
        cands = cands[:5]
    }
    return cands, nil
}
