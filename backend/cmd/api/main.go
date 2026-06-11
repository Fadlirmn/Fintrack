package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fintrack-backend/config"
	"fintrack-backend/internal/auth"
	"fintrack-backend/internal/db"
	"fintrack-backend/internal/telegram"
	"fintrack-backend/internal/transaction"
)

func main() {
	log.Println("Starting FinTrack API Server...")

	// 1. Load configuration from environment
	cfg := config.LoadConfig()

	// 2. Initialize Firestore connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	firestoreClient, err := db.InitFirestore(ctx, cfg.FirebaseProjectID)
	if err != nil {
		log.Fatalf("Failed to initialize database connections: %v\n", err)
	}
	defer firestoreClient.Close()

	// 3. Register HTTP Route Handlers
	mux := http.NewServeMux()

	// Public Health Check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Instantiate handlers
	authHandler := auth.NewAuthHandler(cfg, firestoreClient)
	txHandler := transaction.NewHandler(firestoreClient)
	webhookHandler := telegram.NewWebhookHandler(cfg, firestoreClient)

	// Telegram Webhook endpoint (Verify request header to ensure safety)
	mux.Handle("/api/v1/telegram/webhook", webhookHandler)

	// Auth Endpoints (Public)
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.Register)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)

	// Auth Middleware Setup
	authMiddleware := auth.AuthMiddleware(cfg)

	// Auth Endpoints (Protected)
	mux.Handle("GET /api/v1/auth/me", authMiddleware(http.HandlerFunc(authHandler.Me)))
	mux.Handle("PUT /api/v1/auth/profile", authMiddleware(http.HandlerFunc(authHandler.UpdateProfile)))
	mux.Handle("POST /api/v1/telegram/link-code", authMiddleware(http.HandlerFunc(authHandler.GenerateLinkCode)))

	// Transaction Endpoints (Protected)
	mux.Handle("GET /api/v1/transactions", authMiddleware(http.HandlerFunc(txHandler.ListTransactions)))
	mux.Handle("POST /api/v1/transactions", authMiddleware(http.HandlerFunc(txHandler.CreateTransaction)))
	mux.Handle("PUT /api/v1/transactions/{id}", authMiddleware(http.HandlerFunc(txHandler.UpdateTransaction)))
	mux.Handle("DELETE /api/v1/transactions/{id}", authMiddleware(http.HandlerFunc(txHandler.DeleteTransaction)))

	// Category Endpoints (Protected)
	mux.Handle("GET /api/v1/categories", authMiddleware(http.HandlerFunc(txHandler.ListCategories)))
	mux.Handle("POST /api/v1/categories", authMiddleware(http.HandlerFunc(txHandler.CreateCategory)))
	mux.Handle("PUT /api/v1/categories/{id}", authMiddleware(http.HandlerFunc(txHandler.UpdateCategory)))
	mux.Handle("DELETE /api/v1/categories/{id}", authMiddleware(http.HandlerFunc(txHandler.DeleteCategory)))

	// Dashboard Aggregation Endpoints (Protected)
	mux.Handle("GET /api/v1/dashboard/summary", authMiddleware(http.HandlerFunc(txHandler.GetDashboardSummary)))

	// Apply CORS wrapper (whitelist dari env ALLOWED_ORIGINS)
	allowedOrigins := getAllowedOrigins()
	mainHandler := corsMiddleware(mux, allowedOrigins)

	// 4. Initialize HTTP Server
	serverAddr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: mainHandler,
	}

	// Run server in background goroutine
	go func() {
		log.Printf("Server is running on address %s\n", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server ListenAndServe failed: %v\n", err)
		}
	}()

	// 5. Graceful shutdown handler
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	
	log.Println("Shutting down server gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v\n", err)
	}

	log.Println("Server stopped cleanly.")
}

// getAllowedOrigins membaca daftar origin yang diizinkan dari env var ALLOWED_ORIGINS.
// Format: comma-separated, contoh: "https://fintrack.vercel.app,https://fintrack.kamu.id"
// Jika kosong, fallback ke "*" (open CORS — TIDAK recommended untuk production).
func getAllowedOrigins() []string {
	raw := os.Getenv("ALLOWED_ORIGINS")
	if raw == "" {
		log.Println("Warning: ALLOWED_ORIGINS tidak diset. CORS terbuka untuk semua origin.")
		return nil // nil = allow all
	}
	origins := strings.Split(raw, ",")
	result := make([]string, 0, len(origins))
	for _, o := range origins {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			// Trim trailing slash to prevent CORS mismatch issues
			trimmed = strings.TrimSuffix(trimmed, "/")
			result = append(result, trimmed)
		}
	}
	log.Printf("CORS: Allowed origins: %v\n", result)
	return result
}

// corsMiddleware menangani CORS dengan whitelist origin.
// Mendukung Vercel preview URLs dan custom domain.
func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	isAllowed := func(origin string) bool {
		if len(allowedOrigins) == 0 {
			return true // allow all jika tidak dikonfigurasi
		}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
			// Support wildcard untuk Vercel preview: *.vercel.app
			if strings.HasPrefix(allowed, "*.") {
				suffix := allowed[1:] // ".vercel.app"
				if strings.HasSuffix(origin, suffix) {
					return true
				}
			}
		}
		return false
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" && isAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else if origin == "" {
			// Direct API call (tidak ada Origin header) — izinkan
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
