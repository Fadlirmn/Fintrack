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
	"sync"
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

	// pendingScans holds OCR results awaiting user confirmation (sandbox mode).
	// Key: UUID string. Auto-expired after 5 minutes.
	pendingScans map[string]*PendingScan
	pendingMu    sync.Mutex

	// scanOnlyUsers holds chatIDs in persistent "scan batch" mode.
	// Mode stays active until user sends /stop. Each photo refreshes the TTL.
	scanOnlyUsers map[int64]*scanOnlyState
	scanOnlyMu    sync.Mutex

	// mediaGroups buffers album photos (same media_group_id) for 2 seconds
	// before processing them all together into a single combined PDF.
	mediaGroups map[string]*mediaGroupBuffer
	mediaGroupMu sync.Mutex
}

// scanOnlyState tracks per-user scan batch mode.
type scanOnlyState struct {
	LastActive time.Time
	Count      int // number of receipts scanned so far
}

// mediaGroupBuffer holds photos belonging to a Telegram album (same media_group_id).
type mediaGroupBuffer struct {
	ChatID     int64
	UserID     string // empty if scan-only mode
	IsScanOnly bool
	Photos     []PhotoSize   // largest photo from each message in the album
	Timer      *time.Timer   // fires processMediaGroup after 2s of no new photos
	CreatedAt  time.Time
}

// PendingScan holds a scan result waiting for user's ✅ or ❌ confirmation.
type PendingScan struct {
	ScanRes   ScanResponse
	UserID    string
	ChatID    int64
	CreatedAt time.Time
}

func NewWebhookHandler(cfg *config.Config, router *gateway.GatewayRouter) *WebhookHandler {
	h := &WebhookHandler{
		cfg:           cfg,
		router:        router,
		pendingScans:  make(map[string]*PendingScan),
		scanOnlyUsers: make(map[int64]*scanOnlyState),
		mediaGroups:   make(map[string]*mediaGroupBuffer),
	}
	go h.cleanupExpiredScans()
	return h
}

// cleanupExpiredScans removes pendingScans and scanOnlyUsers older than 10 minutes.
// scanOnlyUsers TTL resets on each photo — if idle for 10 minutes, auto-exit.
func (h *WebhookHandler) cleanupExpiredScans() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.pendingMu.Lock()
		for key, ps := range h.pendingScans {
			if time.Since(ps.CreatedAt) > 5*time.Minute {
				delete(h.pendingScans, key)
			}
		}
		h.pendingMu.Unlock()

		h.scanOnlyMu.Lock()
		for chatID, st := range h.scanOnlyUsers {
			if time.Since(st.LastActive) > 10*time.Minute {
				delete(h.scanOnlyUsers, chatID)
			}
		}
		h.scanOnlyMu.Unlock()
	}
}

// ── Telegram Types ────────────────────────────────────────────────────────────

