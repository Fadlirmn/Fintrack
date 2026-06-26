package transaction

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// ── Internal Handler ──────────────────────────────────────────────────────────
// Routes under /internal/v1/* are protected by API-key middleware (not JWT).
// They are only intended to be called by the bot-gateway service.

// InternalHandler provides DB-backed endpoints for the bot gateway.
type InternalHandler struct {
	db interface {
		QueryRowContext(ctx interface{}, query string, args ...interface{}) interface{}
	}
	dbRaw interface{}
}

// We reuse the existing Handler's db field — declare a separate constructor
// so the internal handler can be used independently.

type InternalHandlerDB struct {
	h *Handler
}

// NewInternalHandler wraps an existing Handler for internal bot-gateway use.
func NewInternalHandler(h *Handler) *InternalHandlerDB {
	return &InternalHandlerDB{h: h}
}

// ── GET /internal/v1/binding?chat_id=<id> ─────────────────────────────────
// Returns the fintrack user_id linked to a Telegram chat_id.

func (ih *InternalHandlerDB) GetBinding(w http.ResponseWriter, r *http.Request) {
	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		writeInternalError(w, "chat_id query parameter required", http.StatusBadRequest)
		return
	}

	var userID string
	var isActive bool
	err := ih.h.db.QueryRowContext(r.Context(),
		`SELECT user_id, is_active FROM telegram_binds WHERE chat_id=$1`, chatID,
	).Scan(&userID, &isActive)

	if errors.Is(err, sql.ErrNoRows) || !isActive {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"linked":  false,
			"user_id": "",
		})
		return
	}
	if err != nil {
		writeInternalError(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"linked":  true,
		"user_id": userID,
	})
}

// ── POST /internal/v1/link ────────────────────────────────────────────────
// Verifies a link code and creates a telegram_binds entry.
// Body: { "chat_id": "...", "code": "...", "name": "..." }

func (ih *InternalHandlerDB) LinkAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID string `json:"chat_id"`
		Code   string `json:"code"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == "" || req.Code == "" {
		writeInternalError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Look up verification code
	var codeData struct {
		RecordID  string    `db:"id"`
		UserID    string    `db:"user_id"`
		ExpiresAt time.Time `db:"expires_at"`
	}
	err := ih.h.db.QueryRowxContext(ctx,
		`SELECT id, user_id, expires_at FROM verification_codes WHERE code=$1`, req.Code,
	).StructScan(&codeData)

	if errors.Is(err, sql.ErrNoRows) {
		writeInternalError(w, "Code not found or already used", http.StatusNotFound)
		return
	}
	if err != nil || time.Now().After(codeData.ExpiresAt) {
		writeInternalError(w, "Code expired", http.StatusUnprocessableEntity)
		return
	}

	// Transactional: insert binding + delete code
	tx, err := ih.h.db.BeginTxx(ctx, nil)
	if err != nil {
		writeInternalError(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO telegram_binds (chat_id, user_id, is_active) VALUES ($1,$2,TRUE)
		 ON CONFLICT (chat_id) DO UPDATE SET user_id=EXCLUDED.user_id, is_active=TRUE`,
		req.ChatID, codeData.UserID,
	)
	if err != nil {
		_ = tx.Rollback()
		writeInternalError(w, "Failed to save binding", http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM verification_codes WHERE id=$1`, codeData.RecordID)
	if err != nil {
		_ = tx.Rollback()
		writeInternalError(w, "Failed to delete used code", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		writeInternalError(w, "Transaction commit failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"user_id": codeData.UserID,
		"name":    req.Name,
	})
}

// ── GET /internal/v1/balance?user_id=<id> ────────────────────────────────
// Returns spendable balance data as structured JSON (bot formats the text).

func (ih *InternalHandlerDB) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeInternalError(w, "user_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var income, goal int64
	_ = ih.h.db.QueryRowContext(ctx,
		`SELECT monthly_income, wealth_goal FROM users WHERE id=$1`, userID,
	).Scan(&income, &goal)

	spendPct := float64(100-goal) / 100.0
	dailyBudget := int64(float64(income) * spendPct / 30)
	monthlyBudget := int64(float64(income) * spendPct)
	weeklyBudget := dailyBudget * 7

	var fixedTotal int64
	_ = ih.h.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM fixed_expenses WHERE user_id=$1 AND is_active=TRUE`, userID,
	).Scan(&fixedTotal)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	weekStart := todayStart.AddDate(0, 0, -int(now.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	var todaySpend, weekSpend, monthSpend int64
	_ = ih.h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, todayStart).Scan(&todaySpend)
	_ = ih.h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, weekStart).Scan(&weekSpend)
	_ = ih.h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`, userID, monthStart).Scan(&monthSpend)

	daysInMonth := int64(time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day())
	spendableToday := max64Internal(dailyBudget-fixedTotal-todaySpend, 0)
	spendableWeek := max64Internal(weeklyBudget-(fixedTotal*int64(now.Weekday()+1))-weekSpend, 0)
	spendableMonth := max64Internal(monthlyBudget-(fixedTotal*daysInMonth)-monthSpend, 0)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"daily_budget":     dailyBudget,
		"weekly_budget":    weeklyBudget,
		"monthly_budget":   monthlyBudget,
		"fixed_daily":      fixedTotal,
		"spendable_today":  spendableToday,
		"spendable_week":   spendableWeek,
		"spendable_month":  spendableMonth,
	})
}

// ── GET /internal/v1/summary?user_id=<id> ────────────────────────────────
// Returns monthly spending summary as structured JSON.

func (ih *InternalHandlerDB) GetSummary(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeInternalError(w, "user_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	type catRow struct {
		CategoryName string `db:"category_name"`
		Total        int64  `db:"total"`
	}
	var cats []catRow
	_ = ih.h.db.SelectContext(ctx, &cats,
		`SELECT category_name, COALESCE(SUM(amount),0) AS total
		 FROM transactions WHERE user_id=$1 AND created_at>=$2
		 GROUP BY category_name ORDER BY total DESC LIMIT 5`,
		userID, monthStart,
	)

	var totalMonth int64
	_ = ih.h.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`,
		userID, monthStart,
	).Scan(&totalMonth)

	// Convert to map slice for JSON
	catList := make([]map[string]interface{}, 0, len(cats))
	for _, c := range cats {
		catList = append(catList, map[string]interface{}{
			"category": c.CategoryName,
			"total":    c.Total,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"month":       now.Month().String(),
		"year":        now.Year(),
		"total":       totalMonth,
		"top_categories": catList,
	})
}

