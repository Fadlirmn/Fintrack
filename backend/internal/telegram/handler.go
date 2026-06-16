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

	"fintrack-backend/config"
	"fintrack-backend/internal/gateway"
	"fintrack-backend/internal/telegram/parser"
)

// WebhookHandler processes incoming update messages from Telegram webhook.
// It NO LONGER has direct DB access — all data operations go through GatewayRouter.
type WebhookHandler struct {
	cfg    *config.Config
	router *gateway.GatewayRouter
}

func NewWebhookHandler(cfg *config.Config, router *gateway.GatewayRouter) *WebhookHandler {
	return &WebhookHandler{cfg: cfg, router: router}
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

	go h.processUpdate(update)
	w.WriteHeader(http.StatusOK)
}

// ── Shared Update Processor ───────────────────────────────────────────────────

func (h *WebhookHandler) processUpdate(update Update) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if update.CallbackQuery != nil {
		h.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}
	if update.Message != nil && update.Message.Text != "" {
		h.handleMessage(ctx, update.Message)
	}
}

// ── Callback Query Handler ─────────────────────────────────────────────────────

func (h *WebhookHandler) handleCallbackQuery(ctx context.Context, cq *CallbackQuery) {
	token := h.cfg.TelegramBotToken
	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID

	answerCallbackQuery(token, cq.ID, "")

	chatIDStr := strconv.FormatInt(chatID, 10)
	userID, isLinked := h.router.GetBinding(ctx, chatIDStr)

	switch cq.Data {
	case "btn_refresh", "btn_menu":
		name := cq.From.FirstName
		editMessage(token, chatID, msgID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))

	case "btn_saldo":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, h.router.GetBalance(ctx, userID), mainMenuKeyboard(true))

	case "btn_summary":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, h.router.GetSummary(ctx, userID), mainMenuKeyboard(true))

	case "btn_panduan":
		editMessage(token, chatID, msgID, guideText(), mainMenuKeyboard(isLinked))

	case "btn_cara_link":
		editMessage(token, chatID, msgID, linkGuideText(), mainMenuKeyboard(false))

	// ── Home Server callbacks ─────────────────────────────────────────────
	case "btn_server_status":
		editMessage(token, chatID, msgID, h.router.ServerStatus(ctx), serverMenuKeyboard())

	case "btn_server_resources":
		editMessage(token, chatID, msgID, h.router.ServerResources(ctx), serverMenuKeyboard())

	case "btn_server_devices":
		editMessage(token, chatID, msgID, h.router.ServerDevices(ctx), serverMenuKeyboard())
	}
}

// ── Message Handler ───────────────────────────────────────────────────────────

