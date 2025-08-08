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

type yfResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
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

// TopCandidates queries Yahoo Finance for Nifty 50 stocks and returns
// the top five that are furthest below their 20-day SMA.
func (c *Client) TopCandidates(ctx context.Context) ([]models.Candidate, error) {
	symbols, err := c.sheets.GetNifty50Symbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("load symbols: %w", err)
	}

	now := time.Now()
	period1 := now.AddDate(0, 0, -30).Unix()
	period2 := now.Unix()

	cands := make([]models.Candidate, 0, len(symbols))
	for _, sym := range symbols {
		url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s.NS?period1=%d&period2=%d&interval=1d", sym, period1, period2)
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
		closes := result.Indicators.Quote[0].Close
		price := result.Meta.RegularMarketPrice
		if len(closes) < 20 {
			continue
		}
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
		if dev < 0 {
			cands = append(cands, models.Candidate{
				Symbol: sym, Deviation: dev, Price: price, SMA20: sma20, Source: "yahoo",
			})
		}
		time.Sleep(500 * time.Millisecond)
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
