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
	return &WebhookHandler{cfg: cfg, db: db}
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

	if update.Message == nil || update.Message.Text == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	go h.processMessage(update.Message)
	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) processMessage(msg *Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	chatID := msg.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	text := strings.TrimSpace(msg.Text)

	// ── Commands (tanpa harus login) ──────────────────────────
	if strings.HasPrefix(text, "/start") {
		h.sendReply(chatID, menuText())
		return
	}
	if strings.HasPrefix(text, "/menu") || strings.HasPrefix(text, "/help") {
		h.sendReply(chatID, menuText())
		return
	}
	if strings.HasPrefix(text, "/link") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			h.sendReply(chatID, "❓ Format salah.\nGunakan: `/link [kode_verifikasi]`")
			return
		}
		h.handleLinking(ctx, chatID, chatIDStr, parts[1])
		return
	}

	// ── Commands yang butuh akun terhubung ────────────────────
	bindData, ok := h.getBinding(ctx, chatIDStr)
	if !ok {
		h.sendReply(chatID, "⚠️ Akun Telegram Anda belum terhubung.\n\nBuka dashboard FinTrack → Profil → *Hubungkan Telegram* untuk mendapatkan kode, lalu kirim:\n`/link [kode]`")
		return
	}

	if strings.HasPrefix(text, "/saldo") {
		h.handleSaldo(ctx, chatID, bindData.UserID)
		return
	}
	if strings.HasPrefix(text, "/summary") || strings.HasPrefix(text, "/rekap") {
		h.handleSummary(ctx, chatID, bindData.UserID)
		return
	}

	// ── Catat pengeluaran biasa ────────────────────────────────
	parsed, err := ParseMessage(text)
	if err != nil {
		h.sendReply(chatID, fmt.Sprintf("⚠️ %v\n\nFormat yang benar:\n`Beli kopi 25000 #makanan`\n\nKetik /menu untuk panduan lengkap.", err))
		return
	}

	_, err = h.db.ExecContext(ctx,
		`INSERT INTO transactions (user_id, category_name, amount, description, source)
		 VALUES ($1, $2, $3, $4, 'telegram')`,
		bindData.UserID, parsed.Category, parsed.Amount, parsed.Description,
	)
	if err != nil {
		log.Printf("Failed to save transaction: %v\n", err)
		h.sendReply(chatID, "❌ Gagal menyimpan transaksi. Coba lagi nanti.")
		return
	}

	h.sendReply(chatID, fmt.Sprintf(
		"✅ *Transaksi Dicatat!*\n\n📝 %s\n💰 *%s*\n🏷️ %s\n\nLihat selengkapnya di dashboard.",
		parsed.Description, formatRupiah(parsed.Amount), parsed.Category,
	))
}

// handleSaldo menampilkan saldo yang bisa dibelanjakan hari ini, minggu ini, bulan ini
func (h *WebhookHandler) handleSaldo(ctx context.Context, chatID int64, userID string) {
	spendable := getSpendableBalance(ctx, h.db, userID)
	h.sendReply(chatID, spendable)
}

// handleSummary menampilkan ringkasan pengeluaran bulan ini
func (h *WebhookHandler) handleSummary(ctx context.Context, chatID int64, userID string) {
	summary := getSpendingSummary(ctx, h.db, userID)
	h.sendReply(chatID, summary)
}

