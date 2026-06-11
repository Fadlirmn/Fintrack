package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"fintrack-backend/config"
	"golang.org/x/crypto/bcrypt"
	"github.com/jmoiron/sqlx"
)

// AuthHandler handles requests related to registration, login, logout, and linking
type AuthHandler struct {
	cfg *config.Config
	db  *sqlx.DB
}

// NewAuthHandler creates a new instance of AuthHandler
func NewAuthHandler(cfg *config.Config, db *sqlx.DB) *AuthHandler {
	return &AuthHandler{
		cfg: cfg,
		db:  db,
	}
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Register creates a new user, hashes their password, and sets a JWT cookie
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || len(req.Password) < 6 {
		h.writeJSONError(w, "Email and password (minimum 6 characters) are required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Check if email already registered
	var existingID string
	err := h.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email=$1`, req.Email).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		h.writeJSONError(w, "Database lookup failure", http.StatusInternalServerError)
		return
	}
	if err == nil {
		h.writeJSONError(w, "Email address is already in use", http.StatusBadRequest)
		return
	}

	// Encrypt the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.writeJSONError(w, "Internal server security error", http.StatusInternalServerError)
		return
	}

	// Insert new user
	var userID string
	err = h.db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, monthly_income, wealth_goal)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		req.Email, string(hashedPassword), int64(10000000), int64(30),
	).Scan(&userID)
	if err != nil {
		h.writeJSONError(w, "Failed to save user account", http.StatusInternalServerError)
		return
	}

	// Generate and issue JWT
	token, err := GenerateToken(userID, req.Email, h.cfg.JWTSecret)
	if err != nil {
		h.writeJSONError(w, "Authentication session generation failed", http.StatusInternalServerError)
		return
	}

	h.setSessionCookie(w, token)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Account created successfully",
		"user": map[string]string{
			"id":    userID,
			"email": req.Email,
		},
	})
}

// Login validates user credentials and issues a JWT cookie
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	ctx := r.Context()

	var userID, passwordHash string
	err := h.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM users WHERE email=$1`,
		req.Email,
	).Scan(&userID, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		h.writeJSONError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	} else if err != nil {
		h.writeJSONError(w, "Database lookup failure", http.StatusInternalServerError)
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		h.writeJSONError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Create JWT token
	token, err := GenerateToken(userID, req.Email, h.cfg.JWTSecret)
	if err != nil {
		h.writeJSONError(w, "Authentication session generation failed", http.StatusInternalServerError)
		return
	}

	h.setSessionCookie(w, token)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Successfully authenticated",
		"user": map[string]string{
			"id":    userID,
			"email": req.Email,
		},
	})
}

// Logout removes the authentication cookie from client's browser
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message": "Logged out successfully"}`))
}

// Me returns the details of the currently authenticated user
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, email, ok := GetUserFromContext(r.Context())
	if !ok {
		h.writeJSONError(w, "Access token missing or invalid", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Fetch financial profile
	var monthlyIncome, wealthGoal int64
	err := h.db.QueryRowContext(ctx,
		`SELECT monthly_income, wealth_goal FROM users WHERE id=$1`, userID,
	).Scan(&monthlyIncome, &wealthGoal)
	if err != nil {
		monthlyIncome = 10000000
		wealthGoal = 30
	}

	// Check Telegram binding
	var chatID sql.NullString
	_ = h.db.QueryRowContext(ctx,
		`SELECT chat_id FROM telegram_binds WHERE user_id=$1 AND is_active=TRUE LIMIT 1`, userID,
	).Scan(&chatID)

	isLinked := chatID.Valid
	telegramChatID := ""
	if chatID.Valid {
		telegramChatID = chatID.String
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":               userID,
		"email":            email,
		"telegram_linked":  isLinked,
		"telegram_chat_id": telegramChatID,
		"monthly_income":   monthlyIncome,
		"wealth_goal":      wealthGoal,
	})
}

// GenerateLinkCode produces a short, secure random code to bind the account with Telegram Bot
func (h *AuthHandler) GenerateLinkCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, ok := GetUserFromContext(r.Context())
	if !ok {
		h.writeJSONError(w, "Access token missing or invalid", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	code := generateRandomCode(6)
	expiresAt := time.Now().Add(10 * time.Minute)

	// Delete any existing codes for this user first, then insert fresh one
	_, _ = h.db.ExecContext(ctx, `DELETE FROM verification_codes WHERE user_id=$1`, userID)

	_, err := h.db.ExecContext(ctx,
		`INSERT INTO verification_codes (code, user_id, expires_at) VALUES ($1, $2, $3)`,
		code, userID, expiresAt,
	)
	if err != nil {
		h.writeJSONError(w, "Could not store verification code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":       code,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// UpdateProfile modifies user financial settings (monthly income and saving goal)
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _, ok := GetUserFromContext(r.Context())
	if !ok {
		h.writeJSONError(w, "Access token missing or invalid", http.StatusUnauthorized)
		return
	}

	var req struct {
		MonthlyIncome int64 `json:"monthly_income"`
		WealthGoal    int64 `json:"wealth_goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, "Invalid input data", http.StatusBadRequest)
		return
	}

	if req.MonthlyIncome < 0 || req.WealthGoal < 0 || req.WealthGoal > 100 {
		h.writeJSONError(w, "Invalid parameters for income or target goal", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_, err := h.db.ExecContext(ctx,
		`UPDATE users SET monthly_income=$1, wealth_goal=$2 WHERE id=$3`,
		req.MonthlyIncome, req.WealthGoal, userID,
	)
	if err != nil {
		h.writeJSONError(w, "Failed to update financial profile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message": "Financial profile updated successfully",
	})
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
}

func (h *AuthHandler) writeJSONError(w http.ResponseWriter, errMsg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": errMsg,
	})
}

func generateRandomCode(n int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}
