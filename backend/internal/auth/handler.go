package auth

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"fintrack-backend/config"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/iterator"
)

// AuthHandler handles requests related to registration, login, logout, and linking
type AuthHandler struct {
	cfg *config.Config
	db  *firestore.Client
}

// NewAuthHandler creates a new instance of AuthHandler
func NewAuthHandler(cfg *config.Config, db *firestore.Client) *AuthHandler {
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
	iter := h.db.Collection("users").Where("email", "==", req.Email).Documents(ctx)
	defer iter.Stop()
	_, err := iter.Next()
	if err != iterator.Done {
		h.writeJSONError(w, "Email address is already in use", http.StatusBadRequest)
		return
	}

	// Encrypt the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.writeJSONError(w, "Internal server security error", http.StatusInternalServerError)
		return
	}

	// Write new user document
	userRef := h.db.Collection("users").NewDoc()
	userID := userRef.ID

	_, err = userRef.Set(ctx, map[string]interface{}{
		"email":         req.Email,
		"password_hash": string(hashedPassword),
		"created_at":    time.Now(),
	})
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
	iter := h.db.Collection("users").Where("email", "==", req.Email).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		h.writeJSONError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	} else if err != nil {
		h.writeJSONError(w, "Database lookup failure", http.StatusInternalServerError)
		return
	}

	var userData struct {
		PasswordHash string `firestore:"password_hash"`
	}
	if err := doc.DataTo(&userData); err != nil {
		h.writeJSONError(w, "Failed to read user data", http.StatusInternalServerError)
		return
	}

	// Check password hash
	err = bcrypt.CompareHashAndPassword([]byte(userData.PasswordHash), []byte(req.Password))
	if err != nil {
		h.writeJSONError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	userID := doc.Ref.ID

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
		Secure:   h.cfg.Env == "production",
		SameSite: http.SameSiteLaxMode,
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
	isLinked := false
	var telegramChatID string

	// Check if Telegram binding exists
	iter := h.db.Collection("telegram_binds").Where("user_id", "==", userID).Documents(ctx)
	defer iter.Stop()
	bindDoc, err := iter.Next()
	if err == nil {
		isLinked = true
		telegramChatID = bindDoc.Ref.ID
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":               userID,
		"email":            email,
		"telegram_linked":  isLinked,
		"telegram_chat_id": telegramChatID,
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

	_, err := h.db.Collection("verification_codes").NewDoc().Set(ctx, map[string]interface{}{
		"code":       code,
		"user_id":    userID,
		"expires_at": expiresAt,
	})
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

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Env == "production",
		SameSite: http.SameSiteLaxMode,
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