type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID    int         `json:"message_id"`
	Chat         Chat        `json:"chat"`
	Text         string      `json:"text"`
	From         TGUser      `json:"from"`
	Photo        []PhotoSize `json:"photo"`
	MediaGroupID string      `json:"media_group_id"`
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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

	data := cq.Data

	switch {
	case data == "btn_refresh" || data == "btn_menu":
		name := cq.From.FirstName
		editMessage(token, chatID, msgID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))

	case data == "btn_saldo":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, h.router.GetBalance(ctx, userID), mainMenuKeyboard(true))

	case data == "btn_summary":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, h.router.GetSummary(ctx, userID), mainMenuKeyboard(true))

	case data == "btn_akun":
		if !isLinked {
			editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
			return
		}
		editMessage(token, chatID, msgID, h.router.GetAccountDetail(ctx, userID), mainMenuKeyboard(true))

	case data == "btn_panduan":
		editMessage(token, chatID, msgID, guideText(), mainMenuKeyboard(isLinked))

	case data == "btn_cara_link":
		editMessage(token, chatID, msgID, linkGuideText(), mainMenuKeyboard(false))

	case data == "btn_scan_struk":
		h.scanOnlyMu.Lock()
		h.scanOnlyUsers[chatID] = &scanOnlyState{LastActive: time.Now(), Count: 0}
		h.scanOnlyMu.Unlock()
		editMessage(token, chatID, msgID,
			"📸 *Mode Scan Struk — Batch Mode*\n\n"+
				"Kirim foto struk satu per satu.\n"+
				"Setiap struk akan dianalisis AI dan dikirim sebagai PDF.\n"+
				"*Tidak disimpan ke FinTrack.*\n\n"+
				"Ketik /stop atau klik tombol di bawah jika selesai.",
			map[string]interface{}{
				"inline_keyboard": [][]map[string]string{
					{{"text": "⏹ Selesai Scan", "callback_data": "stop_scan_mode"}},
				},
			})

	case data == "cancel_scan_mode", data == "stop_scan_mode":
		h.scanOnlyMu.Lock()
		st := h.scanOnlyUsers[chatID]
		delete(h.scanOnlyUsers, chatID)
		h.scanOnlyMu.Unlock()
		count := 0
		if st != nil {
			count = st.Count
		}
		summary := fmt.Sprintf("✅ *Mode Scan Selesai*\n\n📊 Total struk discan: *%d buah*\n\nKembali ke menu utama.", count)
		editMessage(token, chatID, msgID, summary, mainMenuKeyboard(isLinked))

	// ── Scan Sandbox Callbacks ─────────────────────────────────────────────

	case strings.HasPrefix(data, "confirm_scan:"):
		scanKey := strings.TrimPrefix(data, "confirm_scan:")
		h.handleConfirmScan(ctx, token, chatID, msgID, userID, isLinked, scanKey)

	case strings.HasPrefix(data, "cancel_scan:"):
		scanKey := strings.TrimPrefix(data, "cancel_scan:")
		h.pendingMu.Lock()
		delete(h.pendingScans, scanKey)
		h.pendingMu.Unlock()
		editMessage(token, chatID, msgID,
			"❌ *Scan dibatalkan.*\n\nTransaksi tidak disimpan.",
			mainMenuKeyboard(isLinked))

	case strings.HasPrefix(data, "save_all_scans:"):
		// Save all scans from an album batch to FinTrack
		keysStr := strings.TrimPrefix(data, "save_all_scans:")
		keys := strings.Split(keysStr, ",")

		editMessage(token, chatID, msgID,
			fmt.Sprintf("⏳ *Menyimpan %d transaksi ke FinTrack...*", len(keys)),
			nil)

		var savedCount int
		var totalAmount int64
		for _, key := range keys {
			key = strings.TrimSpace(key)
			h.pendingMu.Lock()
			ps, ok := h.pendingScans[key]
			if ok {
				delete(h.pendingScans, key)
			}
			h.pendingMu.Unlock()
			if !ok || ps == nil {
				continue
			}
			// Build description
			uid := ps.UserID
			if uid == "" {
				uid = userID
			}
			description := cleanText(ps.ScanRes.Merchant)
			if len(ps.ScanRes.Items) > 0 {
				var items []string
				for _, item := range ps.ScanRes.Items {
					items = append(items, cleanText(item.Name))
				}
				summary := strings.Join(items, ", ")
				if len(summary) > 60 {
					summary = summary[:57] + "..."
				}
				description += " (" + summary + ")"
			}
			amount := int64(ps.ScanRes.Total)
			h.router.SaveTransaction(ctx, uid, description, ps.ScanRes.Category, amount)
			savedCount++
			totalAmount += amount
		}

		if savedCount == 0 {
			editMessage(token, chatID, msgID,
				"⚠️ *Sesi sudah kedaluwarsa.* Kirim ulang foto untuk mencoba lagi.",
				mainMenuKeyboard(isLinked))
			return
		}

		editMessage(token, chatID, msgID,
			fmt.Sprintf("✅ *%d transaksi berhasil disimpan!*\n\n💰 Total: *Rp %s*\n\nSemua struk sudah tercatat di FinTrack.",
				savedCount, formatThousands(totalAmount)),
			mainMenuKeyboard(isLinked))
	}
}

