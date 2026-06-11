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

func NewWebhookHandler(cfg *config.Config, db *sqlx.DB) *WebhookHandler {
	return &WebhookHandler{cfg: cfg, db: db}
}

// ── Telegram Types ────────────────────────────────────────────────────────────

type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	From      TGUser `json:"from"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type TGUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    TGUser   `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// ── HTTP Handler ─────────────────────────────────────────────────────────────

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.TelegramSecretToken != "" {
		if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != h.cfg.TelegramSecretToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var update Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	go processUpdate(h.cfg.TelegramBotToken, h.db, update)
	w.WriteHeader(http.StatusOK)
}

// ── Shared Update Processor ───────────────────────────────────────────────────

func processUpdate(token string, db *sqlx.DB, update Update) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Handle callback_query (tombol inline keyboard diklik)
	if update.CallbackQuery != nil {
		handleCallbackQuery(ctx, token, db, update.CallbackQuery)
		return
	}

	// Handle pesan biasa
	if update.Message != nil && update.Message.Text != "" {
		handleMessage(ctx, token, db, update.Message)
	}
}

// ── Callback Query Handler ─────────────────────────────────────────────────────

func handleCallbackQuery(ctx context.Context, token string, db *sqlx.DB, cq *CallbackQuery) {
	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID
	data := cq.Data

	// Selalu answer callback query agar spinner tombol berhenti
	answerCallbackQuery(token, cq.ID, "")

	// Cek apakah user sudah linked
	chatIDStr := strconv.FormatInt(chatID, 10)
	b, isLinked := getBinding(ctx, db, chatIDStr)

	switch data {
	case "btn_refresh", "btn_menu":
		name := cq.From.FirstName
		editMessage(token, chatID, msgID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))

	case "btn_saldo":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, getSpendableBalance(ctx, db, b.UserID), mainMenuKeyboard(true))

	case "btn_summary":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, getSpendingSummary(ctx, db, b.UserID), mainMenuKeyboard(true))

	case "btn_panduan":
		editMessage(token, chatID, msgID, guideText(), mainMenuKeyboard(isLinked))

	case "btn_cara_link":
		editMessage(token, chatID, msgID, linkGuideText(), mainMenuKeyboard(false))
	}
}

// ── Message Handler ───────────────────────────────────────────────────────────

func handleMessage(ctx context.Context, token string, db *sqlx.DB, msg *Message) {
	chatID := msg.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	text := strings.TrimSpace(msg.Text)
	name := msg.From.FirstName

	// Commands tanpa perlu login
	switch {
	case strings.HasPrefix(text, "/start"):
		_, isLinked := getBinding(ctx, db, chatIDStr)
		sendWithKeyboard(token, chatID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))
		return

	case strings.HasPrefix(text, "/menu"), strings.HasPrefix(text, "/help"):
		_, isLinked := getBinding(ctx, db, chatIDStr)
		sendWithKeyboard(token, chatID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))
		return

	case strings.HasPrefix(text, "/link"):
		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendReply(token, chatID, "❓ Format salah.\nGunakan: `/link [kode_verifikasi]`\n\nDapatkan kode di dashboard FinTrack → Profil → Telegram.")
			return
		}
		handleLinking(ctx, token, db, chatID, chatIDStr, parts[1], name)
		return
	}

	// Commands yang butuh akun terhubung
	b, isLinked := getBinding(ctx, db, chatIDStr)
	if !isLinked {
		sendWithKeyboard(token, chatID, notLinkedText(), mainMenuKeyboard(false))
		return
	}

	switch {
	case strings.HasPrefix(text, "/saldo"):
		sendReply(token, chatID, getSpendableBalance(ctx, db, b.UserID))
	case strings.HasPrefix(text, "/summary"), strings.HasPrefix(text, "/rekap"):
		sendReply(token, chatID, getSpendingSummary(ctx, db, b.UserID))
	default:
		// Catat pengeluaran biasa
		parsed, err := ParseMessage(text)
		if err != nil {
			sendReply(token, chatID, fmt.Sprintf("⚠️ %v\n\nFormat: `Beli kopi 25000 #makanan`\nAtau ketik /menu.", err))
			return
		}
		_, err = db.ExecContext(ctx,
			`INSERT INTO transactions (user_id, category_name, amount, description, source) VALUES ($1,$2,$3,$4,'telegram')`,
			b.UserID, parsed.Category, parsed.Amount, parsed.Description,
		)
		if err != nil {
			log.Printf("Failed to save transaction: %v", err)
			sendReply(token, chatID, "❌ Gagal menyimpan. Coba lagi nanti.")
			return
		}
		sendWithKeyboard(token, chatID,
			fmt.Sprintf("✅ *Transaksi Dicatat!*\n\n📝 %s\n💰 *%s*\n🏷️ _%s_",
				parsed.Description, formatRupiah(parsed.Amount), parsed.Category),
			afterSaveKeyboard(),
		)
	}
}