// ── POST /internal/v1/transactions ───────────────────────────────────────
// Saves a transaction from the bot (source="telegram").
// Body: { "user_id": "...", "category": "...", "amount": 12345, "description": "..." }

func (ih *InternalHandlerDB) SaveTransaction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID      string `json:"user_id"`
		Category    string `json:"category"`
		Amount      int64  `json:"amount"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInternalError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.UserID == "" || req.Amount <= 0 {
		writeInternalError(w, "user_id and positive amount required", http.StatusBadRequest)
		return
	}

	req.Category = strings.TrimSpace(strings.ToLower(req.Category))
	if req.Category == "" {
		req.Category = "uncategorized"
	}

	var id string
	err := ih.h.db.QueryRowContext(r.Context(),
		`INSERT INTO transactions (user_id, category_name, amount, description, source)
		 VALUES ($1, $2, $3, $4, 'telegram') RETURNING id`,
		req.UserID, req.Category, req.Amount, req.Description,
	).Scan(&id)
	if err != nil {
		writeInternalError(w, "Failed to save transaction", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          id,
		"user_id":     req.UserID,
		"category":    req.Category,
		"amount":      req.Amount,
		"description": req.Description,
		"source":      "telegram",
	})
}

// ── GET /internal/v1/account?user_id=<id> ────────────────────────────────
// Returns full account detail for the bot's /akun command.

func (ih *InternalHandlerDB) GetAccountDetail(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeInternalError(w, "user_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// 1. User profile
	var email string
	var monthlyIncome, wealthGoal int64
	err := ih.h.db.QueryRowContext(ctx,
		`SELECT email, monthly_income, wealth_goal FROM users WHERE id=$1`, userID,
	).Scan(&email, &monthlyIncome, &wealthGoal)
	if err != nil {
		writeInternalError(w, "User not found", http.StatusNotFound)
		return
	}

	// 2. Telegram binding
	var chatID sql.NullString
	_ = ih.h.db.QueryRowContext(ctx,
		`SELECT chat_id FROM telegram_binds WHERE user_id=$1 AND is_active=TRUE LIMIT 1`, userID,
	).Scan(&chatID)

	// 3. Monthly spending
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	var monthSpend int64
	_ = ih.h.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM transactions WHERE user_id=$1 AND created_at>=$2`,
		userID, monthStart,
	).Scan(&monthSpend)

	// 4. Transaction count this month
	var txCount int
	_ = ih.h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id=$1 AND created_at>=$2`,
		userID, monthStart,
	).Scan(&txCount)

	telegramChatID := ""
	if chatID.Valid {
		telegramChatID = chatID.String
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":        userID,
		"email":          email,
		"monthly_income": monthlyIncome,
		"wealth_goal":    wealthGoal,
		"telegram_linked": chatID.Valid,
		"telegram_chat_id": telegramChatID,
		"month_spending": monthSpend,
		"month_tx_count": txCount,
		"month":          now.Month().String(),
		"year":           now.Year(),
	})
}

// ── helpers ───────────────────────────────────────────────────────────────

func writeInternalError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func max64Internal(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
