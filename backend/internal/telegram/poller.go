package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"fintrack-backend/config"
	"github.com/jmoiron/sqlx"
)

// BotPoller handles long polling from Telegram Bot API
type BotPoller struct {
	cfg *config.Config
	db  *sqlx.DB
}

func NewBotPoller(cfg *config.Config, db *sqlx.DB) *BotPoller {
	return &BotPoller{cfg: cfg, db: db}
}

type UpdateResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// Start runs the polling loop until context is cancelled.
// On startup it also registers bot commands via setMyCommands.
func (p *BotPoller) Start(ctx context.Context) {
	log.Println("[Poller] Starting Telegram Bot Poller...")

	// Daftarkan perintah bot ke Telegram (muncul di menu "/" Telegram)
	SetMyCommands(p.cfg.TelegramBotToken)

	offset := 0
	client := &http.Client{Timeout: 40 * time.Second}

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
				go processUpdate(p.cfg.TelegramBotToken, p.db, update)
				if update.UpdateID >= offset {
					offset = update.UpdateID + 1
				}
			}
		}
	}
}

func (p *BotPoller) getUpdates(client *http.Client, offset int) ([]Update, error) {
	url := fmt.Sprintf(
		"https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30&allowed_updates=[\"message\",\"callback_query\"]",
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
