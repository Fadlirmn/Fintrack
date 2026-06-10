package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"fintrack-backend/config"
)

// WebhookHandler processes incoming update messages from Telegram webhook
type WebhookHandler struct {
	cfg *config.Config
	db  *firestore.Client
}

// NewWebhookHandler creates a new WebhookHandler instance
func NewWebhookHandler(cfg *config.Config, db *firestore.Client) *WebhookHandler {
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
		h.handleLinking(ctx, chatIDStr, verificationCode)
		return
	}

	// 2. Fetch linked account from telegram_binds
	bindDoc, err := h.db.Collection("telegram_binds").Doc(chatIDStr).Get(ctx)
	if err != nil {
		h.sendTelegramReply(chatID, "⚠️ Akun Telegram Anda belum terhubung.\nSilakan kunjungi dashboard web FinTrack untuk menghubungkan akun Telegram Anda.")
		return
	}

	var bindData struct {
		UserID   string `firestore:"user_id"`
		IsActive bool   `firestore:"is_active"`
	}
	if err := bindDoc.DataTo(&bindData); err != nil || !bindData.IsActive {
		h.sendTelegramReply(chatID, "⚠️ Akun Anda dinonaktifkan atau terjadi kesalahan sistem.")
		return
	}

	// 3. Parse expense record
	parsed, err := ParseMessage(text)
	if err != nil {
		h.sendTelegramReply(chatID, fmt.Sprintf("⚠️ %v", err))
		return
	}

	// 4. Save to Firestore transactions collection
	txRef := h.db.Collection("transactions").NewDoc()
	txData := map[string]interface{}{
		"user_id":       bindData.UserID,
		"category_name": parsed.Category,
		"amount":        parsed.Amount,
		"description":   parsed.Description,
		"source":        "telegram",
		"created_at":    time.Now(),
	}

	_, err = txRef.Set(ctx, txData)
	if err != nil {
		log.Printf("Failed to save transaction to Firestore: %v\n", err)
		h.sendTelegramReply(chatID, "❌ Gagal menyimpan transaksi. Silakan coba kembali nanti.")
		return
	}

	formattedAmount := formatRupiah(parsed.Amount)
	h.sendTelegramReply(chatID, fmt.Sprintf("✅ *Transaksi Berhasil Dicatat!*\n\n📝 Deskripsi: %s\n💰 Jumlah: *%s*\n🏷️ Kategori: *%s*\n\nData telah diperbarui di dashboard.", parsed.Description, formattedAmount, parsed.Category))
}

func (h *WebhookHandler) handleLinking(ctx context.Context, chatIDStr string, code string) {
	log.Printf("Processing account link request for chatID %s with verification code: %s\n", chatIDStr, code)

	// Find the verification code
	iter := h.db.Collection("verification_codes").Where("code", "==", code).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err != nil {
		h.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Kode verifikasi tidak ditemukan atau sudah tidak valid.")
		return
	}

	var codeData struct {
		UserID    string    `firestore:"user_id"`
		ExpiresAt time.Time `firestore:"expires_at"`
	}
	if err := doc.DataTo(&codeData); err != nil {
		h.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Terjadi kegagalan memuat data verifikasi.")
		return
	}

	if time.Now().After(codeData.ExpiresAt) {
		h.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Kode verifikasi ini sudah kedaluwarsa.")
		return
	}

	// Commit transactional changes
	batch := h.db.Batch()
	
	// Create bindings
	bindRef := h.db.Collection("telegram_binds").Doc(chatIDStr)
	batch.Set(bindRef, map[string]interface{}{
		"user_id":    codeData.UserID,
		"is_active":  true,
		"created_at": time.Now(),
	})

	// Clean up verification code
	batch.Delete(doc.Ref)

	err = func() error {
		_, commitErr := batch.Commit(ctx)
		return commitErr
	}()
	if err != nil {
		log.Printf("Failed to commit bind transaction batch: %v\n", err)
		h.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Gagal menghubungkan akun. Silakan coba lagi.")
		return
	}

	h.sendTelegramReply(mustParseInt64(chatIDStr), "🎉 *Akun Berhasil Terhubung!*\n\nAnda sekarang dapat mencatat pengeluaran langsung dengan format:\n`[Deskripsi] [Nominal] #[Kategori]`\nContoh: `Beli kopi 25000 #makanan`")
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

func mustParseInt64(s string) int64 {
	val, _ := strconv.ParseInt(s, 10, 64)
	return val
}
