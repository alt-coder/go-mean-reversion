package models

// PortfolioStock represents a stock in the portfolio.
type PortfolioStock struct {
	Symbol        string
	Quantity      int
	AvgPrice      float64
	CurrentPrice  float64
	ProfitLoss    float64
	PercentChange float64
}
