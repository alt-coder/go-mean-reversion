package models

// Candidate represents a stock candidate from analysis sources.
type Candidate struct {
	Symbol    string
	Deviation float64
	Price     float64
	SMA20     float64
	Source    string
}
