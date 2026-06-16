package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"home-server/internal/config"
	"home-server/internal/handlers"
	"home-server/internal/middleware"
)

func main() {
	log.Println("Starting Home Server...")

	// 1. Load configuration
	cfg := config.Load()

	// 2. Build middleware
	apiKeyMW := middleware.APIKeyMiddleware(cfg.APIKey)
	dangerousMW := chainMiddleware(
		apiKeyMW,
		middleware.IPWhitelistMiddleware(cfg.AllowedIPs),
	)

	// 3. Register routes
	mux := http.NewServeMux()

	// Health check — public
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Info routes — API-key protected
	mux.Handle("GET /status", apiKeyMW(http.HandlerFunc(handlers.Status)))
	mux.Handle("GET /resources", apiKeyMW(http.HandlerFunc(handlers.Resources)))
	mux.Handle("GET /devices", apiKeyMW(http.HandlerFunc(handlers.Devices)))

	// Dangerous routes — API-key + IP whitelist
	mux.Handle("POST /scripts/run", dangerousMW(http.HandlerFunc(handlers.ScriptRunner)))
	mux.Handle("POST /pc/sleep", dangerousMW(http.HandlerFunc(handlers.PCControl)))
	mux.Handle("POST /pc/shutdown", dangerousMW(http.HandlerFunc(handlers.PCControl)))
	mux.Handle("POST /pc/reboot", dangerousMW(http.HandlerFunc(handlers.PCControl)))

	// 4. Start HTTP server
	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 35 * time.Second, // slightly higher for script runs
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Home Server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Home Server error: %v", err)
		}
	}()

	// 5. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down Home Server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Home Server forced shutdown: %v", err)
	}
	log.Println("Home Server stopped cleanly.")
}

// chainMiddleware chains multiple middleware functions left-to-right.
func chainMiddleware(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
