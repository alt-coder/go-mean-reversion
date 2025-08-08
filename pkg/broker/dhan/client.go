package dhan

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "time"

        "github.com/alt-coder/go-mean-reversion/pkg/broker"
        "github.com/alt-coder/go-mean-reversion/pkg/config"
)

// Client implements the broker.Broker interface for Dhan API.
// It manages authentication and HTTP communication with Dhan endpoints.
type Client struct {
        clientID string
        token    string
        baseURL  string
        http     *http.Client
}

// New returns a new Dhan client instance configured with the provided settings.
func New(cfg config.DhanConfig) *Client {
        httpClient := &http.Client{Timeout: 10 * time.Second}
        return &Client{
                clientID: cfg.ClientID,
                token:    cfg.AccessToken,
                baseURL:  cfg.BaseURL,
                http:     httpClient,
        }
}

// GetHoldings retrieves current holdings and their average cost price.
func (c *Client) GetHoldings(ctx context.Context) (map[string]float64, error) {
        payload := map[string]string{"dhanClientId": c.clientID}
        b, _ := json.Marshal(payload)
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/holdings", bytes.NewBuffer(b))
        if err != nil {
                return nil, err
        }
        req.Header.Set("access-token", c.token)
        req.Header.Set("Content-Type", "application/json")

        resp, err := c.http.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("dhan holdings status: %s", resp.Status)
        }

        var data struct {
                Data []struct {
                        TradingSymbol string  `json:"tradingSymbol"`
                        AvgCostPrice  float64 `json:"avgCostPrice"`
                } `json:"data"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
                return nil, err
        }
        out := make(map[string]float64, len(data.Data))
        for _, h := range data.Data {
                out[h.TradingSymbol] = h.AvgCostPrice
        }
        return out, nil
}

// PlaceOrder sends a BUY order request to Dhan.
func (c *Client) PlaceOrder(ctx context.Context, order broker.Order) (*broker.OrderResponse, error) {
        return c.place(ctx, order, "BUY")
}

// PlaceSellOrder sends a SELL order request to Dhan.
func (c *Client) PlaceSellOrder(ctx context.Context, order broker.Order) (*broker.OrderResponse, error) {
        return c.place(ctx, order, "SELL")
}

// place executes the HTTP request for placing an order.
func (c *Client) place(ctx context.Context, order broker.Order, txnType string) (*broker.OrderResponse, error) {
        payload := map[string]interface{}{
                "dhanClientId":     c.clientID,
                "transactionType":  txnType,
                "exchangeSegment":  "NSE_EQ",
                "productType":      "CNC",
                "orderType":        order.OrderType,
                "validity":         "DAY",
                "symbol":           order.Symbol,
                "quantity":         order.Quantity,
                "afterMarketOrder": order.AfterMarket,
                "price":            order.Price,
        }
        b, _ := json.Marshal(payload)
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/orders", bytes.NewBuffer(b))
        if err != nil {
                return nil, err
        }
        req.Header.Set("access-token", c.token)
        req.Header.Set("Content-Type", "application/json")

        resp, err := c.http.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        var data struct {
                OrderID string `json:"orderId"`
                Status  string `json:"orderStatus"`
                Message string `json:"message"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
                return nil, err
        }
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("dhan order status %s: %s", resp.Status, data.Message)
        }
        return &broker.OrderResponse{OrderID: data.OrderID, Status: data.Status, Message: data.Message}, nil
}

