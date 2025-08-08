package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alt-coder/go-mean-reversion/pkg/broker"
	"github.com/alt-coder/go-mean-reversion/pkg/broker/dhan"
	"github.com/alt-coder/go-mean-reversion/pkg/config"
	"github.com/alt-coder/go-mean-reversion/pkg/models"
	"github.com/alt-coder/go-mean-reversion/pkg/sheets"
	"github.com/alt-coder/go-mean-reversion/pkg/telegram"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// initialise broker
	br := dhan.New(cfg.Dhan)

	// initialise sheets service
	sh, err := sheets.NewService(ctx, cfg.Sheets)
	if err != nil {
		log.Fatalf("sheets service: %v", err)
	}

	// initialise telegram client
	bot, err := tgbotapi.NewBotAPI(cfg.Telegram.BotToken)
	if err != nil {
		log.Fatalf("telegram bot: %v", err)
	}
	tg := telegram.NewService(bot, []telegram.UserConfig{{ChatID: cfg.Telegram.ChatID, Active: true}})

	handler := newActionHandler(br, sh)
	if err := tg.StartListening(ctx, handler); err != nil {
		log.Fatalf("telegram listen: %v", err)
	}

	actions, err := runStrategy(ctx, cfg, br, sh)
	if err != nil {
		log.Fatalf("run strategy: %v", err)
	}

	for _, a := range actions {
		handler.actions[a.ID] = a
		if err := tg.SendActionMessage(ctx, cfg.Telegram.ChatID, a); err != nil {
			log.Printf("telegram send: %v", err)
		}
	}

	select {}
}

// runStrategy orchestrates a basic trading workflow using modular services.
func runStrategy(ctx context.Context, cfg config.Config, br broker.Broker, sh sheets.Client) ([]models.Action, error) {
	cands, err := sh.GetTopCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("candidates: %w", err)
	}

	holds, err := br.GetHoldings(ctx)
	if err != nil {
		return nil, fmt.Errorf("holdings: %w", err)
	}

	actions := make([]models.Action, 0)
	newPos := 0
	for _, c := range cands {
		if _, owned := holds[c.Symbol]; owned {
			continue
		}
		if newPos >= cfg.Trading.MaxNewPositions {
			break
		}
		a := models.Action{
			ID:        fmt.Sprintf("%s-%d", c.Symbol, time.Now().UnixNano()),
			Type:      models.NewPosition,
			Symbol:    c.Symbol,
			Lots:      1,
			Price:     c.Price,
			Reason:    fmt.Sprintf("dev %.2f", c.Deviation),
			CreatedAt: time.Now(),
		}
		actions = append(actions, a)
		newPos++
	}
	return actions, nil
}
