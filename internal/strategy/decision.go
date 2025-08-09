package strategy

import (
	"fmt"
	"time"

	"github.com/alt-coder/go-mean-reversion/pkg/models"
)

// DetermineBuyActions evaluates candidates and current holdings to produce buy or averaging actions.
func DetermineBuyActions(cands []models.Candidate, holds map[string]float64, maxActions int, avgThreshold float64) []models.Action {
	var actions []models.Action

	// First collect entry actions for symbols not already held
	var entryActions []models.Action
	now := time.Now()
	for _, c := range cands {
		if _, ok := holds[c.Symbol]; ok {
			continue
		}
		reason := fmt.Sprintf("%.2f%% below SMA20 at ₹%.2f", c.Deviation*100, c.Price)
		if c.SMA20 > 0 {
			reason = fmt.Sprintf("%.2f%% below SMA20 (₹%.2f) at ₹%.2f", c.Deviation*100, c.SMA20, c.Price)
		}
		entryActions = append(entryActions, models.Action{
			ID:        fmt.Sprintf("buy_%s_%d", c.Symbol, now.UnixNano()),
			Type:      models.NewPosition,
			Symbol:    c.Symbol,
			Lots:      1,
			Price:     c.Price,
			Reason:    reason,
			CreatedAt: now,
		})
	}

	// Determine potential averaging action for worst performer we hold
	var avgAction *models.Action
	worstSymbol := ""
	worstDrop := avgThreshold
	var worstPrice float64
	for _, c := range cands {
		avgPrice, exists := holds[c.Symbol]
		if !exists {
			continue
		}
		actualDrop := (c.Price - avgPrice) / avgPrice
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
		avgAction = &models.Action{
			ID:        fmt.Sprintf("avg_%s_%d", worstSymbol, now.UnixNano()),
			Type:      models.AverageDown,
			Symbol:    worstSymbol,
			Lots:      1,
			Price:     worstPrice,
			Reason:    fmt.Sprintf("averaging down at ₹%.2f (%.2f%% drop)", worstPrice, worstDrop*100),
			CreatedAt: now,
		}
	}

	// Prioritize entry actions first then averaging, respecting maxActions limit
	for i := 0; i < len(entryActions) && len(actions) < maxActions; i++ {
		actions = append(actions, entryActions[i])
	}
	if len(actions) < maxActions && avgAction != nil {
		actions = append(actions, *avgAction)
	}
	return actions
}

// DetermineSellActions returns sell actions for profitable portfolio stocks.
func DetermineSellActions(portfolio []models.PortfolioStock, profitThreshold float64) []models.Action {
	var sellActions []models.Action
	now := time.Now()
	for _, stock := range portfolio {
		if stock.PercentChange > profitThreshold {
			sellActions = append(sellActions, models.Action{
				ID:        fmt.Sprintf("sell_%s_%d", stock.Symbol, now.UnixNano()),
				Type:      models.Sell,
				Symbol:    stock.Symbol,
				Lots:      stock.Quantity,
				Price:     stock.CurrentPrice,
				Reason:    fmt.Sprintf("%.2f%% profit (₹%.2f → ₹%.2f)", stock.PercentChange, stock.AvgPrice, stock.CurrentPrice),
				CreatedAt: now,
			})
		}
	}
	return sellActions
}
