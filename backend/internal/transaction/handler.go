package transaction

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"fintrack-backend/internal/auth"
	"github.com/jmoiron/sqlx"
)

type Handler struct {
	db *sqlx.DB
}

// NewHandler creates a new instance of transaction Handler
func NewHandler(db *sqlx.DB) *Handler {
	return &Handler{db: db}
}

type Transaction struct {
	ID           string    `json:"id"            db:"id"`
	UserID       string    `json:"user_id"       db:"user_id"`
	CategoryName string    `json:"category_name" db:"category_name"`
	Amount       int64     `json:"amount"        db:"amount"`
	Description  string    `json:"description"   db:"description"`
	Source       string    `json:"source"        db:"source"`
	CreatedAt    time.Time `json:"created_at"    db:"created_at"`
}

type Category struct {
	ID          string `json:"id"           db:"id"`
	UserID      string `json:"user_id"      db:"user_id"`
	Name        string `json:"name"         db:"name"`
	BudgetLimit int64  `json:"budget_limit" db:"budget_limit"`
}

// Transaction Handlers

// ListTransactions retrieves all transactions associated with the logged-in user
func (h *Handler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())

	var list []Transaction
	err := h.db.SelectContext(r.Context(), &list,
		`SELECT id, user_id, category_name, amount, description, source, created_at
		 FROM transactions
		 WHERE user_id=$1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		h.writeJSONError(w, "Failed to retrieve transactions", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []Transaction{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// CreateTransaction saves a new transaction to the database
func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		CategoryName string `json:"category_name"`
		Amount       int64  `json:"amount"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid input request data", http.StatusBadRequest)
		return
	}

	req.CategoryName = strings.TrimSpace(strings.ToLower(req.CategoryName))
	if req.CategoryName == "" {
		req.CategoryName = "uncategorized"
	}
	if req.Amount <= 0 {
		h.writeJSONError(w, "Transaction amount must be a positive integer", http.StatusBadRequest)
		return
	}

	now := time.Now()
	var id string
	err := h.db.QueryRowContext(r.Context(),
		`INSERT INTO transactions (user_id, category_name, amount, description, source, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		userID, req.CategoryName, req.Amount, req.Description, "web", now,
	).Scan(&id)
	if err != nil {
		h.writeJSONError(w, "Database saving failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            id,
		"user_id":       userID,
		"category_name": req.CategoryName,
		"amount":        req.Amount,
		"description":   req.Description,
		"source":        "web",
		"created_at":    now,
	})
}

// UpdateTransaction handles updating specific transaction details
func (h *Handler) UpdateTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	txID := r.PathValue("id")
	if txID == "" {
		h.writeJSONError(w, "Transaction ID parameter required", http.StatusBadRequest)
		return
	}

	var req struct {
		CategoryName string `json:"category_name"`
		Amount       int64  `json:"amount"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid input data", http.StatusBadRequest)
		return
	}

	// Build dynamic SET clause — only update provided fields
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.CategoryName != "" {
		setClauses = append(setClauses, "category_name=$"+itoa(argIdx))
		args = append(args, strings.TrimSpace(strings.ToLower(req.CategoryName)))
		argIdx++
	}
	if req.Amount > 0 {
		setClauses = append(setClauses, "amount=$"+itoa(argIdx))
		args = append(args, req.Amount)
		argIdx++
	}
	if req.Description != "" {
		setClauses = append(setClauses, "description=$"+itoa(argIdx))
		args = append(args, req.Description)
		argIdx++
	}

	if len(setClauses) == 0 {
		h.writeJSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// Append WHERE args — AND user_id check prevents unauthorized update
	args = append(args, txID, userID)
	query := "UPDATE transactions SET " + strings.Join(setClauses, ", ") +
		" WHERE id=$" + itoa(argIdx) + " AND user_id=$" + itoa(argIdx+1)

	result, err := h.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		h.writeJSONError(w, "Transaction database update failed", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.writeJSONError(w, "Transaction not found or forbidden", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Transaction updated successfully"})
}

// DeleteTransaction removes a transaction from the database
func (h *Handler) DeleteTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	txID := r.PathValue("id")
	if txID == "" {
		h.writeJSONError(w, "Transaction ID parameter required", http.StatusBadRequest)
		return
	}

	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM transactions WHERE id=$1 AND user_id=$2`,
		txID, userID,
	)
	if err != nil {
		h.writeJSONError(w, "Database deletion failed", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.writeJSONError(w, "Transaction not found or forbidden", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Transaction deleted successfully"})
}

// Category Handlers

// ListCategories lists user-defined expense categories
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())

	var list []Category
	err := h.db.SelectContext(r.Context(), &list,
		`SELECT id, user_id, name, budget_limit FROM categories WHERE user_id=$1 ORDER BY name ASC`,
		userID,
	)
	if err != nil {
		h.writeJSONError(w, "Failed to retrieve categories", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []Category{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// CreateCategory adds a new user-defined expense category
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name        string `json:"name"`
		BudgetLimit int64  `json:"budget_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid input data", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(strings.ToLower(req.Name))
	if req.Name == "" {
		h.writeJSONError(w, "Category name cannot be empty", http.StatusBadRequest)
		return
	}

	var id string
	err := h.db.QueryRowContext(r.Context(),
		`INSERT INTO categories (user_id, name, budget_limit)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		userID, req.Name, req.BudgetLimit,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			h.writeJSONError(w, "Category already exists", http.StatusBadRequest)
			return
		}
		h.writeJSONError(w, "Failed to save category to database", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           id,
		"user_id":      userID,
		"name":         req.Name,
		"budget_limit": req.BudgetLimit,
	})
}

// UpdateCategory handles changes to category name or budget limits
func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	catID := r.PathValue("id")
	if catID == "" {
		h.writeJSONError(w, "Category ID parameter required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name        string `json:"name"`
		BudgetLimit *int64 `json:"budget_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid input data", http.StatusBadRequest)
		return
	}

	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Name != "" {
		setClauses = append(setClauses, "name=$"+itoa(argIdx))
		args = append(args, strings.TrimSpace(strings.ToLower(req.Name)))
		argIdx++
	}
	if req.BudgetLimit != nil {
		setClauses = append(setClauses, "budget_limit=$"+itoa(argIdx))
		args = append(args, *req.BudgetLimit)
		argIdx++
	}

	if len(setClauses) == 0 {
		h.writeJSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	args = append(args, catID, userID)
	query := "UPDATE categories SET " + strings.Join(setClauses, ", ") +
		" WHERE id=$" + itoa(argIdx) + " AND user_id=$" + itoa(argIdx+1)

	result, err := h.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		h.writeJSONError(w, "Database updates failed", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.writeJSONError(w, "Category not found or forbidden", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Category updated successfully"})
}

// DeleteCategory deletes a category from the database
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	catID := r.PathValue("id")
	if catID == "" {
		h.writeJSONError(w, "Category ID parameter required", http.StatusBadRequest)
		return
	}

	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM categories WHERE id=$1 AND user_id=$2`,
		catID, userID,
	)
	if err != nil {
		h.writeJSONError(w, "Database deletion failed", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.writeJSONError(w, "Category not found or forbidden", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Category deleted successfully"})
}

// GetDashboardSummary aggregates financial records via SQL
func (h *Handler) GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	ctx := r.Context()

	// 1. Aggregate transaction totals
	type agg struct {
		TotalSpending    int64 `db:"total_spending"`
		TelegramSpending int64 `db:"telegram_spending"`
		WebSpending      int64 `db:"web_spending"`
	}
	var totals agg
	_ = h.db.QueryRowxContext(ctx,
		`SELECT
			COALESCE(SUM(amount), 0)                                                     AS total_spending,
			COALESCE(SUM(amount) FILTER (WHERE source='telegram'), 0)                    AS telegram_spending,
			COALESCE(SUM(amount) FILTER (WHERE source='web'), 0)                         AS web_spending
		 FROM transactions WHERE user_id=$1`,
		userID,
	).StructScan(&totals)

	// 2. Category-level totals
	type catRow struct {
		CategoryName string `db:"category_name"`
		Total        int64  `db:"total"`
	}
	var catRows []catRow
	_ = h.db.SelectContext(ctx, &catRows,
		`SELECT category_name, COALESCE(SUM(amount),0) AS total
		 FROM transactions WHERE user_id=$1
		 GROUP BY category_name`,
		userID,
	)
	categoryTotals := make(map[string]int64)
	for _, cr := range catRows {
		categoryTotals[cr.CategoryName] = cr.Total
	}

	// 3. Budget progress
	type catBudget struct {
		Name        string `db:"name"`
		BudgetLimit int64  `db:"budget_limit"`
	}
	var cats []catBudget
	_ = h.db.SelectContext(ctx, &cats,
		`SELECT name, budget_limit FROM categories WHERE user_id=$1`,
		userID,
	)

	type BudgetProgress struct {
		CategoryName string  `json:"category_name"`
		Limit        int64   `json:"limit"`
		Spent        int64   `json:"spent"`
		Percentage   float64 `json:"percentage"`
	}
	budgets := make([]BudgetProgress, 0, len(cats))
	for _, cat := range cats {
		spent := categoryTotals[cat.Name]
		pct := 0.0
		if cat.BudgetLimit > 0 {
			pct = (float64(spent) / float64(cat.BudgetLimit)) * 100
		}
		budgets = append(budgets, BudgetProgress{
			CategoryName: cat.Name,
			Limit:        cat.BudgetLimit,
			Spent:        spent,
			Percentage:   pct,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total_spending":       totals.TotalSpending,
		"telegram_spending":    totals.TelegramSpending,
		"web_spending":         totals.WebSpending,
		"categories_breakdown": categoryTotals,
		"budget_progress":      budgets,
	})
}

func (h *Handler) writeJSONError(w http.ResponseWriter, errMsg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": errMsg,
	})
}

// itoa converts int to string for building parameterized queries
func itoa(n int) string {
	return strings.TrimSpace(strings.Join(strings.Split("0123456789", "")[:0], "") + intToStr(n))
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
