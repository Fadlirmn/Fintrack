package telegram

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// GenerateReceiptPDF generates a PDF summary of a scanned receipt.
// Returns the PDF as a byte slice ready to be sent via Telegram sendDocument.
func GenerateReceiptPDF(scan ScanResponse, savedAmount int64) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()

	pageW, _ := pdf.GetPageSize()
	contentW := pageW - 40 // 20mm margin each side

	// ── Header ────────────────────────────────────────────────────────────────

	// Title bar background
	pdf.SetFillColor(30, 41, 59) // slate-800
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
	pdf.SetTextColor(30, 41, 59)
	pdf.SetFillColor(241, 245, 249) // slate-100

	// Merchant block
	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetTextColor(15, 23, 42)
	pdf.SetX(20)
	pdf.CellFormat(contentW, 10, cleanText(scan.Merchant), "", 1, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetX(20)

	dateStr := scan.Date
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}
	pdf.CellFormat(contentW, 6, "Date: "+dateStr+"   |   Category: "+strings.Title(scan.Category), "", 1, "L", false, 0, "")

	pdf.Ln(6)

	// ── Divider ───────────────────────────────────────────────────────────────
	pdf.SetDrawColor(203, 213, 225)
	pdf.SetLineWidth(0.3)
	pdf.Line(20, pdf.GetY(), pageW-20, pdf.GetY())
	pdf.Ln(6)

	// ── Items Table ───────────────────────────────────────────────────────────

	if len(scan.Items) > 0 {
		// Table header
		pdf.SetFillColor(248, 250, 252)
		pdf.SetTextColor(71, 85, 105)
		pdf.SetFont("Helvetica", "B", 9)

		colDesc := contentW * 0.70
		colPrice := contentW * 0.30

		pdf.SetX(20)
		pdf.CellFormat(colDesc, 8, "Item", "B", 0, "L", true, 0, "")
		pdf.CellFormat(colPrice, 8, "Harga", "B", 1, "R", true, 0, "")

		// Table rows
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
			priceStr := formatPDFCurrency(scan.Currency, item.Price)
			pdf.CellFormat(colDesc, 7, name, "", 0, "L", true, 0, "")
			pdf.CellFormat(colPrice, 7, priceStr, "", 1, "R", true, 0, "")
		}

		pdf.Ln(4)
	}

	// ── Total Box ─────────────────────────────────────────────────────────────
	pdf.SetFillColor(30, 41, 59)
	pdf.SetX(20)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetTextColor(255, 255, 255)

	totalStr := formatPDFCurrency(scan.Currency, scan.Total)
	pdf.CellFormat(contentW*0.6, 12, "TOTAL", "", 0, "L", true, 0, "")
	pdf.CellFormat(contentW*0.4, 12, totalStr, "", 1, "R", true, 0, "")

	// FinTrack saved amount
	if savedAmount > 0 {
		pdf.SetFillColor(16, 185, 129) // emerald-500
		pdf.SetX(20)
		pdf.SetFont("Helvetica", "", 9)
		savedStr := fmt.Sprintf("Rp %s", formatThousands(savedAmount))
		pdf.CellFormat(contentW*0.6, 8, "Dicatat ke FinTrack", "", 0, "L", true, 0, "")
		pdf.CellFormat(contentW*0.4, 8, savedStr, "", 1, "R", true, 0, "")
	}

	pdf.Ln(8)

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
		analysis := cleanText(scan.Analysis)
		pdf.MultiCell(contentW, 5.5, analysis, "LRB", "L", true)
		pdf.Ln(4)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	pdf.SetFillColor(30, 41, 59)
	pageH := 297.0 // A4 height mm
	pdf.Rect(0, pageH-18, pageW, 18, "F")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(148, 163, 184)
	pdf.SetXY(20, pageH-12)
	pdf.CellFormat(contentW, 6, "FinTrack · Personal Finance Manager · fintrack.home-sumbul.my.id", "", 0, "L", false, 0, "")

	// ── Output to bytes ───────────────────────────────────────────────────────
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
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
