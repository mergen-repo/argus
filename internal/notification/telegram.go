package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type TelegramConfig struct {
	BotToken      string
	DefaultChatID string
}

type TelegramBotSender struct {
	cfg    TelegramConfig
	client *http.Client
}

func NewTelegramBotSender(cfg TelegramConfig) *TelegramBotSender {
	return &TelegramBotSender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (t *TelegramBotSender) SendMessage(ctx context.Context, message string) error {
	return t.SendToChat(ctx, t.cfg.DefaultChatID, message)
}

func (t *TelegramBotSender) SendToChat(ctx context.Context, chatID, message string) error {
	if chatID == "" {
		chatID = t.cfg.DefaultChatID
	}
	if chatID == "" {
		return fmt.Errorf("notification: telegram chat_id not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.cfg.BotToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notification: telegram marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("notification: telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("notification: telegram send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notification: telegram status %d", resp.StatusCode)
	}

	return nil
}
