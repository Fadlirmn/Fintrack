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

// WebhookHandler processes incoming update messages from Telegram webhook
type WebhookHandler struct {
	cfg *config.Config
	db  *sqlx.DB
}

// NewWebhookHandler creates a new WebhookHandler instance
func NewWebhookHandler(cfg *config.Config, db *sqlx.DB) *WebhookHandler {
	return &WebhookHandler{
		cfg: cfg,
		db:  db,
	}
}

// Telegram Update Structs
type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type Chat struct {
	ID int64 `json:"id"`
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify X-Telegram-Bot-Api-Secret-Token to secure webhook endpoint
	secretToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if h.cfg.TelegramSecretToken != "" && secretToken != h.cfg.TelegramSecretToken {
		http.Error(w, "Unauthorized request source", http.StatusUnauthorized)
		return
	}

	var update Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Error decoding Telegram update payload: %v\n", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// If update is empty or not containing text, respond OK and return
	if update.Message == nil || update.Message.Text == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Run processing asynchronously to fulfill the < 2 seconds response constraint
	go h.processMessage(update.Message)

	// Acknowledge update receipt immediately to Telegram
	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) processMessage(msg *Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	chatID := msg.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	text := strings.TrimSpace(msg.Text)

	// 1. Handle commands: /start or /link
	if strings.HasPrefix(text, "/start") {
		h.sendTelegramReply(chatID, "Halo! Selamat datang di *FinTrack*.\n\nUntuk mulai mencatat pengeluaran, hubungkan akun Telegram ini di dashboard web FinTrack terlebih dahulu menggunakan perintah:\n`/link [kode_verifikasi]`")
		return
	}

	if strings.HasPrefix(text, "/link") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			h.sendTelegramReply(chatID, "Format salah. Gunakan perintah:\n`/link [kode_verifikasi]`")
			return
		}
		verificationCode := parts[1]
		h.handleLinking(ctx, chatID, chatIDStr, verificationCode)
		return
	}

	// 2. Fetch linked account from telegram_binds
	var bindData struct {
		UserID   string `db:"user_id"`
		IsActive bool   `db:"is_active"`
	}
	err := h.db.QueryRowxContext(ctx,
		`SELECT user_id, is_active FROM telegram_binds WHERE chat_id=$1`, chatIDStr,
	).StructScan(&bindData)

	if errors.Is(err, sql.ErrNoRows) {
		h.sendTelegramReply(chatID, "⚠️ Akun Telegram Anda belum terhubung.\nSilakan kunjungi dashboard web FinTrack untuk menghubungkan akun Telegram Anda.")
		return
	}
	if err != nil || !bindData.IsActive {
		h.sendTelegramReply(chatID, "⚠️ Akun Anda dinonaktifkan atau terjadi kesalahan sistem.")
		return
	}

	// 3. Parse expense record
	parsed, err := ParseMessage(text)
	if err != nil {
		h.sendTelegramReply(chatID, fmt.Sprintf("⚠️ %v", err))
		return
	}

	// 4. Save to transactions table
	_, err = h.db.ExecContext(ctx,
		`INSERT INTO transactions (user_id, category_name, amount, description, source)
		 VALUES ($1, $2, $3, $4, 'telegram')`,
		bindData.UserID, parsed.Category, parsed.Amount, parsed.Description,
	)
	if err != nil {
		log.Printf("Failed to save transaction to PostgreSQL: %v\n", err)
		h.sendTelegramReply(chatID, "❌ Gagal menyimpan transaksi. Silakan coba kembali nanti.")
		return
	}

	formattedAmount := formatRupiah(parsed.Amount)
	h.sendTelegramReply(chatID, fmt.Sprintf("✅ *Transaksi Berhasil Dicatat!*\n\n📝 Deskripsi: %s\n💰 Jumlah: *%s*\n🏷️ Kategori: *%s*\n\nData telah diperbarui di dashboard.", parsed.Description, formattedAmount, parsed.Category))
}

func (h *WebhookHandler) handleLinking(ctx context.Context, chatID int64, chatIDStr, code string) {
	log.Printf("Processing account link request for chatID %s with verification code: %s\n", chatIDStr, code)

	// Find the verification code
	var codeData struct {
		UserID    string    `db:"user_id"`
		ExpiresAt time.Time `db:"expires_at"`
		RecordID  string    `db:"id"`
	}
	err := h.db.QueryRowxContext(ctx,
		`SELECT id, user_id, expires_at FROM verification_codes WHERE code=$1`, code,
	).StructScan(&codeData)

	if errors.Is(err, sql.ErrNoRows) {
		h.sendTelegramReply(chatID, "❌ Kode verifikasi tidak ditemukan atau sudah tidak valid.")
		return
	}
	if err != nil {
		h.sendTelegramReply(chatID, "❌ Terjadi kegagalan memuat data verifikasi.")
		return
	}

	if time.Now().After(codeData.ExpiresAt) {
		h.sendTelegramReply(chatID, "❌ Kode verifikasi ini sudah kedaluwarsa.")
		return
	}

	// Use a database transaction to atomically bind and delete the code
	tx, err := h.db.BeginTxx(ctx, nil)
	if err != nil {
		h.sendTelegramReply(chatID, "❌ Gagal memulai proses penghubungan akun.")
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
		log.Printf("Failed to upsert telegram_binds: %v\n", err)
		h.sendTelegramReply(chatID, "❌ Gagal menghubungkan akun. Silakan coba lagi.")
		return
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM verification_codes WHERE id=$1`, codeData.RecordID)
	if err != nil {
		_ = tx.Rollback()
		h.sendTelegramReply(chatID, "❌ Gagal menghubungkan akun. Silakan coba lagi.")
		return
	}

	if err := tx.Commit(); err != nil {
		h.sendTelegramReply(chatID, "❌ Gagal menghubungkan akun. Silakan coba lagi.")
		return
	}

	h.sendTelegramReply(chatID, "🎉 *Akun Berhasil Terhubung!*\n\nAnda sekarang dapat mencatat pengeluaran langsung dengan format:\n`[Deskripsi] [Nominal] #[Kategori]`\nContoh: `Beli kopi 25000 #makanan`")
}

func (h *WebhookHandler) sendTelegramReply(chatID int64, text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", h.cfg.TelegramBotToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal telegram sendMessage request: %v\n", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Failed to post message to Telegram API: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API request failed with status: %s\n", resp.Status)
	}
}

func formatRupiah(amount int64) string {
	s := strconv.FormatInt(amount, 10)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return "Rp " + strings.Join(parts, ".")
}
