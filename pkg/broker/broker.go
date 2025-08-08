package broker

import "context"

// Broker provides a common interface for broker operations.
type Broker interface {
	GetHoldings(ctx context.Context) (map[string]float64, error)
	PlaceOrder(ctx context.Context, order Order) (*OrderResponse, error)
	PlaceSellOrder(ctx context.Context, order Order) (*OrderResponse, error)
}

// Order represents a trade instruction sent to the broker.
type Order struct {
	Symbol      string
	Quantity    int
	Price       float64
	OrderType   string
	AfterMarket bool
}

// OrderResponse captures the broker's response to an order request.
type OrderResponse struct {
	OrderID string
	Status  string
	Message string
}
