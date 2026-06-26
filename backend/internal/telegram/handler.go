package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
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
	MessageID int         `json:"message_id"`
	Chat      Chat        `json:"chat"`
	Text      string      `json:"text"`
	From      TGUser      `json:"from"`
	Photo     []PhotoSize `json:"photo"`
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size"`
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second) // boost to 45s for LLM OCR
	defer cancel()

	if update.CallbackQuery != nil {
		h.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}
	if update.Message != nil {
		if update.Message.Text != "" {
			h.handleMessage(ctx, update.Message)
		} else if len(update.Message.Photo) > 0 {
			h.handlePhoto(ctx, update.Message)
		}
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

	// ── n8n callbacks ─────────────────────────────────────────────────────
	case "btn_n8n_menu":
		editMessage(token, chatID, msgID, n8nMenuText(), n8nMenuKeyboard())

	case "btn_n8n_list":
		editMessage(token, chatID, msgID, h.router.N8NListWorkflows(ctx), n8nMenuKeyboard())
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

	// ── Server commands ───────────────────────────────────────────────────
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

	// ── n8n commands ──────────────────────────────────────────────────────
	case strings.HasPrefix(text, "/n8n"):
		parts := strings.Fields(text)
		subcmd := ""
		if len(parts) > 1 {
			subcmd = parts[1]
		}
		switch subcmd {
		case "list":
			sendReply(token, chatID, h.router.N8NListWorkflows(ctx))
		case "run":
			if len(parts) < 3 {
				sendReply(token, chatID, "❓ Format: `/n8n run <webhook-path>`\n\nContoh: `/n8n run my-workflow`")
				return
			}
			webhookPath := parts[2]
			// Optional: sisa parts sebagai simple payload
			var payload map[string]interface{}
			if len(parts) > 3 {
				payload = map[string]interface{}{
					"args": strings.Join(parts[3:], " "),
					"chat_id": chatIDStr,
					"user":    name,
				}
			}
			sendReply(token, chatID, h.router.N8NTrigger(ctx, webhookPath, payload))
		default:
			sendWithKeyboard(token, chatID, n8nMenuText(), n8nMenuKeyboard())
		}
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
			{"text": "🔁 n8n", "callback_data": "btn_n8n_menu"},
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

func n8nMenuKeyboard() map[string]interface{} {
	return map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "📋 List Workflows", "callback_data": "btn_n8n_list"},
			},
			{
				{"text": "🏠 Menu Utama", "callback_data": "btn_menu"},
			},
		},
	}
}

func n8nMenuText() string {
	return "🔁 *n8n Automation*\n━━━━━━━━━━━━━━━━\n" +
		"Kelola dan trigger workflow otomatisasi kamu.\n\n" +
		"*Commands:*\n" +
		"`/n8n list` — Daftar semua workflow\n" +
		"`/n8n run <webhook-path>` — Trigger workflow\n\n" +
		"_Webhook path diset di node Webhook n8n kamu._"
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
			{"command": "server", "description": "Home server: /server status|resources|devices"},
			{"command": "pc", "description": "Kontrol PC: /pc sleep|shutdown|reboot"},
			{"command": "run", "description": "Jalankan script: /run [nama]"},
			{"command": "n8n", "description": "n8n automation: /n8n list | /n8n run [webhook]"},
			{"command": "help", "description": "Panduan format pencatatan"},
		},
	})
	log.Println("[TG] Bot commands registered via setMyCommands")
}

// ── Unified Bot Photo OCR Integration ─────────────────────────────────────────

type FileResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		FilePath string `json:"file_path"`
	} `json:"result"`
}

type ScanResponse struct {
	Merchant string   `json:"merchant"`
	Date     string   `json:"date"`
	Total    float64  `json:"total"`
	Currency string   `json:"currency"`
	Category string   `json:"category"`
	Items    []struct {
		Name  string  `json:"name"`
		Price float64 `json:"price"`
	} `json:"items"`
	Analysis string `json:"analysis"`
}

