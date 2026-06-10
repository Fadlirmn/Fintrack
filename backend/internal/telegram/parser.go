package telegram

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ParsedMessage holds the structured data extracted from a raw Telegram text message.
type ParsedMessage struct {
	Description string
	Amount      int64
	Category    string
}

// ParseMessage parses a transaction text message from a user.
// Supported formats:
// 1. "Beli kopi 25000 #makanan" -> Description: "Beli kopi", Amount: 25000, Category: "makanan"
// 2. "Beli kopi 25000" -> Description: "Beli kopi", Amount: 25000, Category: "uncategorized"
func ParseMessage(text string) (*ParsedMessage, error) {
	text = strings.TrimSpace(text)
	
	// Pattern breakdown:
	// ^(.+)        -> Capture Group 1: Description (at least one character, matches greedily)
	// \s+          -> Matches one or more spaces separating description and amount
	// (\d+)        -> Capture Group 2: Amount (numeric characters only)
	// (?:\s+#([\w-]+))?$ -> Capture Group 3 (optional): Non-capturing group for spaces, hash symbol, and category name
	pattern := `^(.+)\s+(\d+)(?:\s+#([\w-]+))?$`
	re := regexp.MustCompile(pattern)
	
	matches := re.FindStringSubmatch(text)
	if len(matches) < 3 {
		return nil, errors.New("format pesan tidak valid. Gunakan: [Deskripsi] [Nominal] #[Kategori] (Contoh: Beli kopi 25000 #makanan)")
	}
	
	description := strings.TrimSpace(matches[1])
	amountStr := matches[2]
	
	amount, err := strconv.ParseInt(amountStr, 10, 64)
	if err != nil || amount <= 0 {
		return nil, errors.New("nominal transaksi harus berupa angka positif yang valid")
	}
	
	category := "uncategorized"
	if len(matches) > 3 && matches[3] != "" {
		category = strings.ToLower(strings.TrimSpace(matches[3]))
	}
	
	return &ParsedMessage{
		Description: description,
		Amount:      amount,
		Category:    category,
	}, nil
}
