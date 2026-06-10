package auth

import (
	"context"
	"net/http"
	"strings"

	"fintrack-backend/config"
)

type contextKey string

const (
	UserIDKey contextKey = "user_id"
	EmailKey  contextKey = "email"
)

// AuthMiddleware intercepts HTTP requests, extracts the JWT token from either the Cookie or Authorization header,
// and injects the verified UserID and Email into the request's context.
func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenStr string

			// 1. Try reading the secure http-only cookie
			cookie, err := r.Cookie("token")
			if err == nil {
				tokenStr = cookie.Value
			}

			// 2. Fallback to Authorization Header (Bearer token)
			if tokenStr == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			if tokenStr == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "Authentication token missing"}`))
				return
			}

			// Validate token
			claims, err := ValidateToken(tokenStr, cfg.JWTSecret)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "Invalid or expired session"}`))
				return
			}

			// Inject user ID and email into context
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, EmailKey, claims.Email)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserFromContext helper extracts the UserID and Email from the request context
func GetUserFromContext(ctx context.Context) (string, string, bool) {
	userID, ok1 := ctx.Value(UserIDKey).(string)
	email, ok2 := ctx.Value(EmailKey).(string)
	return userID, email, ok1 && ok2
}
