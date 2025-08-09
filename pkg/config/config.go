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
	ProfitThreshold    float64
	CSVPath            string
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	chatID, err := strconv.ParseInt(os.Getenv(EnvTelegramChatID), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("invalid TELEGRAM_CHAT_ID: %w", err)
	}
	maxPos, err := strconv.Atoi(os.Getenv(EnvMaxNewPositions))
	if err != nil {
		return Config{}, fmt.Errorf("invalid MAX_NEW_POSITIONS: %w", err)
	}
	avgThreshold, err := strconv.ParseFloat(os.Getenv(EnvAveragingThreshold), 64)
	if err != nil {
		return Config{}, fmt.Errorf("invalid AVERAGING_THRESHOLD: %w", err)
	}
	profitStr := os.Getenv(EnvProfitThreshold)
	profitThreshold := 5.0
	if profitStr != "" {
		if profitThreshold, err = strconv.ParseFloat(profitStr, 64); err != nil {
			return Config{}, fmt.Errorf("invalid PROFIT_THRESHOLD: %w", err)
		}
	}
	cfg := Config{
		Dhan: DhanConfig{
			ClientID:    os.Getenv(EnvDhanClientID),
			AccessToken: os.Getenv(EnvDhanAccessToken),
			BaseURL:     "https://api.dhan.co",
		},
		Telegram: TelegramConfig{
			BotToken: os.Getenv(EnvTelegramBotToken),
			ChatID:   chatID,
		},
		Sheets: SheetsConfig{
			SpreadsheetID: os.Getenv(EnvSpreadsheetID),
			ClientEmail:   os.Getenv(EnvGoogleClientEmail),
			PrivateKey:    os.Getenv(EnvGooglePrivateKey),
			ProjectID:     os.Getenv(EnvGoogleProjectID),
		},
		Trading: TradingConfig{
			MaxNewPositions:    maxPos,
			AveragingThreshold: avgThreshold,
			ProfitThreshold:    profitThreshold,
			CSVPath:            os.Getenv(EnvCSVPath),
		},
	}
	return cfg, nil
}
