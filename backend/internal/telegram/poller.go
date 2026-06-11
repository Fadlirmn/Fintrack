package telegram

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fintrack-backend/config"
	"github.com/jmoiron/sqlx"
)

// BotPoller handles long polling requests to Telegram Bot API
type BotPoller struct {
	cfg *config.Config
	db  *sqlx.DB
}

// NewBotPoller instantiates a new BotPoller
func NewBotPoller(cfg *config.Config, db *sqlx.DB) *BotPoller {
	return &BotPoller{
		cfg: cfg,
		db:  db,
	}
}

type UpdateResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// Start runs the polling loop until the context is cancelled
func (p *BotPoller) Start(ctx context.Context) {
	log.Println("Starting Telegram Bot Poller loop...")
	offset := 0
	httpClient := &http.Client{
		Timeout: 40 * time.Second, // Must be longer than Telegram's polling timeout (30s)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Poller loop context cancelled. Exiting...")
			return
		default:
			updates, err := p.getUpdates(httpClient, offset)
			if err != nil {
				log.Printf("Error fetching updates from Telegram: %v. Retrying in 5 seconds...\n", err)
				time.Sleep(5 * time.Second)
				continue
			}

			for _, update := range updates {
				if update.Message != nil && update.Message.Text != "" {
					go p.processMessage(update.Message)
				}
				if update.UpdateID >= offset {
					offset = update.UpdateID + 1
				}
			}
		}
	}
}

func (p *BotPoller) getUpdates(client *http.Client, offset int) ([]Update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30", p.cfg.TelegramBotToken, offset)

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected Telegram response status: %s", resp.Status)
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

func (p *BotPoller) processMessage(msg *Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	chatID := msg.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	text := strings.TrimSpace(msg.Text)

	// 1. Handle command /start or /link
	if strings.HasPrefix(text, "/start") {
		p.sendTelegramReply(chatID, "Halo! Selamat datang di *FinTrack*.\n\nUntuk menghubungkan bot ini dengan akun dashboard Anda, ketik:\n`/link [kode_verifikasi]`")
		return
	}

	if strings.HasPrefix(text, "/link") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			p.sendTelegramReply(chatID, "Format salah. Gunakan perintah:\n`/link [kode_verifikasi]`")
			return
		}
		verificationCode := parts[1]
		p.handleLinking(ctx, chatID, chatIDStr, verificationCode)
		return
	}

	// 2. Query telegram_binds to ensure account is linked
	var bindData struct {
		UserID   string `db:"user_id"`
		IsActive bool   `db:"is_active"`
	}
	err := p.db.QueryRowxContext(ctx,
		`SELECT user_id, is_active FROM telegram_binds WHERE chat_id=$1`, chatIDStr,
	).StructScan(&bindData)

	if errors.Is(err, sql.ErrNoRows) {
		p.sendTelegramReply(chatID, "⚠️ Akun Telegram Anda belum terhubung.\nSilakan kunjungi dashboard web FinTrack untuk menghubungkan akun Telegram Anda.")
		return
	}
	if err != nil || !bindData.IsActive {
		p.sendTelegramReply(chatID, "⚠️ Terjadi kesalahan memuat data binding atau akun dinonaktifkan.")
		return
	}

	// 3. Parse expense format
	parsed, err := ParseMessage(text)
	if err != nil {
		p.sendTelegramReply(chatID, fmt.Sprintf("⚠️ %v", err))
		return
	}

	// 4. Save to PostgreSQL transactions table
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO transactions (user_id, category_name, amount, description, source)
		 VALUES ($1, $2, $3, $4, 'telegram')`,
		bindData.UserID, parsed.Category, parsed.Amount, parsed.Description,
	)
	if err != nil {
		log.Printf("Failed to write transaction to PostgreSQL: %v\n", err)
		p.sendTelegramReply(chatID, "❌ Gagal menyimpan transaksi. Silakan coba beberapa saat lagi.")
		return
	}

	formattedAmount := formatRupiah(parsed.Amount)
	p.sendTelegramReply(chatID, fmt.Sprintf("✅ *Transaksi Berhasil Dicatat!*\n\n📝 Deskripsi: %s\n💰 Jumlah: *%s*\n🏷️ Kategori: *%s*", parsed.Description, formattedAmount, parsed.Category))
}

func (p *BotPoller) handleLinking(ctx context.Context, chatID int64, chatIDStr, code string) {
	log.Printf("Handling link operation for chatID %s with code %s\n", chatIDStr, code)

	var codeData struct {
		RecordID  string    `db:"id"`
		UserID    string    `db:"user_id"`
		ExpiresAt time.Time `db:"expires_at"`
	}
	err := p.db.QueryRowxContext(ctx,
		`SELECT id, user_id, expires_at FROM verification_codes WHERE code=$1`, code,
	).StructScan(&codeData)

	if errors.Is(err, sql.ErrNoRows) {
		p.sendTelegramReply(chatID, "❌ Kode verifikasi tidak ditemukan.")
		return
	}
	if err != nil {
		p.sendTelegramReply(chatID, "❌ Gagal memuat data verifikasi.")
		return
	}

	if time.Now().After(codeData.ExpiresAt) {
		p.sendTelegramReply(chatID, "❌ Kode verifikasi telah kedaluwarsa.")
		return
	}

	// Use SQL transaction to atomically bind and delete code
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		p.sendTelegramReply(chatID, "❌ Gagal memulai proses penghubungan akun.")
		return
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO telegram_binds (chat_id, user_id, is_active)
		 VALUES ($1, $2, TRUE)
		 ON CONFLICT (chat_id) DO UPDATE SET user_id=EXCLUDED.user_id, is_active=TRUE`,
		chatIDStr, codeData.UserID,
	)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("Failed to bind account: %v\n", err)
		p.sendTelegramReply(chatID, "❌ Gagal menyimpan binding akun.")
		return
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM verification_codes WHERE id=$1`, codeData.RecordID)
	if err != nil {
		_ = tx.Rollback()
		p.sendTelegramReply(chatID, "❌ Gagal menyimpan binding akun.")
		return
	}

	if err := tx.Commit(); err != nil {
		p.sendTelegramReply(chatID, "❌ Gagal menghubungkan akun.")
		return
	}

	p.sendTelegramReply(chatID, "🎉 *Akun Berhasil Terhubung!*\n\nSekarang Anda dapat mencatat pengeluaran secara instan. Contoh: `Beli kopi 25000 #makanan`")
}

func (p *BotPoller) sendTelegramReply(chatID int64, text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", p.cfg.TelegramBotToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal telegram payload: %v\n", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Failed to make request to Telegram API: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API returned non-OK status: %s\n", resp.Status)
	}
}
