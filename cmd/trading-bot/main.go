package main

import (
	"context"
	"log"
	"time"

	"github.com/alt-coder/go-mean-reversion/internal/strategy"
	"github.com/alt-coder/go-mean-reversion/internal/yahoo"
	"github.com/alt-coder/go-mean-reversion/pkg/broker"
	"github.com/alt-coder/go-mean-reversion/pkg/broker/dhan"
	"github.com/alt-coder/go-mean-reversion/pkg/config"
	"github.com/alt-coder/go-mean-reversion/pkg/data"
	dcache "github.com/alt-coder/go-mean-reversion/pkg/dhan/cache"
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

	// initialise sheets service
	sh, err := sheets.NewService(ctx, cfg.Sheets)
	if err != nil {
		log.Fatalf("sheets service: %v", err)
	}

	// load Nifty50 symbols and prepare security ID cache
	symbols, err := sh.GetNifty50Symbols(ctx)
	if err != nil {
		log.Fatalf("load symbols: %v", err)
	}
	secCache := dcache.New(cfg.Trading.CSVPath, symbols, 5*time.Minute)

	// initialise broker
	br := dhan.New(cfg.Dhan, secCache)

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

	ds := yahoo.New(sh)

	actions, err := runStrategy(ctx, cfg, br, sh, ds)
	if err != nil {
		log.Fatalf("run strategy: %v", err)
	}

	if len(actions) == 0 {
		log.Println("no actions needed based on current analysis")
	} else {
		if err := tg.SendSummary(ctx, cfg.Telegram.ChatID, actions); err != nil {
			log.Printf("telegram summary: %v", err)
		}
		for _, a := range actions {
			handler.actions[a.ID] = a
			if err := tg.SendActionMessage(ctx, cfg.Telegram.ChatID, a); err != nil {
				log.Printf("telegram send: %v", err)
			}
		}
	}

	select {}
}

// runStrategy orchestrates a basic trading workflow using modular services.
func runStrategy(ctx context.Context, cfg config.Config, br broker.Broker, sh sheets.Client, ds data.Source) ([]models.Action, error) {
	cands, err := ds.TopCandidates(ctx)
	if err != nil {
		log.Printf("yahoo top candidates: %v; falling back to sheets", err)
		cands, err = sh.GetTopCandidates(ctx)
		if err != nil {
			return nil, err
		}
	}

	holds, err := br.GetHoldings(ctx)
	if err != nil {
		return nil, err
	}

	portfolio, err := sh.GetPortfolio(ctx)
	if err != nil {
		log.Printf("portfolio read failed: %v", err)
	}

	buyActions := strategy.DetermineBuyActions(cands, holds, cfg.Trading.MaxNewPositions, cfg.Trading.AveragingThreshold)
	sellActions := strategy.DetermineSellActions(portfolio, cfg.Trading.ProfitThreshold)
	actions := append(buyActions, sellActions...)
	return actions, nil
}