// ── Linking ───────────────────────────────────────────────────────────────────

func handleLinking(ctx context.Context, token string, db *sqlx.DB, chatID int64, chatIDStr, code, name string) {
	var codeData struct {
		RecordID  string    `db:"id"`
		UserID    string    `db:"user_id"`
		ExpiresAt time.Time `db:"expires_at"`
	}
	err := db.QueryRowxContext(ctx,
		`SELECT id, user_id, expires_at FROM verification_codes WHERE code=$1`, code,
	).StructScan(&codeData)

	if errors.Is(err, sql.ErrNoRows) {
		sendReply(token, chatID, "❌ Kode tidak ditemukan atau sudah tidak valid.")
		return
	}
	if err != nil || time.Now().After(codeData.ExpiresAt) {
		sendReply(token, chatID, "❌ Kode kedaluwarsa. Generate kode baru di dashboard → Profil → Telegram.")
		return
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		sendReply(token, chatID, "❌ Gagal memulai proses penghubungan.")
		return
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO telegram_binds (chat_id, user_id, is_active) VALUES ($1,$2,TRUE) ON CONFLICT (chat_id) DO UPDATE SET user_id=EXCLUDED.user_id, is_active=TRUE`,
		chatIDStr, codeData.UserID,
	)
	if err != nil {
		_ = tx.Rollback()
		sendReply(token, chatID, "❌ Gagal menyimpan binding akun.")
		return
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM verification_codes WHERE id=$1`, codeData.RecordID)
	if err != nil {
		_ = tx.Rollback()
		sendReply(token, chatID, "❌ Gagal menghubungkan akun.")
		return
	}
	if err := tx.Commit(); err != nil {
		sendReply(token, chatID, "❌ Gagal menghubungkan akun.")
		return
	}

	sendWithKeyboard(token, chatID, fmt.Sprintf(
		"🎉 *Berhasil Terhubung, %s\\!*\n\nAkun FinTrack kamu sudah tersambung\\. Mulai catat pengeluaran dari sini\\!",
		name,
	), mainMenuKeyboard(true))
}

// ── Keyboards ─────────────────────────────────────────────────────────────────

func mainMenuKeyboard(isLinked bool) map[string]interface{} {
	rows := [][]map[string]string{
		{
			{"text": "💰 Saldo Hari Ini", "callback_data": "btn_saldo"},
			{"text": "📊 Rekap Bulan", "callback_data": "btn_summary"},
		},
		{
			{"text": "📋 Panduan Catat", "callback_data": "btn_panduan"},
			{"text": "🔄 Refresh", "callback_data": "btn_refresh"},
		},
	}
	if !isLinked {
		rows = append(rows, []map[string]string{
			{"text": "🔗 Cara Hubungkan Akun", "callback_data": "btn_cara_link"},
		})
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

func afterSaveKeyboard() map[string]interface{} {
	return map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "💰 Lihat Saldo", "callback_data": "btn_saldo"},
				{"text": "📊 Rekap Bulan", "callback_data": "btn_summary"},
			},
			{
				{"text": "🏠 Menu Utama", "callback_data": "btn_menu"},
			},
		},
	}
}

