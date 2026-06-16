package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"fintrack-backend/config"
	"fintrack-backend/internal/gateway"
)

// BotPoller handles long polling from Telegram Bot API.
// It no longer holds a DB connection — all data operations go through GatewayRouter.
type BotPoller struct {
	cfg    *config.Config
	router *gateway.GatewayRouter
}

// NewBotPoller creates a new poller that routes updates through the gateway.
func NewBotPoller(cfg *config.Config, router *gateway.GatewayRouter) *BotPoller {
	return &BotPoller{cfg: cfg, router: router}
}

type UpdateResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// Start runs the polling loop until context is cancelled.
// On startup it also registers bot commands via setMyCommands.
func (p *BotPoller) Start(ctx context.Context) {
	log.Println("[Poller] Starting Telegram Bot Poller...")

	// Register bot command list with Telegram
	SetMyCommands(p.cfg.TelegramBotToken)

	offset := 0
	client := &http.Client{Timeout: 40 * time.Second}

	// Build a handler that shares our router
	handler := &WebhookHandler{cfg: p.cfg, router: p.router}

	for {
		select {
		case <-ctx.Done():
			log.Println("[Poller] Context cancelled, stopping.")
			return
		default:
			updates, err := p.getUpdates(client, offset)
			if err != nil {
				log.Printf("[Poller] Error fetching updates: %v. Retry in 5s...", err)
				time.Sleep(5 * time.Second)
				continue
			}
			for _, update := range updates {
				go handler.processUpdate(update)
				if update.UpdateID >= offset {
					offset = update.UpdateID + 1
				}
			}
		}
	}
}

func (p *BotPoller) getUpdates(client *http.Client, offset int) ([]Update, error) {
	url := fmt.Sprintf(
		`https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30&allowed_updates=["message","callback_query"]`,
		p.cfg.TelegramBotToken, offset,
	)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram returned status %s", resp.Status)
	}

	var apiResp UpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	if !apiResp.Ok {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}
	return apiResp.Result, nil
}
