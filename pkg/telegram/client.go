package telegram

import (
        "context"
        "fmt"
        "strings"

        "github.com/alt-coder/go-mean-reversion/pkg/models"
        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Client defines messaging operations for Telegram.
type Client interface {
        SendMessage(ctx context.Context, userID int64, message string) error
        SendActionMessage(ctx context.Context, userID int64, action models.Action) error
        SendSummary(ctx context.Context, userID int64, actions []models.Action) error
        StartListening(ctx context.Context, handler CallbackHandler) error
}

// CallbackHandler processes Telegram callbacks.
type CallbackHandler interface {
        HandleAccept(ctx context.Context, actionID string, userID int64) error
        HandleReject(ctx context.Context, actionID string, userID int64) error
}

// UserConfig stores Telegram user configuration.
type UserConfig struct {
        ChatID   int64
        Username string
        Active   bool
}

// Service implements the Client interface using the Telegram Bot API.
type Service struct {
        Bot   *tgbotapi.BotAPI
        Users []UserConfig
}

// NewService creates a new Telegram service.
func NewService(bot *tgbotapi.BotAPI, users []UserConfig) *Service {
        return &Service{Bot: bot, Users: users}
}

// SendMessage sends a plain text message to a user.
func (s *Service) SendMessage(ctx context.Context, userID int64, message string) error {
        msg := tgbotapi.NewMessage(userID, message)
        _, err := s.Bot.Send(msg)
        return err
}

// SendActionMessage sends a trading action with accept/reject buttons.
func (s *Service) SendActionMessage(ctx context.Context, userID int64, action models.Action) error {
        text := fmt.Sprintf("%s %d @ %.2f: %s", action.Symbol, action.Lots, action.Price, action.Reason)
        msg := tgbotapi.NewMessage(userID, text)
        acceptData := fmt.Sprintf("accept:%s", action.ID)
        rejectData := fmt.Sprintf("reject:%s", action.ID)
        row := tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonData("Accept", acceptData),
                tgbotapi.NewInlineKeyboardButtonData("Reject", rejectData),
        )
        msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
        _, err := s.Bot.Send(msg)
        return err
}

// SendSummary sends a summary of actions to a user.
func (s *Service) SendSummary(ctx context.Context, userID int64, actions []models.Action) error {
        var b strings.Builder
        b.WriteString("Summary:\n")
        for _, a := range actions {
                b.WriteString(fmt.Sprintf("%s %d @ %.2f\n", a.Symbol, a.Lots, a.Price))
        }
        return s.SendMessage(ctx, userID, b.String())
}

// StartListening begins processing callback queries and routes them to handler.
func (s *Service) StartListening(ctx context.Context, handler CallbackHandler) error {
        u := tgbotapi.NewUpdate(0)
        u.Timeout = 30
        updates := s.Bot.GetUpdatesChan(u)

        go func() {
                for {
                        select {
                        case <-ctx.Done():
                                return
                        case upd := <-updates:
                                if upd.CallbackQuery == nil {
                                        continue
                                }
                                parts := strings.SplitN(upd.CallbackQuery.Data, ":", 2)
                                if len(parts) != 2 {
                                        continue
                                }
                                actionID := parts[1]
                                switch parts[0] {
                                case "accept":
                                        handler.HandleAccept(ctx, actionID, upd.CallbackQuery.From.ID)
                                case "reject":
                                        handler.HandleReject(ctx, actionID, upd.CallbackQuery.From.ID)
                                }
                                s.Bot.Request(tgbotapi.NewCallback(upd.CallbackQuery.ID, ""))
                        }
                }
        }()
        return nil
}

