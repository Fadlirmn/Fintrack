package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fintrack-backend/config"
	"fintrack-backend/internal/db"
	"fintrack-backend/internal/telegram"
)

func main() {
	log.Println("Starting FinTrack Telegram Bot (Long Polling Mode)...")

	// 1. Load configuration from environment
	cfg := config.LoadConfig()

	// 2. Initialize PostgreSQL connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbConn, err := db.InitPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v\n", err)
	}
	defer dbConn.Close()

	// 3. Run migrations (idempotent)
	db.RunMigrations(dbConn)

	// 4. Initialize the Bot Poller
	poller := telegram.NewBotPoller(cfg, dbConn)

	// 5. Listen for shutdown signals to exit cleanly
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Printf("Received signal: %v. Initiating graceful shutdown...\n", sig)
		cancel()
	}()

	// 6. Start long polling
	poller.Start(ctx)

	log.Println("Telegram Bot stopped.")
}