// handleConfirmScan saves a previously scanned receipt to DB and sends back a PDF.
func (h *WebhookHandler) handleConfirmScan(ctx context.Context, token string, chatID int64, msgID int, userID string, isLinked bool, scanKey string) {
	h.pendingMu.Lock()
	ps, ok := h.pendingScans[scanKey]
	if ok {
		delete(h.pendingScans, scanKey)
	}
	h.pendingMu.Unlock()

	if !ok {
		editMessage(token, chatID, msgID,
			"⚠️ *Sesi scan sudah kedaluwarsa* (5 menit).\n\nKirim ulang foto struk untuk mencoba lagi.",
			mainMenuKeyboard(isLinked))
		return
	}

	if !isLinked || userID == "" {
		editMessage(token, chatID, msgID, notLinkedText(), mainMenuKeyboard(false))
		return
	}

	scanRes := ps.ScanRes

	// Build description
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

	// Save to DB
	savedAmount := int64(scanRes.Total)
	reply := h.router.SaveTransaction(ctx, userID, description, scanRes.Category, savedAmount)

	// Update message with saved confirmation
	editMessage(token, chatID, msgID,
		fmt.Sprintf("✅ *Struk berhasil disimpan!*\n\n%s\n\n📄 _Menyiapkan laporan PDF..._", reply),
		nil)

	// Generate and send PDF — fetch balance data first
	bal := h.router.GetBalanceData(ctx, userID)
	pdfBytes, err := GenerateReceiptPDF(scanRes, savedAmount, bal)
	if err != nil {
		log.Printf("[PDF] Generate failed: %v", err)
		sendReply(token, chatID, "⚠️ Transaksi tersimpan, tapi gagal generate PDF.")
		sendWithKeyboard(token, chatID, "Pilih aksi selanjutnya:", afterSaveKeyboard())
		return
	}

	fileName := fmt.Sprintf("struk_%s_%s.pdf",
		strings.ReplaceAll(strings.ToLower(scanRes.Merchant), " ", "_"),
		time.Now().Format("20060102_150405"),
	)
	if err := sendDocument(token, chatID, fileName, pdfBytes); err != nil {
		log.Printf("[PDF] Send failed: %v", err)
		sendReply(token, chatID, "⚠️ Transaksi tersimpan, tapi gagal mengirim PDF.")
	}

	sendWithKeyboard(token, chatID, "Pilih aksi selanjutnya:", afterSaveKeyboard())
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

	case strings.HasPrefix(text, "/stop"), strings.HasPrefix(text, "/selesai"):
		h.scanOnlyMu.Lock()
		st := h.scanOnlyUsers[chatID]
		delete(h.scanOnlyUsers, chatID)
		h.scanOnlyMu.Unlock()
		if st != nil {
			_, isLinked := h.router.GetBinding(ctx, chatIDStr)
			sendWithKeyboard(token, chatID,
				fmt.Sprintf("✅ *Mode Scan Selesai*\n\n📊 Total struk discan: *%d buah*\n\nKembali ke menu utama.", st.Count),
				mainMenuKeyboard(isLinked))
		} else {
			_, isLinked := h.router.GetBinding(ctx, chatIDStr)
			sendWithKeyboard(token, chatID, welcomeText(name, isLinked), mainMenuKeyboard(isLinked))
		}
		return

	case strings.HasPrefix(text, "/scan"), strings.HasPrefix(text, "/struk"):
		h.scanOnlyMu.Lock()
		h.scanOnlyUsers[chatID] = &scanOnlyState{LastActive: time.Now(), Count: 0}
		h.scanOnlyMu.Unlock()
		sendReply(token, chatID,
			"📸 *Mode Scan Struk — Batch Mode*\n\n"+
				"Kirim foto struk satu per satu.\n"+
				"Setiap struk akan dianalisis AI dan dikirim sebagai PDF.\n"+
				"*Tidak disimpan ke FinTrack.*\n\n"+
				"Ketik */stop* atau */selesai* jika sudah selesai.")
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

	case strings.HasPrefix(text, "/akun"):
		sendReply(token, chatID, h.router.GetAccountDetail(ctx, userID))

	case strings.HasPrefix(text, "/scan"), strings.HasPrefix(text, "/struk"):
		h.scanOnlyMu.Lock()
		h.scanOnlyUsers[chatID] = &scanOnlyState{LastActive: time.Now(), Count: 0}
		h.scanOnlyMu.Unlock()
		sendReply(token, chatID,
			"📸 *Mode Scan Struk — Batch Mode*\n\n"+
				"Kirim foto struk satu per satu.\n"+
				"Setiap struk akan dianalisis AI dan dikirim sebagai PDF.\n"+
				"*Tidak disimpan ke FinTrack.*\n\n"+
				"Ketik */stop* atau */selesai* jika sudah selesai.")

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
			{"text": "👤 Akun Saya", "callback_data": "btn_akun"},
			{"text": "📸 Scan Struk", "callback_data": "btn_scan_struk"},
		},
		{
			{"text": "📋 Panduan Catat", "callback_data": "btn_panduan"},
			{"text": "🔄 Refresh", "callback_data": "btn_refresh"},
		},
		{
			{"text": "🌐 Buka Dashboard", "url": "https://fintrack.home-sumbul.my.id"},
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

func scanConfirmKeyboard(scanKey string) map[string]interface{} {
	return map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "✅ Simpan Transaksi", "callback_data": "confirm_scan:" + scanKey},
				{"text": "❌ Batal", "callback_data": "cancel_scan:" + scanKey},
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
		"📸 *Fitur Pintar Scan Struk:*\n" +
		"Cukup kirim/unggah *foto struk belanja* Anda langsung ke bot ini. AI akan membaca detail item, total nominal, kategori, serta menampilkan preview untuk dikonfirmasi sebelum disimpan. Laporan PDF akan dikirim otomatis!\n\n" +
		"*Perintah tersedia:*\n" +
		"`/saldo` — Saldo yang bisa dibelanjakan\n" +
		"`/summary` — Rekap pengeluaran bulan ini\n" +
		"`/akun` — Detail profil akun keuangan\n" +
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
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	callTelegramAPI(token, "sendMessage", payload)
}