func (h *WebhookHandler) handlePhoto(ctx context.Context, msg *Message) {
	token := h.cfg.TelegramBotToken
	chatID := msg.Chat.ID
	chatIDStr := strconv.FormatInt(chatID, 10)

	// Check if user is linked to FinTrack
	userID, isLinked := h.router.GetBinding(ctx, chatIDStr)
	if !isLinked {
		sendReply(token, chatID, "⚠️ *Akun Telegram Anda belum terhubung*\n\nBuka dashboard FinTrack → Profil → *Telegram*, generate kode, lalu kirim:\n`/link [kode]`")
		return
	}

	// 1. Get the largest photo (the last one in the slice)
	photo := msg.Photo[len(msg.Photo)-1]
	
	// Send initial status message
	sendReply(token, chatID, "📸 *Gambar struk diterima.* Sedang mengunduh dan menganalisis struk... Mohon tunggu.")

	// 2. Call Telegram getFile to retrieve file path
	fileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", token, photo.FileID)
	resp, err := http.Get(fileURL)
	if err != nil {
		log.Printf("[TG-OCR] getFile failed: %v", err)
		sendReply(token, chatID, "❌ Gagal mengunduh metadata foto dari Telegram.")
		return
	}
	defer resp.Body.Close()

	var fileResp FileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil || !fileResp.OK {
		log.Printf("[TG-OCR] Decode getFile failed: %v", err)
		sendReply(token, chatID, "❌ Gagal mengurai metadata foto dari Telegram.")
		return
	}

	// 3. Download the actual image bytes
	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, fileResp.Result.FilePath)
	imgResp, err := http.Get(downloadURL)
	if err != nil {
		log.Printf("[TG-OCR] Download image failed: %v", err)
		sendReply(token, chatID, "❌ Gagal mengunduh gambar struk dari Telegram.")
		return
	}
	defer imgResp.Body.Close()

	imgBytes, err := io.ReadAll(imgResp.Body)
	if err != nil {
		log.Printf("[TG-OCR] Read image bytes failed: %v", err)
		sendReply(token, chatID, "❌ Gagal membaca data gambar.")
		return
	}

	// 4. Send the image to Expense Tracker API
	trackerURL := os.Getenv("EXPENSE_TRACKER_API_URL")
	if trackerURL == "" {
		trackerURL = "http://expense-tracker-api:8000"
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "receipt.jpg")
	if err != nil {
		log.Printf("[TG-OCR] Create form file failed: %v", err)
		sendReply(token, chatID, "❌ Gagal menyiapkan form data upload.")
		return
	}
	if _, err := part.Write(imgBytes); err != nil {
		log.Printf("[TG-OCR] Write form bytes failed: %v", err)
		sendReply(token, chatID, "❌ Gagal menulis form data gambar.")
		return
	}
	writer.Close()

	scanURL := fmt.Sprintf("%s/scan", strings.TrimSuffix(trackerURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "POST", scanURL, body)
	if err != nil {
		log.Printf("[TG-OCR] Create HTTP request failed: %v", err)
		sendReply(token, chatID, "❌ Gagal menyiapkan koneksi API scanner.")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 35 * time.Second}
	scanResp, err := client.Do(req)
	if err != nil {
		log.Printf("[TG-OCR] Request scan failed: %v", err)
		sendReply(token, chatID, "❌ API Expense Tracker offline atau tidak merespons.")
		return
	}
	defer scanResp.Body.Close()

	if scanResp.StatusCode != http.StatusOK {
		var errData map[string]string
		_ = json.NewDecoder(scanResp.Body).Decode(&errData)
		errMsg := errData["error"]
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", scanResp.StatusCode)
		}
		log.Printf("[TG-OCR] Scan status failed: %s", errMsg)
		sendReply(token, chatID, fmt.Sprintf("❌ Gagal menganalisis struk: %s", errMsg))
		return
	}

	var scanRes ScanResponse
	if err := json.NewDecoder(scanResp.Body).Decode(&scanRes); err != nil {
		log.Printf("[TG-OCR] Decode scan failed: %v", err)
		sendReply(token, chatID, "❌ Gagal mengurai data hasil scan dari server.")
		return
	}

	// 5. Save the transaction to FinTrack
	itemsDesc := ""
	for _, item := range scanRes.Items {
		priceStr := fmt.Sprintf("%.2f", item.Price)
		itemsDesc += fmt.Sprintf("  • %s (%s %s)\n", item.Name, scanRes.Currency, priceStr)
	}
	if itemsDesc == "" {
		itemsDesc = "  • (Tidak ada detail item)\n"
	}

	description := fmt.Sprintf("Scan Struk: %s", scanRes.Merchant)
	if len(scanRes.Items) > 0 {
		itemsSummary := ""
		for idx, item := range scanRes.Items {
			if idx > 0 {
				itemsSummary += ", "
			}
			itemsSummary += item.Name
		}
		if len(itemsSummary) > 60 {
			itemsSummary = itemsSummary[:57] + "..."
		}
		description += fmt.Sprintf(" (%s)", itemsSummary)
	}

	// Save transaction via router
	reply := h.router.SaveTransaction(ctx, userID, description, scanRes.Category, int64(scanRes.Total))

	// 6. Format reports and reply to user
	report := fmt.Sprintf(
		"📊 *SMART RECEIPT INSIGHT*\n"+
		"━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
		"🏪 *Merchant:* %s\n"+
		"📅 *Date:* %s\n"+
		"💰 *Total:* %s %.2f\n"+
		"📂 *Category:* %s\n\n"+
		"🛒 *Items:*\n%s\n"+
		"💬 *AI Analysis:*\n%s\n\n"+
		"🔗 *Integrasi FinTrack Status:*\n%s",
		scanRes.Merchant, scanRes.Date, scanRes.Currency, scanRes.Total, scanRes.Category,
		itemsDesc, scanRes.Analysis, reply,
	)

	sendReply(token, chatID, report)
}