func (h *WebhookHandler) handleMessage(ctx context.Context, msg *Message) {
	token := h.cfg.TelegramBotToken
	chatID := msg.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)
	text := strings.TrimSpace(msg.Text)
	name := msg.From.FirstName

	// ── Public commands (no login required) ──────────────────────────────
	switch {
	case strings.HasPrefix(text, "/start"):
		_, isLinked := h.router.GetBinding(ctx, chatIDStr)
		sendWithKeyboard(token, chatID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))
		return

	case strings.HasPrefix(text, "/menu"), strings.HasPrefix(text, "/help"):
		_, isLinked := h.router.GetBinding(ctx, chatIDStr)
		sendWithKeyboard(token, chatID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))
		return

	case strings.HasPrefix(text, "/link"):
		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendReply(token, chatID, "❓ Format salah.\nGunakan: `/link [kode_verifikasi]`\n\nDapatkan kode di dashboard FinTrack → Profil → Telegram.")
			return
		}
		reply := h.router.LinkAccount(ctx, chatIDStr, parts[1], name)
		_, isLinked := h.router.GetBinding(ctx, chatIDStr)
		sendWithKeyboard(token, chatID, reply, mainMenuKeyboard(isLinked))
		return

	// ── Server commands (no FinTrack account needed) ──────────────────
	case strings.HasPrefix(text, "/server"):
		parts := strings.Fields(text)
		subcmd := ""
		if len(parts) > 1 {
			subcmd = parts[1]
		}
		switch subcmd {
		case "status":
			sendReply(token, chatID, h.router.ServerStatus(ctx))
		case "resources":
			sendReply(token, chatID, h.router.ServerResources(ctx))
		case "devices":
			sendReply(token, chatID, h.router.ServerDevices(ctx))
		default:
			sendWithKeyboard(token, chatID, "🖥️ *Home Server*\nPilih aksi:", serverMenuKeyboard())
		}
		return

	case strings.HasPrefix(text, "/pc"):
		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendReply(token, chatID, "❓ Format: `/pc sleep|shutdown|reboot`")
			return
		}
		action := strings.ToLower(parts[1])
		allowed := map[string]bool{"sleep": true, "shutdown": true, "reboot": true}
		if !allowed[action] {
			sendReply(token, chatID, "❌ Aksi tidak dikenal. Gunakan: `sleep`, `shutdown`, atau `reboot`.")
			return
		}
		sendReply(token, chatID, h.router.PCAction(ctx, action))
		return

	case strings.HasPrefix(text, "/run"):
		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendReply(token, chatID, "❓ Format: `/run [nama_script]`")
			return
		}
		sendReply(token, chatID, h.router.RunScript(ctx, parts[1]))
		return
	}

	// ── Commands that require linked FinTrack account ────────────────────
	userID, isLinked := h.router.GetBinding(ctx, chatIDStr)
	if !isLinked {
		sendWithKeyboard(token, chatID, notLinkedText(), mainMenuKeyboard(false))
		return
	}

	switch {
	case strings.HasPrefix(text, "/saldo"):
		sendReply(token, chatID, h.router.GetBalance(ctx, userID))

	case strings.HasPrefix(text, "/summary"), strings.HasPrefix(text, "/rekap"):
		sendReply(token, chatID, h.router.GetSummary(ctx, userID))

	default:
		// Default: attempt to parse as expense entry
		parsed, err := parser.ParseMessage(text)
		if err != nil {
			sendReply(token, chatID, fmt.Sprintf("⚠️ %v\n\nFormat: `Beli kopi 25000 #makanan`\nAtau ketik /menu.", err))
			return
		}
		reply := h.router.SaveTransaction(ctx, userID, parsed.Description, parsed.Category, parsed.Amount)
		sendWithKeyboard(token, chatID, reply, afterSaveKeyboard())
	}
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
		{
			{"text": "🖥️ Server", "callback_data": "btn_server_status"},
		},
	}
	if !isLinked {
		rows = append(rows, []map[string]string{
			{"text": "🔗 Cara Hubungkan Akun", "callback_data": "btn_cara_link"},
		})
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

func serverMenuKeyboard() map[string]interface{} {
	return map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "📊 Resources", "callback_data": "btn_server_resources"},
				{"text": "📡 Devices", "callback_data": "btn_server_devices"},
			},
			{
				{"text": "🖥️ Status", "callback_data": "btn_server_status"},
				{"text": "🏠 Menu Utama", "callback_data": "btn_menu"},
			},
		},
	}
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
		"`/server` — Kontrol home server\n" +
		"`/pc sleep|shutdown|reboot` — Kontrol PC\n" +
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

// SetMyCommands registers bot commands with BotFather.
func SetMyCommands(token string) {
	callTelegramAPI(token, "setMyCommands", map[string]interface{}{
		"commands": []map[string]string{
			{"command": "start", "description": "Buka menu utama"},
			{"command": "menu", "description": "Tampilkan menu interaktif"},
			{"command": "saldo", "description": "Lihat saldo yang bisa dibelanjakan"},
			{"command": "summary", "description": "Rekap pengeluaran bulan ini"},
			{"command": "link", "description": "Hubungkan akun: /link [kode]"},
			{"command": "server", "description": "Kontrol home server: /server status|resources|devices"},
			{"command": "pc", "description": "Kontrol PC: /pc sleep|shutdown|reboot"},
			{"command": "run", "description": "Jalankan script: /run [nama]"},
			{"command": "help", "description": "Panduan format pencatatan"},
		},
	})
	log.Println("[TG] Bot commands registered via setMyCommands")
}
