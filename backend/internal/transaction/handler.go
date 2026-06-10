package transaction

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"fintrack-backend/internal/auth"
	"google.golang.org/api/iterator"
)

type Handler struct {
	db *firestore.Client
}

// NewHandler creates a new instance of transaction Handler
func NewHandler(db *firestore.Client) *Handler {
	return &Handler{
		db: db,
	}
}

type Transaction struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	CategoryName string    `json:"category_name"`
	Amount       int64     `json:"amount"`
	Description  string    `json:"description"`
	Source       string    `json:"source"`
	CreatedAt    time.Time `json:"created_at"`
}

type Category struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	BudgetLimit int64  `json:"budget_limit"`
}

// Transaction Handlers

// ListTransactions retrieves all transactions associated with the logged-in user
func (h *Handler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	ctx := r.Context()

	iter := h.db.Collection("transactions").
		Where("user_id", "==", userID).
		Documents(ctx)
	defer iter.Stop()

	list := make([]Transaction, 0)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			h.writeJSONError(w, "Failed to retrieve transactions", http.StatusInternalServerError)
			return
		}

		var tx Transaction
		if err := doc.DataTo(&tx); err != nil {
			continue
		}
		tx.ID = doc.Ref.ID
		list = append(list, tx)
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
	ctx := r.Context()

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
	txData := map[string]interface{}{
		"user_id":       userID,
		"category_name": req.CategoryName,
		"amount":        req.Amount,
		"description":   req.Description,
		"source":        "web",
		"created_at":    now,
	}

	docRef, _, err := h.db.Collection("transactions").Add(ctx, txData)
	if err != nil {
		h.writeJSONError(w, "Database saving failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            docRef.ID,
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

	ctx := r.Context()
	docRef := h.db.Collection("transactions").Doc(txID)
	doc, err := docRef.Get(ctx)
	if err != nil {
		h.writeJSONError(w, "Transaction document not found", http.StatusNotFound)
		return
	}

	var existing Transaction
	if err := doc.DataTo(&existing); err != nil {
		h.writeJSONError(w, "Failed to parse document data", http.StatusInternalServerError)
		return
	}

	if existing.UserID != userID {
		h.writeJSONError(w, "Forbidden: transaction belongs to another account", http.StatusForbidden)
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

	updates := make([]firestore.Update, 0)
	if req.CategoryName != "" {
		updates = append(updates, firestore.Update{Path: "category_name", Value: strings.TrimSpace(strings.ToLower(req.CategoryName))})
	}
	if req.Amount > 0 {
		updates = append(updates, firestore.Update{Path: "amount", Value: req.Amount})
	}
	if req.Description != "" {
		updates = append(updates, firestore.Update{Path: "description", Value: req.Description})
	}

	if len(updates) > 0 {
		_, err = docRef.Update(ctx, updates)
		if err != nil {
			h.writeJSONError(w, "Transaction database update failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Transaction updated successfully"})
}

// DeleteTransaction removes a transaction from Firestore
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

	ctx := r.Context()
	docRef := h.db.Collection("transactions").Doc(txID)
	doc, err := docRef.Get(ctx)
	if err != nil {
		h.writeJSONError(w, "Transaction document not found", http.StatusNotFound)
		return
	}

	var existing Transaction
	if err := doc.DataTo(&existing); err != nil {
		h.writeJSONError(w, "Failed to parse document data", http.StatusInternalServerError)
		return
	}

	if existing.UserID != userID {
		h.writeJSONError(w, "Forbidden: transaction belongs to another account", http.StatusForbidden)
		return
	}

	_, err = docRef.Delete(ctx)
	if err != nil {
		h.writeJSONError(w, "Database deletion failed", http.StatusInternalServerError)
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
	ctx := r.Context()

	iter := h.db.Collection("categories").Where("user_id", "==", userID).Documents(ctx)
	defer iter.Stop()

	list := make([]Category, 0)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			h.writeJSONError(w, "Failed to retrieve categories", http.StatusInternalServerError)
			return
		}

		var cat Category
		if err := doc.DataTo(&cat); err != nil {
			continue
		}
		cat.ID = doc.Ref.ID
		list = append(list, cat)
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
	ctx := r.Context()

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

	// Verify uniqueness for this user
	iter := h.db.Collection("categories").Where("user_id", "==", userID).Where("name", "==", req.Name).Documents(ctx)
	defer iter.Stop()
	_, err := iter.Next()
	if err != iterator.Done {
		h.writeJSONError(w, "Category already exists", http.StatusBadRequest)
		return
	}

	catData := map[string]interface{}{
		"user_id":      userID,
		"name":         req.Name,
		"budget_limit": req.BudgetLimit,
		"created_at":   time.Now(),
	}

	docRef, _, err := h.db.Collection("categories").Add(ctx, catData)
	if err != nil {
		h.writeJSONError(w, "Failed to save category to database", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           docRef.ID,
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

	ctx := r.Context()
	docRef := h.db.Collection("categories").Doc(catID)
	doc, err := docRef.Get(ctx)
	if err != nil {
		h.writeJSONError(w, "Category document not found", http.StatusNotFound)
		return
	}

	var existing Category
	if err := doc.DataTo(&existing); err != nil {
		h.writeJSONError(w, "Failed to parse category document", http.StatusInternalServerError)
		return
	}

	if existing.UserID != userID {
		h.writeJSONError(w, "Forbidden: category belongs to another account", http.StatusForbidden)
		return
	}

	var req struct {
		Name        string `json:"name"`
		BudgetLimit int64  `json:"budget_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid input data", http.StatusBadRequest)
		return
	}

	updates := make([]firestore.Update, 0)
	if req.Name != "" {
		updates = append(updates, firestore.Update{Path: "name", Value: strings.TrimSpace(strings.ToLower(req.Name))})
	}
	if req.BudgetLimit >= 0 {
		updates = append(updates, firestore.Update{Path: "budget_limit", Value: req.BudgetLimit})
	}

	if len(updates) > 0 {
		_, err = docRef.Update(ctx, updates)
		if err != nil {
			h.writeJSONError(w, "Database updates failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Category updated successfully"})
}

// DeleteCategory deletes a category from Firestore
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

	ctx := r.Context()
	docRef := h.db.Collection("categories").Doc(catID)
	doc, err := docRef.Get(ctx)
	if err != nil {
		h.writeJSONError(w, "Category document not found", http.StatusNotFound)
		return
	}

	var existing Category
	if err := doc.DataTo(&existing); err != nil {
		h.writeJSONError(w, "Failed to parse category document", http.StatusInternalServerError)
		return
	}

	if existing.UserID != userID {
		h.writeJSONError(w, "Forbidden: category belongs to another account", http.StatusForbidden)
		return
	}

	_, err = docRef.Delete(ctx)
	if err != nil {
		h.writeJSONError(w, "Database deletion failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Category deleted successfully"})
}

// Summary Dashboard Stats

// GetDashboardSummary aggregates financial records and maps out total spending, telegram spending, and budget indicators
func (h *Handler) GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, _ := auth.GetUserFromContext(r.Context())
	ctx := r.Context()

	// 1. Accumulate transactions
	txIter := h.db.Collection("transactions").Where("user_id", "==", userID).Documents(ctx)
	defer txIter.Stop()

	var totalSpending int64
	var telegramSpending int64
	var webSpending int64
	categoryTotals := make(map[string]int64)

	for {
		doc, err := txIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			h.writeJSONError(w, "Data aggregation query failed", http.StatusInternalServerError)
			return
		}

		var tx Transaction
		if err := doc.DataTo(&tx); err != nil {
			continue
		}

		totalSpending += tx.Amount
		if tx.Source == "telegram" {
			telegramSpending += tx.Amount
		} else {
			webSpending += tx.Amount
		}

		categoryTotals[tx.CategoryName] += tx.Amount
	}

	// 2. Map budgets
	catIter := h.db.Collection("categories").Where("user_id", "==", userID).Documents(ctx)
	defer catIter.Stop()

	type BudgetProgress struct {
		CategoryName string  `json:"category_name"`
		Limit        int64   `json:"limit"`
		Spent        int64   `json:"spent"`
		Percentage   float64 `json:"percentage"`
	}

	budgets := make([]BudgetProgress, 0)
	for {
		doc, err := catIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			h.writeJSONError(w, "Failed to load budget progress", http.StatusInternalServerError)
			return
		}

		var cat Category
		if err := doc.DataTo(&cat); err != nil {
			continue
		}

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
		"total_spending":       totalSpending,
		"telegram_spending":    telegramSpending,
		"web_spending":         webSpending,
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
