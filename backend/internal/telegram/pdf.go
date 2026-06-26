package telegram

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"fintrack-backend/internal/gateway"
	"github.com/jung-kurt/gofpdf"
)

// GenerateReceiptPDF generates a PDF summary of a scanned receipt.
// bal can be nil (omitted) — it adds a "Saldo Setelah Scan" section when provided.
func GenerateReceiptPDF(scan ScanResponse, savedAmount int64, bal *gateway.BalanceData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()

	pageW, _ := pdf.GetPageSize()
	contentW := pageW - 40
	pageH := 297.0

	// ── Header ────────────────────────────────────────────────────────────────
	pdf.SetFillColor(30, 41, 59)
	pdf.Rect(0, 0, pageW, 45, "F")
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 20)
	pdf.SetXY(20, 10)
	pdf.CellFormat(contentW, 10, "FinTrack", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(20, 22)
	pdf.CellFormat(contentW, 8, "Smart Receipt Report", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(20, 32)
	pdf.CellFormat(contentW, 8, "Generated: "+time.Now().Format("02 Jan 2006, 15:04 WIB"), "", 1, "L", false, 0, "")

	// ── Merchant Info ─────────────────────────────────────────────────────────
	pdf.SetY(55)
	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetTextColor(15, 23, 42)
	pdf.SetX(20)
	pdf.CellFormat(contentW, 10, cleanText(scan.Merchant), "", 1, "L", false, 0, "")
	dateStr := scan.Date
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetX(20)
	pdf.CellFormat(contentW, 6, "Date: "+dateStr+"   |   Category: "+strings.Title(scan.Category), "", 1, "L", false, 0, "")
	pdf.Ln(6)

	pdf.SetDrawColor(203, 213, 225)
	pdf.SetLineWidth(0.3)
	pdf.Line(20, pdf.GetY(), pageW-20, pdf.GetY())
	pdf.Ln(6)

	// ── Items Table ───────────────────────────────────────────────────────────
	if len(scan.Items) > 0 {
		colDesc := contentW * 0.70
		colPrice := contentW * 0.30
		pdf.SetFillColor(248, 250, 252)
		pdf.SetTextColor(71, 85, 105)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetX(20)
		pdf.CellFormat(colDesc, 8, "Item", "B", 0, "L", true, 0, "")
		pdf.CellFormat(colPrice, 8, "Harga", "B", 1, "R", true, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		for i, item := range scan.Items {
			if i%2 == 0 {
				pdf.SetFillColor(255, 255, 255)
			} else {
				pdf.SetFillColor(248, 250, 252)
			}
			pdf.SetTextColor(30, 41, 59)
			pdf.SetX(20)
			name := cleanText(item.Name)
			if len(name) > 50 {
				name = name[:47] + "..."
			}
			pdf.CellFormat(colDesc, 7, name, "", 0, "L", true, 0, "")
			pdf.CellFormat(colPrice, 7, formatPDFCurrency(scan.Currency, item.Price), "", 1, "R", true, 0, "")
		}
		pdf.Ln(4)
	}

	// ── Total Box ─────────────────────────────────────────────────────────────
	pdf.SetFillColor(30, 41, 59)
	pdf.SetX(20)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetTextColor(255, 255, 255)
	pdf.CellFormat(contentW*0.6, 12, "TOTAL BELANJA", "", 0, "L", true, 0, "")
	pdf.CellFormat(contentW*0.4, 12, formatPDFCurrency(scan.Currency, scan.Total), "", 1, "R", true, 0, "")

	if savedAmount > 0 {
		pdf.SetFillColor(16, 185, 129)
		pdf.SetX(20)
		pdf.SetFont("Helvetica", "", 9)
		pdf.CellFormat(contentW*0.6, 8, "Dicatat ke FinTrack", "", 0, "L", true, 0, "")
		pdf.CellFormat(contentW*0.4, 8, fmt.Sprintf("Rp %s", formatThousands(savedAmount)), "", 1, "R", true, 0, "")
	}
	pdf.Ln(6)

	// ── Saldo Box (if balance data available) ─────────────────────────────────
	if bal != nil {
		renderBalanceBox(pdf, contentW, bal)
		pdf.Ln(6)
	}

	// ── AI Analysis ───────────────────────────────────────────────────────────
	if scan.Analysis != "" {
		pdf.SetDrawColor(203, 213, 225)
		pdf.SetFillColor(248, 250, 252)
		pdf.SetTextColor(71, 85, 105)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetX(20)
		pdf.CellFormat(contentW, 7, "AI Analysis", "1", 1, "L", true, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(51, 65, 85)
		pdf.SetX(20)
		pdf.MultiCell(contentW, 5.5, cleanText(scan.Analysis), "LRB", "L", true)
		pdf.Ln(4)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	pdf.SetFillColor(30, 41, 59)
	pdf.Rect(0, pageH-18, pageW, 18, "F")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(148, 163, 184)
	pdf.SetXY(20, pageH-12)
	pdf.CellFormat(contentW, 6, "FinTrack · Personal Finance Manager · fintrack.home-sumbul.my.id", "", 0, "L", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateCombinedReceiptPDF generates a multi-page PDF for a batch of scanned receipts.
// Each receipt gets its own page, followed by a summary page with grand total.
// bal can be nil (scan-only mode).
func GenerateCombinedReceiptPDF(scans []ScanResponse, bal *gateway.BalanceData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)

	pageW, _ := pdf.GetPageSize()
	pageH := 297.0
	contentW := pageW - 40

	// ── One page per receipt ──────────────────────────────────────────────────
	for i, scan := range scans {
		pdf.AddPage()

		// Header with receipt counter badge
		pdf.SetFillColor(30, 41, 59)
		pdf.Rect(0, 0, pageW, 45, "F")
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Helvetica", "B", 18)
		pdf.SetXY(20, 10)
		pdf.CellFormat(contentW-35, 10, "FinTrack", "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "B", 9)
		pdf.CellFormat(35, 10, fmt.Sprintf("Struk %d / %d", i+1, len(scans)), "", 1, "R", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.SetXY(20, 22)
		pdf.CellFormat(contentW, 8, "Batch Receipt Report", "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetXY(20, 32)
		pdf.CellFormat(contentW, 8, "Generated: "+time.Now().Format("02 Jan 2006, 15:04 WIB"), "", 1, "L", false, 0, "")

		// Merchant
		pdf.SetY(55)
		pdf.SetFont("Helvetica", "B", 14)
		pdf.SetTextColor(15, 23, 42)
		pdf.SetX(20)
		pdf.CellFormat(contentW, 10, cleanText(scan.Merchant), "", 1, "L", false, 0, "")
		dateStr := scan.Date
		if dateStr == "" {
			dateStr = time.Now().Format("2006-01-02")
		}
		pdf.SetFont("Helvetica", "", 10)
		pdf.SetTextColor(100, 116, 139)
		pdf.SetX(20)
		pdf.CellFormat(contentW, 6, "Date: "+dateStr+"   |   Category: "+strings.Title(scan.Category), "", 1, "L", false, 0, "")
		pdf.Ln(6)

		pdf.SetDrawColor(203, 213, 225)
		pdf.SetLineWidth(0.3)
		pdf.Line(20, pdf.GetY(), pageW-20, pdf.GetY())
		pdf.Ln(6)

		// Items table
		if len(scan.Items) > 0 {
			colDesc := contentW * 0.70
			colPrice := contentW * 0.30
			pdf.SetFillColor(248, 250, 252)
			pdf.SetTextColor(71, 85, 105)
			pdf.SetFont("Helvetica", "B", 9)
			pdf.SetX(20)
			pdf.CellFormat(colDesc, 8, "Item", "B", 0, "L", true, 0, "")
			pdf.CellFormat(colPrice, 8, "Harga", "B", 1, "R", true, 0, "")
			pdf.SetFont("Helvetica", "", 9)
			for j, item := range scan.Items {
				if j%2 == 0 {
					pdf.SetFillColor(255, 255, 255)
				} else {
					pdf.SetFillColor(248, 250, 252)
				}
				pdf.SetTextColor(30, 41, 59)
				pdf.SetX(20)
				name := cleanText(item.Name)
				if len(name) > 50 {
					name = name[:47] + "..."
				}
				pdf.CellFormat(colDesc, 7, name, "", 0, "L", true, 0, "")
				pdf.CellFormat(colPrice, 7, formatPDFCurrency(scan.Currency, item.Price), "", 1, "R", true, 0, "")
			}
			pdf.Ln(4)
		}

		// Total
		pdf.SetFillColor(30, 41, 59)
		pdf.SetX(20)
		pdf.SetFont("Helvetica", "B", 11)
		pdf.SetTextColor(255, 255, 255)
		pdf.CellFormat(contentW*0.6, 12, "TOTAL", "", 0, "L", true, 0, "")
		pdf.CellFormat(contentW*0.4, 12, formatPDFCurrency(scan.Currency, scan.Total), "", 1, "R", true, 0, "")
		pdf.Ln(8)

		// Analysis
		if scan.Analysis != "" {
			pdf.SetDrawColor(203, 213, 225)
			pdf.SetFillColor(248, 250, 252)
			pdf.SetTextColor(71, 85, 105)
			pdf.SetFont("Helvetica", "B", 9)
			pdf.SetX(20)
			pdf.CellFormat(contentW, 7, "AI Analysis", "1", 1, "L", true, 0, "")
			pdf.SetFont("Helvetica", "", 9)
			pdf.SetTextColor(51, 65, 85)
			pdf.SetX(20)
			pdf.MultiCell(contentW, 5.5, cleanText(scan.Analysis), "LRB", "L", true)
			pdf.Ln(4)
		}

		// Footer
		pdf.SetFillColor(30, 41, 59)
		pdf.Rect(0, pageH-18, pageW, 18, "F")
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(148, 163, 184)
		pdf.SetXY(20, pageH-12)
		pdf.CellFormat(contentW, 6, "FinTrack · Personal Finance Manager · fintrack.home-sumbul.my.id", "", 0, "L", false, 0, "")
	}

	// ── Summary Page ──────────────────────────────────────────────────────────
	pdf.AddPage()

	pdf.SetFillColor(16, 185, 129)
	pdf.Rect(0, 0, pageW, 45, "F")
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 18)
	pdf.SetXY(20, 12)
	pdf.CellFormat(contentW, 10, "Ringkasan Batch Scan", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(20, 26)
	pdf.CellFormat(contentW, 8, fmt.Sprintf("%d struk  ·  %s", len(scans), time.Now().Format("02 Jan 2006, 15:04 WIB")), "", 1, "L", false, 0, "")

	pdf.SetY(55)

	// Summary table
	colNo := 10.0
	colMerchant := contentW * 0.40
	colCat := contentW * 0.25
	colTotal := contentW*0.35 - colNo

	pdf.SetFillColor(30, 41, 59)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetX(20)
	pdf.CellFormat(colNo, 9, "#", "", 0, "C", true, 0, "")
	pdf.CellFormat(colMerchant, 9, "Merchant", "", 0, "L", true, 0, "")
	pdf.CellFormat(colCat, 9, "Kategori", "", 0, "L", true, 0, "")
	pdf.CellFormat(colTotal, 9, "Total", "", 1, "R", true, 0, "")

	var grandTotal float64
	grandCurrency := "Rp"
	for i, scan := range scans {
		if scan.Currency != "" {
			grandCurrency = scan.Currency
		}
		grandTotal += scan.Total
		if i%2 == 0 {
			pdf.SetFillColor(255, 255, 255)
		} else {
			pdf.SetFillColor(248, 250, 252)
		}
		pdf.SetTextColor(30, 41, 59)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetX(20)
		merchant := cleanText(scan.Merchant)
		if len(merchant) > 32 {
			merchant = merchant[:29] + "..."
		}
		pdf.CellFormat(colNo, 8, fmt.Sprintf("%d", i+1), "", 0, "C", true, 0, "")
		pdf.CellFormat(colMerchant, 8, merchant, "", 0, "L", true, 0, "")
		pdf.CellFormat(colCat, 8, cleanText(strings.Title(scan.Category)), "", 0, "L", true, 0, "")
		pdf.CellFormat(colTotal, 8, formatPDFCurrency(scan.Currency, scan.Total), "", 1, "R", true, 0, "")
	}

	pdf.Ln(4)
	pdf.SetFillColor(16, 185, 129)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetX(20)
	pdf.CellFormat(colNo+colMerchant+colCat, 13, fmt.Sprintf("GRAND TOTAL  (%d struk)", len(scans)), "", 0, "L", true, 0, "")
	pdf.CellFormat(colTotal, 13, formatPDFCurrency(grandCurrency, grandTotal), "", 1, "R", true, 0, "")

	// Saldo section on summary page
	if bal != nil {
		pdf.Ln(8)
		renderBalanceBox(pdf, contentW, bal)
	}

	// Footer
	pdf.SetFillColor(30, 41, 59)
	pdf.Rect(0, pageH-18, pageW, 18, "F")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(148, 163, 184)
	pdf.SetXY(20, pageH-12)
	pdf.CellFormat(contentW, 6, "FinTrack · Personal Finance Manager · fintrack.home-sumbul.my.id", "", 0, "L", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output combined: %w", err)
	}
	return buf.Bytes(), nil
}

// renderBalanceBox draws the "Saldo Spendable" info box in the current PDF position.
func renderBalanceBox(pdf *gofpdf.Fpdf, contentW float64, bal *gateway.BalanceData) {
	// Section header
	pdf.SetFillColor(59, 130, 246) // blue-500
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetX(20)
	pdf.CellFormat(contentW, 8, "  Saldo Spendable Setelah Transaksi Ini", "1", 1, "L", true, 0, "")

	// Three columns: hari ini | minggu ini | bulan ini
	colW := contentW / 3.0

	// Row header
	pdf.SetFillColor(239, 246, 255) // blue-50
	pdf.SetTextColor(59, 130, 246)
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetX(20)
	pdf.CellFormat(colW, 7, "  Hari Ini", "LTR", 0, "L", true, 0, "")
	pdf.CellFormat(colW, 7, "  Minggu Ini", "LTR", 0, "L", true, 0, "")
	pdf.CellFormat(colW, 7, "  Bulan Ini", "LTR", 1, "L", true, 0, "")

	// Row values
	pdf.SetFillColor(255, 255, 255)
	pdf.SetTextColor(15, 23, 42)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetX(20)
	pdf.CellFormat(colW, 10, "  Rp "+formatThousands(bal.SpendableToday), "LBR", 0, "L", true, 0, "")
	pdf.CellFormat(colW, 10, "  Rp "+formatThousands(bal.SpendableWeek), "LBR", 0, "L", true, 0, "")
	pdf.CellFormat(colW, 10, "  Rp "+formatThousands(bal.SpendableMonth), "LBR", 1, "L", true, 0, "")

	// Footnote
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetTextColor(148, 163, 184)
	pdf.SetX(20)
	pdf.CellFormat(contentW, 5, fmt.Sprintf("  Pengeluaran wajib aktif: Rp %s/hari", formatThousands(bal.FixedDaily)), "", 1, "L", false, 0, "")
}

// cleanText removes or replaces characters that gofpdf (Latin-1) can't handle.
func cleanText(s string) string {
	replacer := strings.NewReplacer(
		"\u2019", "'", "\u2018", "'",
		"\u201c", "\"", "\u201d", "\"",
		"\u2013", "-", "\u2014", "--",
		"\u2026", "...",
	)
	s = replacer.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		} else {
			b.WriteRune('?')
		}
	}
	return b.String()
}

func formatPDFCurrency(currency string, amount float64) string {
	if currency == "" {
		currency = "Rp"
	}
	return fmt.Sprintf("%s %s", currency, formatThousands(int64(amount)))
}

func formatThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ".")
}
