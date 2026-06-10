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

// BotPoller handles long polling requests to Telegram Bot API
type BotPoller struct {
	cfg *config.Config
	db  *firestore.Client
}

// NewBotPoller instantiates a new BotPoller
func NewBotPoller(cfg *config.Config, db *firestore.Client) *BotPoller {
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
		p.handleLinking(ctx, chatIDStr, verificationCode)
		return
	}

	// 2. Query telegram_binds to ensure account is linked
	bindDoc, err := p.db.Collection("telegram_binds").Doc(chatIDStr).Get(ctx)
	if err != nil {
		p.sendTelegramReply(chatID, "⚠️ Akun Telegram Anda belum terhubung.\nSilakan kunjungi dashboard web FinTrack untuk menghubungkan akun Telegram Anda.")
		return
	}

	var bindData struct {
		UserID   string `firestore:"user_id"`
		IsActive bool   `firestore:"is_active"`
	}
	if err := bindDoc.DataTo(&bindData); err != nil || !bindData.IsActive {
		p.sendTelegramReply(chatID, "⚠️ Terjadi kesalahan memuat data binding atau akun dinonaktifkan.")
		return
	}

	// 3. Parse expense format
	parsed, err := ParseMessage(text)
	if err != nil {
		p.sendTelegramReply(chatID, fmt.Sprintf("⚠️ %v", err))
		return
	}

	// 4. Save directly to Firestore
	txRef := p.db.Collection("transactions").NewDoc()
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
		log.Printf("Failed to write transaction to Firestore: %v\n", err)
		p.sendTelegramReply(chatID, "❌ Gagal menyimpan transaksi. Silakan coba beberapa saat lagi.")
		return
	}

	formattedAmount := formatRupiah(parsed.Amount)
	p.sendTelegramReply(chatID, fmt.Sprintf("✅ *Transaksi Berhasil Dicatat!*\n\n📝 Deskripsi: %s\n💰 Jumlah: *%s*\n🏷️ Kategori: *%s*", parsed.Description, formattedAmount, parsed.Category))
}

func (p *BotPoller) handleLinking(ctx context.Context, chatIDStr string, code string) {
	log.Printf("Handling link operation for chatID %s with code %s\n", chatIDStr, code)

	iter := p.db.Collection("verification_codes").Where("code", "==", code).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err != nil {
		p.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Kode verifikasi tidak ditemukan.")
		return
	}

	var codeData struct {
		UserID    string    `firestore:"user_id"`
		ExpiresAt time.Time `firestore:"expires_at"`
	}
	if err := doc.DataTo(&codeData); err != nil {
		p.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Gagal memuat data verifikasi.")
		return
	}

	if time.Now().After(codeData.ExpiresAt) {
		p.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Kode verifikasi telah kedaluwarsa.")
		return
	}

	batch := p.db.Batch()
	
	// Create mapping doc
	bindRef := p.db.Collection("telegram_binds").Doc(chatIDStr)
	batch.Set(bindRef, map[string]interface{}{
		"user_id":    codeData.UserID,
		"is_active":  true,
		"created_at": time.Now(),
	})

	// Delete verification code
	batch.Delete(doc.Ref)

	err = func() error {
		_, commitErr := batch.Commit(ctx)
		return commitErr
	}()
	if err != nil {
		log.Printf("Failed to bind account: %v\n", err)
		p.sendTelegramReply(mustParseInt64(chatIDStr), "❌ Gagal menyimpan binding akun.")
		return
	}

	p.sendTelegramReply(mustParseInt64(chatIDStr), "🎉 *Akun Berhasil Terhubung!*\n\nSekarang Anda dapat mencatat pengeluaran secara instan. Contoh: `Beli kopi 25000 #makanan`")
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
