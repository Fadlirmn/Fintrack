package gateway

import (
	"context"
	"fmt"
	"strings"

	fintrackClient "fintrack-backend/internal/gateway/fintrack"
	homeClient "fintrack-backend/internal/gateway/home"
)

// GatewayRouter dispatches Telegram commands to the appropriate service client.
// It is the central brain of the bot — no business logic lives here, only routing.
type GatewayRouter struct {
	fintrack *fintrackClient.Client
	home     *homeClient.Client
}

// NewGatewayRouter wires together both service clients.
func NewGatewayRouter(ft *fintrackClient.Client, h *homeClient.Client) *GatewayRouter {
	return &GatewayRouter{fintrack: ft, home: h}
}

// ── Binding / Linking ─────────────────────────────────────────────────────────

// GetBinding checks if a Telegram chat is linked to a FinTrack account.
// Returns (userID, isLinked).
func (r *GatewayRouter) GetBinding(ctx context.Context, chatID string) (string, bool) {
	result, err := r.fintrack.GetBinding(ctx, chatID)
	if err != nil || !result.Linked {
		return "", false
	}
	return result.UserID, true
}

// LinkAccount attempts to link a Telegram account using the given verification code.
// Returns a user-facing message string.
func (r *GatewayRouter) LinkAccount(ctx context.Context, chatID, code, name string) string {
	result, err := r.fintrack.LinkAccount(ctx, chatID, code, name)
	if err != nil {
		return fmt.Sprintf("❌ Gagal menghubungkan akun: %v", err)
	}
	if !result.OK {
		return "❌ Kode tidak valid atau sudah kedaluwarsa. Generate kode baru di dashboard → Profil → Telegram."
	}
	return fmt.Sprintf(
		"🎉 *Berhasil Terhubung, %s\\!*\n\nAkun FinTrack kamu sudah tersambung\\. Mulai catat pengeluaran dari sini\\!",
		name,
	)
}

// ── FinTrack Commands ─────────────────────────────────────────────────────────

// GetBalance returns the formatted spendable balance message for a user.
func (r *GatewayRouter) GetBalance(ctx context.Context, userID string) string {
	data, err := r.fintrack.GetBalance(ctx, userID)
	if err != nil {
		return "❌ Gagal mengambil data saldo. Coba lagi nanti."
	}
	return fmt.Sprintf(
		"💰 *Saldo yang Bisa Dibelanjakan*\n━━━━━━━━━━━━━━━━\n"+
			"☀️ *Hari ini:*   %s\n"+
			"📅 *Minggu ini:* %s\n"+
			"📆 *Bulan ini:*  %s\n"+
			"━━━━━━━━━━━━━━━━\n"+
			"_(Pengeluaran wajib aktif: %s/hari)_",
		formatRupiah(data.SpendableToday),
		formatRupiah(data.SpendableWeek),
		formatRupiah(data.SpendableMonth),
		formatRupiah(data.FixedDaily),
	)
}