func (h *WebhookHandler) handleLinking(ctx context.Context, chatID int64, chatIDStr, code string) {
	log.Printf("Processing account link for chatID %s with code: %s\n", chatIDStr, code)

	var codeData struct {
		RecordID  string    `db:"id"`
		UserID    string    `db:"user_id"`
		ExpiresAt time.Time `db:"expires_at"`
	}
	err := h.db.QueryRowxContext(ctx,
		`SELECT id, user_id, expires_at FROM verification_codes WHERE code=$1`, code,
	).StructScan(&codeData)

	if errors.Is(err, sql.ErrNoRows) {
		h.sendReply(chatID, "❌ Kode verifikasi tidak ditemukan atau sudah tidak valid.")
		return
	}
	if err != nil {
		h.sendReply(chatID, "❌ Terjadi kegagalan memuat data verifikasi.")
		return
	}
	if time.Now().After(codeData.ExpiresAt) {
		h.sendReply(chatID, "❌ Kode verifikasi sudah kedaluwarsa. Generate kode baru di dashboard.")
		return
	}

	tx, err := h.db.BeginTxx(ctx, nil)
	if err != nil {
		h.sendReply(chatID, "❌ Gagal memulai proses penghubungan akun.")
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
		h.sendReply(chatID, "❌ Gagal menghubungkan akun. Coba lagi.")
		return
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM verification_codes WHERE id=$1`, codeData.RecordID)
	if err != nil {
		_ = tx.Rollback()
		h.sendReply(chatID, "❌ Gagal menghubungkan akun. Coba lagi.")
		return
	}

	if err := tx.Commit(); err != nil {
		h.sendReply(chatID, "❌ Gagal menghubungkan akun.")
		return
	}

	h.sendReply(chatID, "🎉 *Akun Berhasil Terhubung!*\n\n"+menuText())
}

type bindResult struct {
	UserID   string `db:"user_id"`
	IsActive bool   `db:"is_active"`
}

func (h *WebhookHandler) getBinding(ctx context.Context, chatIDStr string) (bindResult, bool) {
	var b bindResult
	err := h.db.QueryRowxContext(ctx,
		`SELECT user_id, is_active FROM telegram_binds WHERE chat_id=$1`, chatIDStr,
	).StructScan(&b)
	if err != nil || !b.IsActive {
		return bindResult{}, false
	}
	return b, true
}

func (h *WebhookHandler) sendReply(chatID int64, text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", h.cfg.TelegramBotToken)
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

// ── Shared helpers ────────────────────────────────────────────────────────────

func menuText() string {
	return `🤖 *FinTrack Bot — Menu Perintah*

📝 *Catat Pengeluaran:*
Kirim pesan dengan format:
` + "`" + `[Deskripsi] [Nominal] #[Kategori]` + "`" + `
Contoh: ` + "`" + `Beli kopi 25000 #makanan` + "`" + `

Tanpa kategori (otomatis _uncategorized_):
` + "`" + `Isi bensin 30000` + "`" + `

📊 *Perintah Tersedia:*
/saldo — Saldo yang bisa dibelanjakan hari ini
/summary — Ringkasan pengeluaran bulan ini
/menu — Tampilkan menu ini
/link [kode] — Hubungkan akun FinTrack
/help — Bantuan`
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

func getSpendableBalance(ctx context.Context, db *sqlx.DB, userID string) string {
	// Get user financial settings
	var income, goal int64
	_ = db.QueryRowContext(ctx,
		`SELECT monthly_income, wealth_goal FROM users WHERE id=$1`, userID,
	).Scan(&income, &goal)

	// Daily budget
	spendPct := float64(100-goal) / 100.0
	dailyBudget := int64(float64(income) * spendPct / 30)

	// Active fixed expenses
	var fixedTotal int64
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM fixed_expenses WHERE user_id=$1 AND is_active=TRUE`, userID,
	).Scan(&fixedTotal)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	weekStart := todayStart.AddDate(0, 0, -int(now.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	// Spending totals
	var todaySpend, weekSpend, monthSpend int64
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at >= $2`, userID, todayStart,
	).Scan(&todaySpend)
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at >= $2`, userID, weekStart,
	).Scan(&weekSpend)
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at >= $2`, userID, monthStart,
	).Scan(&monthSpend)

	daysInMonth := int64(time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day())
	monthlyBudget := int64(float64(income) * spendPct)
	weeklyBudget := dailyBudget * 7
	dailyDiscretionary := dailyBudget - fixedTotal

	spendableToday := dailyDiscretionary - todaySpend
	if spendableToday < 0 {
		spendableToday = 0
	}
	spendableWeek := weeklyBudget - (fixedTotal * int64(now.Weekday()+1)) - weekSpend
	if spendableWeek < 0 {
		spendableWeek = 0
	}
	spendableMonth := monthlyBudget - (fixedTotal * daysInMonth) - monthSpend
	if spendableMonth < 0 {
		spendableMonth = 0
	}

	return fmt.Sprintf(
		`💰 *Saldo yang Bisa Dibelanjakan*

🗓️ *Hari ini:* %s
📅 *Minggu ini:* %s  
📆 *Bulan ini:* %s

_(Setelah pengeluaran wajib: %s/hari)_`,
		formatRupiah(spendableToday),
		formatRupiah(spendableWeek),
		formatRupiah(spendableMonth),
		formatRupiah(fixedTotal),
	)
}

func getSpendingSummary(ctx context.Context, db *sqlx.DB, userID string) string {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	type catRow struct {
		CategoryName string `db:"category_name"`
		Total        int64  `db:"total"`
	}
	var cats []catRow
	_ = db.SelectContext(ctx, &cats,
		`SELECT category_name, COALESCE(SUM(amount),0) AS total
		 FROM transactions WHERE user_id=$1 AND created_at >= $2
		 GROUP BY category_name ORDER BY total DESC LIMIT 5`,
		userID, monthStart,
	)

	var totalMonth int64
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at >= $2`,
		userID, monthStart,
	).Scan(&totalMonth)

	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Ags", "Sep", "Okt", "Nov", "Des"}
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("📊 *Rekap %s %d*\n\n", monthNames[now.Month()-1], now.Year()))
	sb.WriteString(fmt.Sprintf("💸 Total: *%s*\n\n", formatRupiah(totalMonth)))
	sb.WriteString("🏷️ *Per Kategori:*\n")
	for _, c := range cats {
		sb.WriteString(fmt.Sprintf("  • %s: %s\n", strings.Title(c.CategoryName), formatRupiah(c.Total)))
	}
	if len(cats) == 0 {
		sb.WriteString("  Belum ada transaksi bulan ini.\n")
	}

	return sb.String()
}