func editMessage(token string, chatID int64, msgID int, text string, keyboard map[string]interface{}) {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	callTelegramAPI(token, "editMessageText", payload)
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

// sendDocument uploads a file to a Telegram chat via sendDocument (multipart/form-data).
func sendDocument(token string, chatID int64, filename string, data []byte) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", token)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))

	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("write file data: %w", err)
	}
	writer.Close()

	resp, err := http.Post(url, writer.FormDataContentType(), &body)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendDocument HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// SetMyCommands registers bot commands with BotFather.
func SetMyCommands(token string) {
	callTelegramAPI(token, "setMyCommands", map[string]interface{}{
		"commands": []map[string]string{
			{"command": "start", "description": "Buka menu utama"},
			{"command": "menu", "description": "Tampilkan menu interaktif"},
			{"command": "saldo", "description": "Lihat saldo yang bisa dibelanjakan"},
			{"command": "summary", "description": "Rekap pengeluaran bulan ini"},
			{"command": "akun", "description": "Detail profil akun keuangan"},
			{"command": "scan", "description": "Scan banyak struk (batch, tanpa simpan ke FinTrack)"},
			{"command": "stop", "description": "Selesai scan struk, kembali ke menu"},
			{"command": "link", "description": "Hubungkan akun: /link [kode]"},
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

	// ── Media group (album) buffering ──────────────────────────────────────
	// Telegram sends each photo in an album as separate messages sharing the
	// same media_group_id. We collect them for 2 seconds, then process together.
	if msg.MediaGroupID != "" {
		largest := msg.Photo[len(msg.Photo)-1]

		// Check mode before buffering
		h.scanOnlyMu.Lock()
		st, isScanOnly := h.scanOnlyUsers[chatID]
		if isScanOnly {
			st.LastActive = time.Now()
		}
		h.scanOnlyMu.Unlock()

		var userID string
		if !isScanOnly {
			var isLinked bool
			userID, isLinked = h.router.GetBinding(ctx, chatIDStr)
			if !isLinked {
				sendReply(token, chatID, "⚠️ *Akun Telegram Anda belum terhubung*\n\nGunakan /scan untuk scan struk tanpa akun FinTrack.")
				return
			}
		}

		h.mediaGroupMu.Lock()
		buf, exists := h.mediaGroups[msg.MediaGroupID]
		if !exists {
			buf = &mediaGroupBuffer{
				ChatID:     chatID,
				UserID:     userID,
				IsScanOnly: isScanOnly,
				CreatedAt:  time.Now(),
			}
			h.mediaGroups[msg.MediaGroupID] = buf
			// First photo of the album — inform user
			sendReply(token, chatID, fmt.Sprintf("📂 *Album diterima.* Mengumpulkan foto...\n_Tunggu sebentar, semua struk akan diproses sekaligus._"))
		}
		buf.Photos = append(buf.Photos, largest)

		// Reset 2-second timer on each new photo
		if buf.Timer != nil {
			buf.Timer.Stop()
		}
		groupID := msg.MediaGroupID
		buf.Timer = time.AfterFunc(2*time.Second, func() {
			h.processMediaGroup(context.Background(), groupID)
		})
		h.mediaGroupMu.Unlock()
		return // handled by processMediaGroup
	}

	// ── Determine mode: scan-only (batch) OR sandbox (save to DB) ─────────
	h.scanOnlyMu.Lock()
	st, isScanOnly := h.scanOnlyUsers[chatID]
	if isScanOnly {
		// Refresh TTL and increment counter — mode stays active
		st.LastActive = time.Now()
		st.Count++
	}
	h.scanOnlyMu.Unlock()

	// For scan-only mode, skip account link check — anyone can scan
	// For save mode, user must be linked
	var userID string

	if !isScanOnly {
		var isLinked bool
		userID, isLinked = h.router.GetBinding(ctx, chatIDStr)
		if !isLinked {
			sendReply(token, chatID, "⚠️ *Akun Telegram Anda belum terhubung*\n\nBuka dashboard FinTrack → Profil → *Telegram*, generate kode, lalu kirim:\n`/link [kode]`\n\nAtau gunakan /scan untuk scan struk tanpa akun FinTrack.")
			return
		}
	}

	// 1. Get the largest photo (the last one in the slice)
	photo := msg.Photo[len(msg.Photo)-1]

	// Send initial status message — show count for batch mode
	if isScanOnly {
		sendReply(token, chatID, fmt.Sprintf("📸 *Struk #%d diterima.* Sedang dianalisis AI...\n_Tidak disimpan ke FinTrack. Ketik /stop jika selesai._", st.Count))
	} else {
		sendReply(token, chatID, "📸 *Gambar struk diterima.* Sedang menganalisis... Mohon tunggu.")
	}

	// 2. Call Telegram getFile to retrieve file path
	fileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", token, photo.FileID)
	fileReq, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		log.Printf("[TG-OCR] Build getFile request failed: %v", err)
		sendReply(token, chatID, "❌ Gagal menyiapkan permintaan metadata foto.")
		return
	}
	fileClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := fileClient.Do(fileReq)
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
	dlReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		log.Printf("[TG-OCR] Build download request failed: %v", err)
		sendReply(token, chatID, "❌ Gagal menyiapkan permintaan unduhan gambar.")
		return
	}
	dlClient := &http.Client{Timeout: 30 * time.Second}
	imgResp, err := dlClient.Do(dlReq)
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

	client := &http.Client{Timeout: 120 * time.Second}
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

	// 5. ── ROUTE: scan-only OR sandbox ────────────────────────────────────────

	if isScanOnly {
		// ── SCAN ONLY (BATCH): langsung generate PDF, mode TETAP AKTIF ─────
		// Fetch balance only if user is linked (userID is set)
		bal := h.router.GetBalanceData(ctx, userID)
		pdfBytes, err := GenerateReceiptPDF(scanRes, 0, bal)
		if err != nil {
			log.Printf("[PDF] Generate failed (scan-only): %v", err)
			sendReply(token, chatID, "⚠️ Gagal generate PDF. Berikut ringkasan scan:\n\n"+formatScanText(scanRes))
		} else {
			fileName := fmt.Sprintf("scan_%s_%s.pdf",
				strings.ReplaceAll(strings.ToLower(scanRes.Merchant), " ", "_"),
				time.Now().Format("20060102_150405"),
			)
			if err := sendDocument(token, chatID, fileName, pdfBytes); err != nil {
				log.Printf("[PDF] Send failed (scan-only): %v", err)
				sendReply(token, chatID, "⚠️ Gagal mengirim PDF. Berikut ringkasan scan:\n\n"+formatScanText(scanRes))
			}
		}

		// Read current count (already incremented above)
		h.scanOnlyMu.Lock()
		curCount := 0
		if cur := h.scanOnlyUsers[chatID]; cur != nil {
			curCount = cur.Count
		}
		h.scanOnlyMu.Unlock()

		// Store to pending so user can optionally save to FinTrack
		scanKey := fmt.Sprintf("%d_%d", chatID, time.Now().UnixNano())
		h.pendingMu.Lock()
		h.pendingScans[scanKey] = &PendingScan{
			ScanRes:   scanRes,
			UserID:    userID,
			ChatID:    chatID,
			CreatedAt: time.Now(),
		}
		h.pendingMu.Unlock()

		// After PDF, show persistent batch controls
		sendWithKeyboard(token, chatID,
			fmt.Sprintf("✅ *Struk #%d selesai dianalisis!*\n\n📸 _Kirim foto struk berikutnya, atau klik Selesai._", curCount),
			map[string]interface{}{
				"inline_keyboard": [][]map[string]string{
					{
						{"text": "✅ Simpan ke FinTrack", "callback_data": "confirm_scan:" + scanKey},
					},
					{
						{"text": fmt.Sprintf("⏹ Selesai Scan (%d struk)", curCount), "callback_data": "stop_scan_mode"},
					},
				},
			})
		return
	}

	// ── SANDBOX MODE: Store result and ask for confirmation ─────────────────

	// Build items preview text
	itemsDesc := ""
	for _, item := range scanRes.Items {
		priceStr := fmt.Sprintf("%.0f", item.Price)
		itemsDesc += fmt.Sprintf("  • %s (%s %s)\n", item.Name, scanRes.Currency, priceStr)
	}
	if itemsDesc == "" {
		itemsDesc = "  • (Tidak ada detail item)\n"
	}

	// Store in pending map
	scanKey := fmt.Sprintf("%d_%d", chatID, time.Now().UnixNano())
	h.pendingMu.Lock()
	h.pendingScans[scanKey] = &PendingScan{
		ScanRes:   scanRes,
		UserID:    userID,
		ChatID:    chatID,
		CreatedAt: time.Now(),
	}
	h.pendingMu.Unlock()

	// 6. Send preview with confirmation keyboard
	preview := fmt.Sprintf(
		"📊 *HASIL SCAN STRUK — PREVIEW*\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"🏪 *Merchant:* %s\n"+
			"📅 *Tanggal:* %s\n"+
			"💰 *Total:* %s %.0f\n"+
			"📂 *Kategori:* %s\n\n"+
			"🛒 *Item:*\n%s\n"+
			"💬 *Analisis:*\n_%s_\n\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"⏳ _Konfirmasi dalam 5 menit sebelum kedaluwarsa_\n\n"+
			"Apakah kamu ingin menyimpan transaksi ini ke FinTrack?",
		scanRes.Merchant,
		scanRes.Date,
		scanRes.Currency, scanRes.Total,
		scanRes.Category,
		itemsDesc,
		scanRes.Analysis,
	)

	sendWithKeyboard(token, chatID, preview, scanConfirmKeyboard(scanKey))
}

// formatScanText returns a plain-text summary of a scan result.
// Used as fallback when PDF generation or sending fails.
func formatScanText(s ScanResponse) string {
	itemsDesc := ""
	for _, item := range s.Items {
		itemsDesc += fmt.Sprintf("  • %s (%.0f)\n", item.Name, item.Price)
	}
	if itemsDesc == "" {
		itemsDesc = "  • (tidak ada detail item)\n"
	}
	return fmt.Sprintf(
		"🏪 *Merchant:* %s\n"+
			"📅 *Tanggal:* %s\n"+
			"💰 *Total:* %s %.0f\n"+
			"📂 *Kategori:* %s\n\n"+
			"🛒 *Item:*\n%s\n"+
			"💬 *Analisis:* %s",
		s.Merchant, s.Date, s.Currency, s.Total, s.Category, itemsDesc, s.Analysis,
	)
}

// processMediaGroup is called ~2 seconds after the last photo in an album arrives.
// It scans all buffered photos in parallel, generates a single combined PDF, and sends it.
func (h *WebhookHandler) processMediaGroup(ctx context.Context, groupID string) {
	// Grab and remove the buffer
	h.mediaGroupMu.Lock()
	buf, ok := h.mediaGroups[groupID]
	if ok {
		delete(h.mediaGroups, groupID)
	}
	h.mediaGroupMu.Unlock()
	if !ok || len(buf.Photos) == 0 {
		return
	}

	token := h.cfg.TelegramBotToken
	chatID := buf.ChatID
	n := len(buf.Photos)

	sendReply(token, chatID, fmt.Sprintf("🔍 *Memproses %d struk...* Mohon tunggu.", n))

	// ── Scan all photos in parallel ───────────────────────────────────────────
	type result struct {
		idx  int
		scan ScanResponse
		err  error
	}
	results := make([]result, n)
	var wg sync.WaitGroup

	trackerURL := os.Getenv("EXPENSE_TRACKER_API_URL")
	if trackerURL == "" {
		trackerURL = "http://expense-tracker-api:8000"
	}
	scanURL := fmt.Sprintf("%s/scan", strings.TrimSuffix(trackerURL, "/"))

	for i, photo := range buf.Photos {
		wg.Add(1)
		go func(idx int, ph PhotoSize) {
			defer wg.Done()

			// Get file path from Telegram
			fileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", token, ph.FileID)
			fileReq, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
			if err != nil {
				results[idx] = result{idx: idx, err: fmt.Errorf("getFile req: %w", err)}
				return
			}
			resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(fileReq)
			if err != nil {
				results[idx] = result{idx: idx, err: fmt.Errorf("getFile: %w", err)}
				return
			}
			defer resp.Body.Close()
			var fileResp FileResponse
			if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil || !fileResp.OK {
				results[idx] = result{idx: idx, err: fmt.Errorf("decode getFile")}
				return
			}

			// Download image
			dlURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, fileResp.Result.FilePath)
			dlResp, err := (&http.Client{Timeout: 30 * time.Second}).Get(dlURL)
			if err != nil {
				results[idx] = result{idx: idx, err: fmt.Errorf("download: %w", err)}
				return
			}
			defer dlResp.Body.Close()
			imgBytes, err := io.ReadAll(dlResp.Body)
			if err != nil {
				results[idx] = result{idx: idx, err: fmt.Errorf("read img: %w", err)}
				return
			}

			// Upload to OCR
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", fmt.Sprintf("receipt_%d.jpg", idx+1))
			if err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			if _, err := part.Write(imgBytes); err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			writer.Close()

			req, err := http.NewRequestWithContext(ctx, "POST", scanURL, &body)
			if err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			req.Header.Set("Content-Type", writer.FormDataContentType())
			scanResp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
			if err != nil {
				results[idx] = result{idx: idx, err: fmt.Errorf("scan req: %w", err)}
				return
			}
			defer scanResp.Body.Close()
			if scanResp.StatusCode != http.StatusOK {
				results[idx] = result{idx: idx, err: fmt.Errorf("scan HTTP %d", scanResp.StatusCode)}
				return
			}
			var sr ScanResponse
			if err := json.NewDecoder(scanResp.Body).Decode(&sr); err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			results[idx] = result{idx: idx, scan: sr}
		}(i, photo)
	}
	wg.Wait()

	// Collect successful scans
	var scans []ScanResponse
	var failed []int
	for _, r := range results {
		if r.err != nil {
			log.Printf("[ALBUM] Scan photo %d failed: %v", r.idx+1, r.err)
			failed = append(failed, r.idx+1)
		} else {
			scans = append(scans, r.scan)
		}
	}

	if len(scans) == 0 {
		sendReply(token, chatID, "❌ Gagal menganalisis semua foto. Coba lagi satu per satu.")
		return
	}

	if len(failed) > 0 {
		sendReply(token, chatID, fmt.Sprintf("⚠️ %d foto berhasil, %d foto gagal dianalisis.", len(scans), len(failed)))
	}

	// Fetch balance data
	bal := h.router.GetBalanceData(ctx, buf.UserID)

	// Generate combined PDF
	pdfBytes, err := GenerateCombinedReceiptPDF(scans, bal)
	if err != nil {
		log.Printf("[ALBUM] Combined PDF failed: %v", err)
		sendReply(token, chatID, "⚠️ Gagal generate PDF gabungan.")
		return
	}

	fileName := fmt.Sprintf("batch_scan_%s_%d_struk.pdf",
		time.Now().Format("20060102_150405"),
		len(scans),
	)
	if err := sendDocument(token, chatID, fileName, pdfBytes); err != nil {
		log.Printf("[ALBUM] Send PDF failed: %v", err)
		sendReply(token, chatID, "⚠️ Gagal mengirim PDF.")
		return
	}

	// Store each scan to pendingScans so user can save individually or all at once
	var scanKeys []string
	for _, sc := range scans {
		key := fmt.Sprintf("%d_%d", chatID, time.Now().UnixNano())
		h.pendingMu.Lock()
		h.pendingScans[key] = &PendingScan{
			ScanRes:   sc,
			UserID:    buf.UserID,
			ChatID:    chatID,
			CreatedAt: time.Now(),
		}
		h.pendingMu.Unlock()
		scanKeys = append(scanKeys, key)
		time.Sleep(1 * time.Microsecond) // ensure unique UnixNano keys
	}

	// Build save-all keyboard
	var kbRows [][]map[string]string
	if buf.UserID != "" {
		// Offer save all as batch
		allKeys := strings.Join(scanKeys, ",")
		kbRows = append(kbRows, []map[string]string{
			{"text": fmt.Sprintf("✅ Simpan Semua (%d struk) ke FinTrack", len(scans)), "callback_data": "save_all_scans:" + allKeys},
		})
	}
	if buf.IsScanOnly {
		kbRows = append(kbRows, []map[string]string{
			{"text": "⏹ Selesai Scan", "callback_data": "stop_scan_mode"},
		})
	} else {
		kbRows = append(kbRows, []map[string]string{
			{"text": "🏠 Menu Utama", "callback_data": "btn_menu"},
		})
	}

	sendWithKeyboard(token, chatID,
		fmt.Sprintf("✅ *PDF Gabungan (%d struk) Terkirim!*\n\n📄 Struk belum disimpan ke FinTrack.\nKlik *Simpan Semua* untuk mencatat semua transaksi.", len(scans)),
		map[string]interface{}{"inline_keyboard": kbRows})
}