// ── Text Templates ────────────────────────────────────────────────────────────

func welcomeText(name string, isLinked bool) string {
	status := "🔴 *Belum terhubung*"
	if isLinked {
		status = "🟢 *Akun terhubung*"
	}
	greeting := "Hei"
	if name != "" {
		greeting = "Hei, " + name
	}
	return fmt.Sprintf(
		"🏦 *FinTrack Bot*\n━━━━━━━━━━━━━━━━\n%s!\nStatus: %s\n━━━━━━━━━━━━━━━━\n\n"+
			"Catat pengeluaran langsung di sini dengan format:\n`Beli kopi 25000 #makanan`\n\n"+
			"Pilih aksi di bawah:",
		greeting, status,
	)
}

func notLinkedText() string {
	return "⚠️ *Akun Telegram belum terhubung*\n\nBuka dashboard FinTrack → Profil → *Telegram*, generate kode, lalu kirim:\n`/link [kode]`\n\nAtau klik tombol di bawah untuk panduan."
}

func linkGuideText() string {
	return "🔗 *Cara Menghubungkan Akun*\n\n" +
		"1️⃣ Buka dashboard FinTrack di browser\n" +
		"2️⃣ Buka tab *Profil* → pilih *Telegram*\n" +
		"3️⃣ Klik *Generate Kode Tautan Baru*\n" +
		"4️⃣ Salin kode yang muncul\n" +
		"5️⃣ Kirim ke sini: `/link KODE_KAMU`\n\n" +
		"_Kode berlaku 10 menit setelah dibuat._"
}

func guideText() string {
	return "📋 *Panduan Mencatat Pengeluaran*\n\n" +
		"*Format dasar:*\n" +
		"`[Deskripsi] [Nominal] #[Kategori]`\n\n" +
		"*Contoh:*\n" +
		"`Beli kopi 25000 #makanan`\n" +
		"`Isi bensin 50000 #transportasi`\n" +
		"`Bayar Netflix 60000 #hiburan`\n\n" +
		"*Tanpa kategori:*\n" +
		"`Parkir 5000`  → auto: _uncategorized_\n\n" +
		"*Perintah tersedia:*\n" +
		"`/saldo` — Saldo yang bisa dibelanjakan\n" +
		"`/summary` — Rekap pengeluaran bulan ini\n" +
		"`/menu` — Kembali ke menu utama"
}

// ── API Helpers ────────────────────────────────────────────────────────────────

func sendReply(token string, chatID int64, text string) {
	callTelegramAPI(token, "sendMessage", map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
}

func sendWithKeyboard(token string, chatID int64, text string, keyboard map[string]interface{}) {
	callTelegramAPI(token, "sendMessage", map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "Markdown",
		"reply_markup": keyboard,
	})
}

func editMessage(token string, chatID int64, msgID int, text string, keyboard map[string]interface{}) {
	callTelegramAPI(token, "editMessageText", map[string]interface{}{
		"chat_id":      chatID,
		"message_id":   msgID,
		"text":         text,
		"parse_mode":   "Markdown",
		"reply_markup": keyboard,
	})
}

func answerCallbackQuery(token, queryID, text string) {
	payload := map[string]interface{}{"callback_query_id": queryID}
	if text != "" {
		payload["text"] = text
	}
	callTelegramAPI(token, "answerCallbackQuery", payload)
}

func callTelegramAPI(token, method string, payload map[string]interface{}) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[TG] Marshal error for %s: %v", method, err)
		return
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("[TG] HTTP error for %s: %v", method, err)
		return
	}
	defer resp.Body.Close()
}

