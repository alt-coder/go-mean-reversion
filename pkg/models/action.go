package models

import "time"

// Action represents a trading decision.
type Action struct {
	ID          string
	Type        ActionType
	Symbol      string
	Lots        int
	Price       float64
	Reason      string
	AfterMarket bool
	UserID      int64
	CreatedAt   time.Time
}

type ActionType string

const (
	NewPosition ActionType = "new_position"
	AverageDown ActionType = "average_down"
	Sell        ActionType = "sell"
)
