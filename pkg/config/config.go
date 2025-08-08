package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds application configuration values.
type Config struct {
	Dhan     DhanConfig
	Telegram TelegramConfig
	Sheets   SheetsConfig
	Trading  TradingConfig
}

type DhanConfig struct {
	ClientID    string
	AccessToken string
	BaseURL     string
}

type TelegramConfig struct {
	BotToken string
	ChatID   int64
}

type SheetsConfig struct {
	SpreadsheetID string
	ClientEmail   string
	PrivateKey    string
	ProjectID     string
}

type TradingConfig struct {
	MaxNewPositions    int
	AveragingThreshold float64
	CSVPath            string
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	chatID, err := strconv.ParseInt(os.Getenv("TELEGRAM_CHAT_ID"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("invalid TELEGRAM_CHAT_ID: %w", err)
	}
	maxPos, err := strconv.Atoi(os.Getenv("MAX_NEW_POSITIONS"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid MAX_NEW_POSITIONS: %w", err)
	}
	avgThreshold, err := strconv.ParseFloat(os.Getenv("AVERAGING_THRESHOLD"), 64)
	if err != nil {
		return Config{}, fmt.Errorf("invalid AVERAGING_THRESHOLD: %w", err)
	}
	cfg := Config{
		Dhan: DhanConfig{
			ClientID:    os.Getenv("DHAN_CLIENT_ID"),
			AccessToken: os.Getenv("DHAN_ACCESS_TOKEN"),
			BaseURL:     "https://api.dhan.co",
		},
		Telegram: TelegramConfig{
			BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
			ChatID:   chatID,
		},
		Sheets: SheetsConfig{
			SpreadsheetID: os.Getenv("SPREADSHEET_ID"),
			ClientEmail:   os.Getenv("GOOGLE_CLIENT_EMAIL"),
			PrivateKey:    os.Getenv("GOOGLE_PRIVATE_KEY"),
			ProjectID:     os.Getenv("GOOGLE_PROJECT_ID"),
		},
		Trading: TradingConfig{
			MaxNewPositions:    maxPos,
			AveragingThreshold: avgThreshold,
			CSVPath:            os.Getenv("CSV_PATH"),
		},
	}
	return cfg, nil
}