// SetMyCommands mendaftarkan daftar perintah ke BotFather agar muncul di menu /
func SetMyCommands(token string) {
	callTelegramAPI(token, "setMyCommands", map[string]interface{}{
		"commands": []map[string]string{
			{"command": "start", "description": "Buka menu utama FinTrack"},
			{"command": "menu", "description": "Tampilkan menu interaktif"},
			{"command": "saldo", "description": "Lihat saldo yang bisa dibelanjakan"},
			{"command": "summary", "description": "Rekap pengeluaran bulan ini"},
			{"command": "link", "description": "Hubungkan akun: /link [kode]"},
			{"command": "help", "description": "Panduan format pencatatan"},
		},
	})
	log.Println("[TG] Bot commands registered via setMyCommands")
}

// ── Binding Helper ─────────────────────────────────────────────────────────────

type bindResult struct {
	UserID   string `db:"user_id"`
	IsActive bool   `db:"is_active"`
}

func getBinding(ctx context.Context, db *sqlx.DB, chatIDStr string) (bindResult, bool) {
	var b bindResult
	err := db.QueryRowxContext(ctx,
		`SELECT user_id, is_active FROM telegram_binds WHERE chat_id=$1`, chatIDStr,
	).StructScan(&b)
	if err != nil || !b.IsActive {
		return bindResult{}, false
	}
	return b, true
}

// ── Shared Business Logic ─────────────────────────────────────────────────────

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
	var income, goal int64
	_ = db.QueryRowContext(ctx, `SELECT monthly_income, wealth_goal FROM users WHERE id=$1`, userID).Scan(&income, &goal)

	spendPct := float64(100-goal) / 100.0
	dailyBudget := int64(float64(income) * spendPct / 30)

	var fixedTotal int64
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM fixed_expenses WHERE user_id=$1 AND is_active=TRUE`, userID,
	).Scan(&fixedTotal)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	weekStart := todayStart.AddDate(0, 0, -int(now.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	var todaySpend, weekSpend, monthSpend int64
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, todayStart).Scan(&todaySpend)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, weekStart).Scan(&weekSpend)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, monthStart).Scan(&monthSpend)

	daysInMonth := int64(time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day())
	monthlyBudget := int64(float64(income) * spendPct)
	weeklyBudget := dailyBudget * 7

	spendableToday := max64(dailyBudget-fixedTotal-todaySpend, 0)
	spendableWeek := max64(weeklyBudget-(fixedTotal*int64(now.Weekday()+1))-weekSpend, 0)
	spendableMonth := max64(monthlyBudget-(fixedTotal*daysInMonth)-monthSpend, 0)

	return fmt.Sprintf(
		"💰 *Saldo yang Bisa Dibelanjakan*\n━━━━━━━━━━━━━━━━\n"+
			"☀️ *Hari ini:*   %s\n"+
			"📅 *Minggu ini:* %s\n"+
			"📆 *Bulan ini:*  %s\n"+
			"━━━━━━━━━━━━━━━━\n"+
			"_(Pengeluaran wajib aktif: %s/hari)_",
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
		`SELECT category_name, COALESCE(SUM(amount),0) AS total FROM transactions WHERE user_id=$1 AND created_at>=$2 GROUP BY category_name ORDER BY total DESC LIMIT 5`,
		userID, monthStart,
	)

	var totalMonth int64
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, monthStart,
	).Scan(&totalMonth)

	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Ags", "Sep", "Okt", "Nov", "Des"}
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("📊 *Rekap %s %d*\n━━━━━━━━━━━━━━━━\n", monthNames[now.Month()-1], now.Year()))
	sb.WriteString(fmt.Sprintf("💸 *Total:* %s\n\n", formatRupiah(totalMonth)))
	sb.WriteString("🏷️ *Top Kategori:*\n")
	for i, c := range cats {
		medals := []string{"🥇", "🥈", "🥉", "4️⃣", "5️⃣"}
		medal := medals[i]
		sb.WriteString(fmt.Sprintf("  %s %s: *%s*\n", medal, strings.Title(c.CategoryName), formatRupiah(c.Total)))
	}
	if len(cats) == 0 {
		sb.WriteString("  Belum ada transaksi bulan ini.\n")
	}
	return sb.String()
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func menuText() string { return guideText() }
