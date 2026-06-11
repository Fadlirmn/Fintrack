package fixedexpense

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fintrack-backend/internal/auth"
	"github.com/jmoiron/sqlx"
)

type Handler struct {
	db *sqlx.DB
}

func NewHandler(db *sqlx.DB) *Handler {
	return &Handler{db: db}
}

type FixedExpense struct {
	ID        string    `json:"id"         db:"id"`
	UserID    string    `json:"user_id"    db:"user_id"`
	Name      string    `json:"name"       db:"name"`
	Amount    int64     `json:"amount"     db:"amount"`
	IsActive  bool      `json:"is_active"  db:"is_active"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// List returns all fixed expenses for the logged-in user
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := auth.GetUserFromContext(r.Context())

	var list []FixedExpense
	err := h.db.SelectContext(r.Context(), &list,
		`SELECT id, user_id, name, amount, is_active, created_at
		 FROM fixed_expenses WHERE user_id=$1 ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		writeJSONError(w, "Failed to retrieve fixed expenses", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []FixedExpense{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// Create adds a new fixed expense item
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name   string `json:"name"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Amount <= 0 {
		writeJSONError(w, "Invalid input: name and positive amount required", http.StatusBadRequest)
		return
	}

	var id string
	err := h.db.QueryRowContext(r.Context(),
		`INSERT INTO fixed_expenses (user_id, name, amount)
		 VALUES ($1, $2, $3) RETURNING id`,
		userID, req.Name, req.Amount,
	).Scan(&id)
	if err != nil {
		writeJSONError(w, "Failed to create fixed expense", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":        id,
		"user_id":   userID,
		"name":      req.Name,
		"amount":    req.Amount,
		"is_active": true,
	})
}

// Update modifies name, amount, or is_active for a fixed expense
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := auth.GetUserFromContext(r.Context())
	expID := r.PathValue("id")
	if expID == "" {
		writeJSONError(w, "ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name     *string `json:"name"`
		Amount   *int64  `json:"amount"`
		IsActive *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid input", http.StatusBadRequest)
		return
	}

	// Build dynamic SET clause
	setClauses := []string{}
	args := []interface{}{}
	idx := 1

	if req.Name != nil && *req.Name != "" {
		setClauses = append(setClauses, fmt.Sprintf("name=$%d", idx))
		args = append(args, *req.Name)
		idx++
	}
	if req.Amount != nil && *req.Amount > 0 {
		setClauses = append(setClauses, fmt.Sprintf("amount=$%d", idx))
		args = append(args, *req.Amount)
		idx++
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active=$%d", idx))
		args = append(args, *req.IsActive)
		idx++
	}

	if len(setClauses) == 0 {
		writeJSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	query := fmt.Sprintf(
		"UPDATE fixed_expenses SET %s WHERE id=$%d AND user_id=$%d",
		strings.Join(setClauses, ", "), idx, idx+1,
	)
	args = append(args, expID, userID)

	result, err := h.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		writeJSONError(w, "Failed to update fixed expense", http.StatusInternalServerError)
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeJSONError(w, "Not found or forbidden", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Fixed expense updated"})
}

// Delete removes a fixed expense
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := auth.GetUserFromContext(r.Context())
	expID := r.PathValue("id")
	if expID == "" {
		writeJSONError(w, "ID required", http.StatusBadRequest)
		return
	}

	result, err := h.db.ExecContext(r.Context(),
		`DELETE FROM fixed_expenses WHERE id=$1 AND user_id=$2`, expID, userID,
	)
	if err != nil {
		writeJSONError(w, "Failed to delete fixed expense", http.StatusInternalServerError)
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeJSONError(w, "Not found or forbidden", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Fixed expense deleted"})
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
