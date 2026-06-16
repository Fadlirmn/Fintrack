package middleware

import (
	"net/http"
)

// APIKeyMiddleware protects internal endpoints by checking the X-API-Key header.
// Only requests with a matching key are forwarded to the next handler.
func APIKeyMiddleware(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If no key is configured, skip validation (dev mode)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			if r.Header.Get("X-API-Key") != key {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "Unauthorized: invalid API key"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
