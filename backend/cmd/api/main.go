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
	"fintrack-backend/internal/fixedexpense"
	"fintrack-backend/internal/middleware"
	"fintrack-backend/internal/transaction"
)

func main() {
	log.Println("Starting FinTrack API Server...")

	// 1. Load configuration from environment
	cfg := config.LoadConfig()

	// 2. Initialize PostgreSQL connection
	dbConn, err := db.InitPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database connection: %v\n", err)
	}
	defer dbConn.Close()

	// 3. Run schema migrations (idempotent — safe on every restart)
	db.RunMigrations(dbConn)

	// 4. Register HTTP Route Handlers
	mux := http.NewServeMux()

	// Public Health Check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// ── Instantiate Handlers ──────────────────────────────────────────────
	authHandler := auth.NewAuthHandler(cfg, dbConn)
	txHandler := transaction.NewHandler(dbConn)
	feHandler := fixedexpense.NewHandler(dbConn)

	// Internal handler — for bot-gateway inter-service calls (API-key protected)
	internalTxHandler := transaction.NewInternalHandler(txHandler)

	// ── Auth Middleware ───────────────────────────────────────────────────
	authMiddleware := auth.AuthMiddleware(cfg)
	// API-key middleware for internal routes (called by bot-gateway)
	apiKeyMiddleware := middleware.APIKeyMiddleware(cfg.GatewayAPIKey)

	// ── Public Auth Endpoints ─────────────────────────────────────────────
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.Register)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)

	// ── Protected Auth Endpoints ──────────────────────────────────────────
	mux.Handle("GET /api/v1/auth/me", authMiddleware(http.HandlerFunc(authHandler.Me)))
	mux.Handle("PUT /api/v1/auth/profile", authMiddleware(http.HandlerFunc(authHandler.UpdateProfile)))
	mux.Handle("POST /api/v1/telegram/link-code", authMiddleware(http.HandlerFunc(authHandler.GenerateLinkCode)))

	// ── Transaction Endpoints (JWT Protected) ─────────────────────────────
	mux.Handle("GET /api/v1/transactions", authMiddleware(http.HandlerFunc(txHandler.ListTransactions)))
	mux.Handle("POST /api/v1/transactions", authMiddleware(http.HandlerFunc(txHandler.CreateTransaction)))
	mux.Handle("PUT /api/v1/transactions/{id}", authMiddleware(http.HandlerFunc(txHandler.UpdateTransaction)))
	mux.Handle("DELETE /api/v1/transactions/{id}", authMiddleware(http.HandlerFunc(txHandler.DeleteTransaction)))

	// ── Category Endpoints (JWT Protected) ───────────────────────────────
	mux.Handle("GET /api/v1/categories", authMiddleware(http.HandlerFunc(txHandler.ListCategories)))
	mux.Handle("POST /api/v1/categories", authMiddleware(http.HandlerFunc(txHandler.CreateCategory)))
	mux.Handle("PUT /api/v1/categories/{id}", authMiddleware(http.HandlerFunc(txHandler.UpdateCategory)))
	mux.Handle("DELETE /api/v1/categories/{id}", authMiddleware(http.HandlerFunc(txHandler.DeleteCategory)))

	// ── Dashboard (JWT Protected) ─────────────────────────────────────────
	mux.Handle("GET /api/v1/dashboard/summary", authMiddleware(http.HandlerFunc(txHandler.GetDashboardSummary)))

	// ── Fixed Expenses (JWT Protected) ────────────────────────────────────
	mux.Handle("GET /api/v1/fixed-expenses", authMiddleware(http.HandlerFunc(feHandler.List)))
	mux.Handle("POST /api/v1/fixed-expenses", authMiddleware(http.HandlerFunc(feHandler.Create)))
	mux.Handle("PUT /api/v1/fixed-expenses/{id}", authMiddleware(http.HandlerFunc(feHandler.Update)))
	mux.Handle("DELETE /api/v1/fixed-expenses/{id}", authMiddleware(http.HandlerFunc(feHandler.Delete)))

	// ── Internal Routes (API-key Protected) — used by bot-gateway ─────────
	mux.Handle("GET /internal/v1/binding", apiKeyMiddleware(http.HandlerFunc(internalTxHandler.GetBinding)))
	mux.Handle("POST /internal/v1/link", apiKeyMiddleware(http.HandlerFunc(internalTxHandler.LinkAccount)))
	mux.Handle("GET /internal/v1/balance", apiKeyMiddleware(http.HandlerFunc(internalTxHandler.GetBalance)))
	mux.Handle("GET /internal/v1/summary", apiKeyMiddleware(http.HandlerFunc(internalTxHandler.GetSummary)))
	mux.Handle("POST /internal/v1/transactions", apiKeyMiddleware(http.HandlerFunc(internalTxHandler.SaveTransaction)))

	// Apply CORS wrapper (whitelist from env ALLOWED_ORIGINS)
	allowedOrigins := getAllowedOrigins()
	mainHandler := corsMiddleware(mux, allowedOrigins)

	// 5. Initialize HTTP Server
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

	// 6. Graceful shutdown handler
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

// getAllowedOrigins reads the ALLOWED_ORIGINS env var (comma-separated).
func getAllowedOrigins() []string {
	raw := os.Getenv("ALLOWED_ORIGINS")
	if raw == "" {
		log.Println("Warning: ALLOWED_ORIGINS tidak diset. CORS terbuka untuk semua origin.")
		return nil
	}
	origins := strings.Split(raw, ",")
	result := make([]string, 0, len(origins))
	for _, o := range origins {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			trimmed = strings.TrimSuffix(trimmed, "/")
			result = append(result, trimmed)
		}
	}
	log.Printf("CORS: Allowed origins: %v\n", result)
	return result
}

// corsMiddleware handles CORS with origin whitelist.
func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	isAllowed := func(origin string) bool {
		if len(allowedOrigins) == 0 {
			return true
		}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
			if strings.HasPrefix(allowed, "*.") {
				suffix := allowed[1:]
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
