package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alt-coder/go-mean-reversion/pkg/broker"
	"github.com/alt-coder/go-mean-reversion/pkg/models"
	"github.com/alt-coder/go-mean-reversion/pkg/sheets"
)

// actionHandler processes Telegram callback actions.
type actionHandler struct {
	broker  broker.Broker
	sheets  sheets.Client
	actions map[string]models.Action
}

// newActionHandler creates a handler with dependencies.
func newActionHandler(br broker.Broker, sh sheets.Client) *actionHandler {
	return &actionHandler{
		broker:  br,
		sheets:  sh,
		actions: make(map[string]models.Action),
	}
}

// HandleAccept executes the action by placing an order and logging it.
func (h *actionHandler) HandleAccept(ctx context.Context, actionID string, userID int64) error {
	a, ok := h.actions[actionID]
	if !ok {
		return fmt.Errorf("unknown action %s", actionID)
	}

	order := broker.Order{
		Symbol:      a.Symbol,
		Quantity:    a.Lots,
		Price:       a.Price,
		OrderType:   "MARKET",
		AfterMarket: a.AfterMarket,
	}

	var (
		resp *broker.OrderResponse
		err  error
	)

	if a.Type == models.Sell {
		resp, err = h.broker.PlaceSellOrder(ctx, order)
	} else {
		resp, err = h.broker.PlaceOrder(ctx, order)
	}
	if err != nil {
		return fmt.Errorf("place order: %w", err)
	}

	logEntry := models.OrderLog{
		Timestamp: time.Now(),
		Symbol:    a.Symbol,
		Action:    string(a.Type),
		Lots:      a.Lots,
		Price:     a.Price,
		Status:    resp.Status,
	}
	if err := h.sheets.LogOrder(ctx, logEntry); err != nil {
		return fmt.Errorf("log order: %w", err)
	}

	delete(h.actions, actionID)
	return nil
}

// HandleReject removes the action from pending list without executing.
func (h *actionHandler) HandleReject(ctx context.Context, actionID string, userID int64) error {
	delete(h.actions, actionID)
	return nil
}
