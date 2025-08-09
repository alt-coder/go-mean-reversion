package dhan

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/alt-coder/go-mean-reversion/pkg/broker"
	"github.com/alt-coder/go-mean-reversion/pkg/config"
	dcache "github.com/alt-coder/go-mean-reversion/pkg/dhan/cache"
)

// Client implements the broker.Broker interface for Dhan API.
// It uses resty for HTTP communication with Dhan endpoints.
type Client struct {
	clientID string
	token    string
	client   *resty.Client
	holdURL  string
	orderURL string
	cache    *dcache.Cache
}

// New returns a new Dhan client instance configured with the provided settings
// and security cache.
func New(cfg config.DhanConfig, cache *dcache.Cache) *Client {
	r := resty.New()
	r.SetTimeout(10 * time.Second)
	return &Client{
		clientID: cfg.ClientID,
		token:    cfg.AccessToken,
		client:   r,
		holdURL:  cfg.BaseURL + "/v2/holdings",
		orderURL: cfg.BaseURL + "/v2/orders",
		cache:    cache,
	}
}

// GetHoldings retrieves current holdings and their average cost price.
func (d *Client) GetHoldings(ctx context.Context) (map[string]float64, error) {
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

	resp, err := d.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("access-token", d.token).
		SetResult(&holdings).
		Get(d.holdURL)

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

// PlaceOrder sends a BUY order request to Dhan.
func (d *Client) PlaceOrder(ctx context.Context, order broker.Order) (*broker.OrderResponse, error) {
	return d.place(ctx, order, "BUY")
}

// PlaceSellOrder sends a SELL order request to Dhan.
func (d *Client) PlaceSellOrder(ctx context.Context, order broker.Order) (*broker.OrderResponse, error) {
	return d.place(ctx, order, "SELL")
}

// place executes the HTTP request for placing an order.
func (d *Client) place(ctx context.Context, order broker.Order, txnType string) (*broker.OrderResponse, error) {
	securityID, err := d.cache.GetID(order.Symbol)
	if err != nil {
		log.Print(err)
		return nil, fmt.Errorf("failed to get security ID: %v", err)
	}

	payload := map[string]interface{}{
		"dhanClientId":      d.clientID,
		"correlationId":     fmt.Sprintf("bot_%d", time.Now().Unix()),
		"transactionType":   txnType,
		"exchangeSegment":   "NSE_EQ",
		"productType":       "CNC",
		"orderType":         "MARKET",
		"validity":          "DAY",
		"securityId":        securityID,
		"quantity":          fmt.Sprintf("%d", order.Quantity),
		"disclosedQuantity": "",
		"price":             "",
		"triggerPrice":      "",
		"afterMarketOrder":  order.AfterMarket,
		"amoTime":           "",
		"boProfitValue":     "",
		"boStopLossValue":   "",
	}

	var respBody struct {
		OrderID     string `json:"orderId"`
		OrderStatus string `json:"orderStatus"`
	}

	resp, err := d.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("access-token", d.token).
		SetBody(payload).
		SetResult(&respBody).
		Post(d.orderURL)

	if err != nil {
		return nil, fmt.Errorf("failed to place order: %v", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return &broker.OrderResponse{OrderID: respBody.OrderID, Status: respBody.OrderStatus, Message: respBody.OrderStatus}, nil
}

var _ broker.Broker = (*Client)(nil)
