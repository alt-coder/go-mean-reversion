package models

import "time"

// OrderLog represents an order execution record for Sheets.
type OrderLog struct {
	Timestamp time.Time
	Symbol    string
	Action    string
	Lots      int
	Price     float64
	Status    string
}
