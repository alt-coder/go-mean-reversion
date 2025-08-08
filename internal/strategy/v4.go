package strategy

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/alt-coder/go-mean-reversion/pkg/models"
)

// Inputs you already have:
// - candidates (Symbol, Price, SMA20, Deviation) 
// - holdings map[symbol]avgPrice via Broker.GetHoldings 
// - portfolio slice with Quantity/AvgPrice/CurrentPrice for sells 
// - models.Action / ActionType for output 

// ATRPositionSize returns floor( (equity * riskPct) / (atrMult * atr) ) with a minimum of 1 share.
// If atr is missing or <=0, returns 0 (caller skips).
func ATRPositionSize(equity float64, riskPct float64, atr float64, atrMult float64) int {
	if atr <= 0 || atrMult <= 0 || equity <= 0 || riskPct <= 0 {
		return 0
	}
	riskRupees := equity * riskPct
	qty := int(math.Floor(riskRupees / (atrMult * atr)))
	if qty < 1 {
		qty = 1
	}
	return qty
}

// DetermineBuyActionsV4 builds buy/avg-down actions using ATR sizing.
// Params:
//   - cands: top candidates (sorted inside by most below SMA20)
//   - holds: map[symbol]avgPrice (current holdings)
//   - atrBySymbol: map[symbol]ATR(14) in rupees
//   - equityNow: current account equity (or cash proxy) used to size risk
//   - riskPct: risk per trade (e.g., 0.01 = 1%)
//   - atrMult: stop distance in ATRs (e.g., 2.0 = 2×ATR)
//   - maxBuysPerDay: cap across new+avg orders
//   - avgTrigger: e.g., -0.05 means average if price <= avg*(1-5%)
func DetermineBuyActionsV4(
	cands []models.Candidate,
	holds map[string]float64,
	atrBySymbol map[string]float64,
	equityNow float64,
	riskPct, atrMult float64,
	maxBuysPerDay int,
	avgTrigger float64,
) []models.Action {

	now := time.Now()
	actions := make([]models.Action, 0, maxBuysPerDay)

	// Rank: most below SMA20 first (Deviation is (Price-SMA20)/SMA20; more negative = more below)
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Deviation < cands[j].Deviation })

	buysLeft := maxBuysPerDay
	for _, c := range cands {
		if buysLeft <= 0 {
			break
		}
		atr := c.ATR
		qty := ATRPositionSize(equityNow, riskPct, atr, atrMult)
		if qty <= 0 {
			continue
		}

		if avg, held := holds[c.Symbol]; held {
			// Average-down rule
			if c.Price/avg-1.0 <= avgTrigger {
				actions = append(actions, models.Action{
					ID:          fmt.Sprintf("v4-avg-%s-%d", c.Symbol, now.UnixNano()),
					Type:        models.AverageDown,
					Symbol:      c.Symbol,
					Lots:        qty,           // quantity sized by ATR
					Price:       c.Price,       // EOD signal; your runner executes next day @ signal price
					AfterMarket: true,
					Reason:      fmt.Sprintf("V4 ATR avg: qty=%d (risk %.2f%%, %.1fxATR=₹%.2f) Deviation=%.2f%%", qty, riskPct*100, atrMult, atr*atrMult, c.Deviation*100),
					CreatedAt:   now,
				})
				buysLeft--
			}
			continue
		}

		// New position
		actions = append(actions, models.Action{
			ID:          fmt.Sprintf("v4-new-%s-%d", c.Symbol, now.UnixNano()),
			Type:        models.NewPosition,
			Symbol:      c.Symbol,
			Lots:        qty,
			Price:       c.Price,
			AfterMarket: true,
			Reason:      fmt.Sprintf("V4 ATR entry: qty=%d (risk %.2f%%, %.1fxATR=₹%.2f) Deviation=%.2f%%", qty, riskPct*100, atrMult, atr*atrMult, c.Deviation*100),
			CreatedAt:   now,
		})
		buysLeft--
	}

	return actions
}

// DetermineSellActionsV4: full exit if profit ≥ profitThreshold (e.g., 0.08 = +8%).
// Uses your portfolio view for current price and quantities.
func DetermineSellActionsV4(
	portfolio []models.PortfolioStock,
	profitThreshold float64,
) []models.Action {
	now := time.Now()
	var actions []models.Action

	for _, p := range portfolio {
		if p.AvgPrice <= 0 || p.Quantity <= 0 {
			continue
		}
		gain := p.CurrentPrice/p.AvgPrice - 1.0
		if gain >= profitThreshold {
			actions = append(actions, models.Action{
				ID:          fmt.Sprintf("v4-sell-%s-%d", p.Symbol, now.UnixNano()),
				Type:        models.Sell,
				Symbol:      p.Symbol,
				Lots:        p.Quantity, // full exit; change to partial if desired
				Price:       p.CurrentPrice,
				AfterMarket: true,
				Reason:      fmt.Sprintf("V4 TP: +%.2f%% ≥ %.2f%%", gain*100, profitThreshold*100),
				CreatedAt:   now,
			})
		}
	}
	return actions
}
