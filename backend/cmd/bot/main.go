package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fintrack-backend/config"
	"fintrack-backend/internal/db"
	"fintrack-backend/internal/telegram"
)

func main() {
	log.Println("Starting FinTrack Telegram Bot (Long Polling Mode)...")

	// 1. Load configuration from environment
	cfg := config.LoadConfig()

	// 2. Initialize Firestore connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initCancel()

	firestoreClient, err := db.InitFirestore(initCtx, cfg.FirebaseProjectID)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v\n", err)
	}
	defer firestoreClient.Close()

	// 3. Initialize the Bot Poller
	poller := telegram.NewBotPoller(cfg, firestoreClient)

	// 4. Listen for shutdown signals to exit cleanly
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Printf("Received signal: %v. Initiating graceful shutdown...\n", sig)
		cancel()
	}()

	// 5. Start long polling
	poller.Start(ctx)

	log.Println("Telegram Bot stopped.")
}
