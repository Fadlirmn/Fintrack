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
	return &BotPoller{cfg: cfg, db: db}
}

type UpdateResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// Start runs the polling loop until the context is cancelled
func (p *BotPoller) Start(ctx context.Context) {
	log.Println("Starting Telegram Bot Poller loop...")
	offset := 0
	httpClient := &http.Client{Timeout: 40 * time.Second}

	for {
		select {
		case <-ctx.Done():
			log.Println("Poller loop context cancelled. Exiting...")
			return
		default:
			updates, err := p.getUpdates(httpClient, offset)
			if err != nil {
				log.Printf("Error fetching updates from Telegram: %v. Retrying in 5s...\n", err)
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

	// ── Commands (tanpa login) ────────────────────────────────
	if strings.HasPrefix(text, "/start") {
		p.sendReply(chatID, menuText())
		return
	}
	if strings.HasPrefix(text, "/menu") || strings.HasPrefix(text, "/help") {
		p.sendReply(chatID, menuText())
		return
	}
	if strings.HasPrefix(text, "/link") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			p.sendReply(chatID, "❓ Format salah.\nGunakan: `/link [kode_verifikasi]`")
			return
		}
		p.handleLinking(ctx, chatID, chatIDStr, parts[1])
		return
	}

	// ── Commands yang butuh akun terhubung ────────────────────
	var b bindResult
	err := p.db.QueryRowxContext(ctx,
		`SELECT user_id, is_active FROM telegram_binds WHERE chat_id=$1`, chatIDStr,
	).StructScan(&b)

	if errors.Is(err, sql.ErrNoRows) || !b.IsActive {
		p.sendReply(chatID, "⚠️ Akun Telegram Anda belum terhubung.\n\nBuka dashboard FinTrack → Profil → *Hubungkan Telegram*, lalu kirim:\n`/link [kode]`")
		return
	}
	if err != nil {
		p.sendReply(chatID, "⚠️ Terjadi kesalahan sistem. Coba lagi nanti.")
		return
	}

	if strings.HasPrefix(text, "/saldo") {
		p.sendReply(chatID, getSpendableBalance(ctx, p.db, b.UserID))
		return
	}
	if strings.HasPrefix(text, "/summary") || strings.HasPrefix(text, "/rekap") {
		p.sendReply(chatID, getSpendingSummary(ctx, p.db, b.UserID))
		return
	}

	// ── Catat pengeluaran ─────────────────────────────────────
	parsed, err := ParseMessage(text)
	if err != nil {
		p.sendReply(chatID, fmt.Sprintf("⚠️ %v\n\nFormat: `Beli kopi 25000 #makanan`\nKetik /menu untuk panduan.", err))
		return
	}

	_, err = p.db.ExecContext(ctx,
		`INSERT INTO transactions (user_id, category_name, amount, description, source)
		 VALUES ($1, $2, $3, $4, 'telegram')`,
		b.UserID, parsed.Category, parsed.Amount, parsed.Description,
	)
	if err != nil {
		log.Printf("Failed to write transaction: %v\n", err)
		p.sendReply(chatID, "❌ Gagal menyimpan transaksi. Coba beberapa saat lagi.")
		return
	}

	p.sendReply(chatID, fmt.Sprintf(
		"✅ *Transaksi Dicatat!*\n\n📝 %s\n💰 *%s*\n🏷️ %s",
		parsed.Description, formatRupiah(parsed.Amount), parsed.Category,
	))
}

func (p *BotPoller) handleLinking(ctx context.Context, chatID int64, chatIDStr, code string) {
	log.Printf("Handling link for chatID %s with code %s\n", chatIDStr, code)

	var codeData struct {
		RecordID  string    `db:"id"`
		UserID    string    `db:"user_id"`
		ExpiresAt time.Time `db:"expires_at"`
	}
	err := p.db.QueryRowxContext(ctx,
		`SELECT id, user_id, expires_at FROM verification_codes WHERE code=$1`, code,
	).StructScan(&codeData)

	if errors.Is(err, sql.ErrNoRows) {
		p.sendReply(chatID, "❌ Kode verifikasi tidak ditemukan.")
		return
	}
	if err != nil {
		p.sendReply(chatID, "❌ Gagal memuat data verifikasi.")
		return
	}
	if time.Now().After(codeData.ExpiresAt) {
		p.sendReply(chatID, "❌ Kode verifikasi telah kedaluwarsa. Generate kode baru di dashboard.")
		return
	}

	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		p.sendReply(chatID, "❌ Gagal memulai proses penghubungan.")
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
		p.sendReply(chatID, "❌ Gagal menyimpan binding akun.")
		return
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM verification_codes WHERE id=$1`, codeData.RecordID)
	if err != nil {
		_ = tx.Rollback()
		p.sendReply(chatID, "❌ Gagal menghubungkan akun.")
		return
	}

	if err := tx.Commit(); err != nil {
		p.sendReply(chatID, "❌ Gagal menghubungkan akun.")
		return
	}

	p.sendReply(chatID, "🎉 *Akun Berhasil Terhubung!*\n\n"+menuText())
}

func (p *BotPoller) sendReply(chatID int64, text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", p.cfg.TelegramBotToken)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal telegram message: %v\n", err)
		return
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Failed to send telegram message: %v\n", err)
		return
	}
	defer resp.Body.Close()
}