// GetSummary returns the formatted monthly spending summary for a user.
func (r *GatewayRouter) GetSummary(ctx context.Context, userID string) string {
	data, err := r.fintrack.GetSummary(ctx, userID)
	if err != nil {
		return "❌ Gagal mengambil rekap. Coba lagi nanti."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 *Rekap %s %d*\n━━━━━━━━━━━━━━━━\n", data.Month, data.Year))
	sb.WriteString(fmt.Sprintf("💸 *Total:* %s\n\n", formatRupiah(data.Total)))
	sb.WriteString("🏷️ *Top Kategori:*\n")

	medals := []string{"🥇", "🥈", "🥉", "4️⃣", "5️⃣"}
	for i, cat := range data.TopCategories {
		if i >= len(medals) {
			break
		}
		sb.WriteString(fmt.Sprintf("  %s %s: *%s*\n",
			medals[i],
			strings.Title(cat.Category),
			formatRupiah(cat.Total),
		))
	}
	if len(data.TopCategories) == 0 {
		sb.WriteString("  Belum ada transaksi bulan ini.\n")
	}
	return sb.String()
}

// SaveTransaction saves a new expense from the bot.
// Returns a user-facing confirmation message.
func (r *GatewayRouter) SaveTransaction(ctx context.Context, userID, description, category string, amount int64) string {
	_, err := r.fintrack.SaveTransaction(ctx, userID, description, category, amount)
	if err != nil {
		return "❌ Gagal menyimpan. Coba lagi nanti."
	}
	return fmt.Sprintf(
		"✅ *Transaksi Dicatat!*\n\n📝 %s\n💰 *%s*\n🏷️ _%s_",
		description, formatRupiah(amount), category,
	)
}

// ── Home Server Commands ───────────────────────────────────────────────────────

// ServerStatus returns formatted server info from home-server.
func (r *GatewayRouter) ServerStatus(ctx context.Context) string {
	if !r.home.IsEnabled() {
		return "⚠️ Home Server tidak dikonfigurasi."
	}
	data, err := r.home.GetStatus(ctx)
	if err != nil {
		return fmt.Sprintf("❌ Home Server tidak dapat dijangkau: %v", err)
	}
	return fmt.Sprintf(
		"🖥️ *Server Status*\n━━━━━━━━━━━━━━━━\n"+
			"🏷️ *Hostname:* %s\n"+
			"💻 *OS:* %s\n"+
			"⏱️ *Uptime:* %s\n"+
			"📊 *Load:* %s",
		data.Hostname, data.OS, data.Uptime, data.LoadAvg,
	)
}

// ServerResources returns formatted resource usage from home-server.
func (r *GatewayRouter) ServerResources(ctx context.Context) string {
	if !r.home.IsEnabled() {
		return "⚠️ Home Server tidak dikonfigurasi."
	}
	data, err := r.home.GetResources(ctx)
	if err != nil {
		return fmt.Sprintf("❌ Gagal ambil resource: %v", err)
	}
	ramPct := 0.0
	if data.RAMTotal > 0 {
		ramPct = float64(data.RAMUsed) / float64(data.RAMTotal) * 100
	}
	diskPct := 0.0
	if data.DiskTotal > 0 {
		diskPct = float64(data.DiskUsed) / float64(data.DiskTotal) * 100
	}
	return fmt.Sprintf(
		"📊 *Resource Monitor*\n━━━━━━━━━━━━━━━━\n"+
			"🔥 *CPU:* %.1f%%\n"+
			"🧠 *RAM:* %d/%d MB (%.0f%%)\n"+
			"💾 *Disk:* %d/%d GB (%.0f%%)",
		data.CPUPercent,
		data.RAMUsed, data.RAMTotal, ramPct,
		data.DiskUsed, data.DiskTotal, diskPct,
	)
}

// ServerDevices returns a list of devices on the local network.
func (r *GatewayRouter) ServerDevices(ctx context.Context) string {
	if !r.home.IsEnabled() {
		return "⚠️ Home Server tidak dikonfigurasi."
	}
	data, err := r.home.GetDevices(ctx)
	if err != nil {
		return fmt.Sprintf("❌ Gagal scan device: %v", err)
	}
	if len(data.Devices) == 0 {
		return "📡 Tidak ada device yang ditemukan di jaringan."
	}
	var sb strings.Builder
	sb.WriteString("📡 *Device di Jaringan*\n━━━━━━━━━━━━━━━━\n")
	for _, d := range data.Devices {
		name := d.Hostname
		if name == "" {
			name = "unknown"
		}
		sb.WriteString(fmt.Sprintf("• `%s` — %s\n", d.IP, name))
	}
	return sb.String()
}

// PCAction sends a power control command (sleep/shutdown/reboot) to home-server.
func (r *GatewayRouter) PCAction(ctx context.Context, action string) string {
	if !r.home.IsEnabled() {
		return "⚠️ Home Server tidak dikonfigurasi."
	}
	result, err := r.home.PCAction(ctx, action)
	if err != nil || !result.OK {
		return fmt.Sprintf("❌ PC action '%s' gagal: %v", action, err)
	}
	icons := map[string]string{"sleep": "😴", "shutdown": "🔴", "reboot": "🔄"}
	icon := icons[action]
	if icon == "" {
		icon = "⚙️"
	}
	return fmt.Sprintf("%s *PC %s* berhasil dijalankan.", icon, strings.Title(action))
}

// RunScript asks the home-server to execute a whitelisted script.
func (r *GatewayRouter) RunScript(ctx context.Context, scriptName string) string {
	if !r.home.IsEnabled() {
		return "⚠️ Home Server tidak dikonfigurasi."
	}
	result, err := r.home.RunScript(ctx, scriptName)
	if err != nil || !result.OK {
		return fmt.Sprintf("❌ Script '%s' gagal: %v", scriptName, err)
	}
	output := result.Output
	if len(output) > 500 {
		output = output[:500] + "\n...(truncated)"
	}
	return fmt.Sprintf("✅ *Script '%s' selesai:*\n```\n%s\n```", scriptName, output)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func formatRupiah(amount int64) string {
	s := fmt.Sprintf("%d", amount)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return "Rp " + strings.Join(parts, ".")
}
