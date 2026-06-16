package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fintrack-backend/config"
	"fintrack-backend/internal/gateway"
	fintrackGW "fintrack-backend/internal/gateway/fintrack"
	homeGW "fintrack-backend/internal/gateway/home"
	"fintrack-backend/internal/telegram"
)

func main() {
	log.Println("Starting FinTrack Bot Gateway...")

	// 1. Load configuration
	cfg := config.LoadConfig()

	// Validate required bot token
	if cfg.TelegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required but not set")
	}

	// 2. Validate FinTrack API connection config
	fintrackURL := os.Getenv("FINTRACK_API_URL")
	if fintrackURL == "" {
		fintrackURL = "http://localhost:8080"
		log.Println("Warning: FINTRACK_API_URL not set, using default http://localhost:8080")
	}
	if cfg.GatewayAPIKey == "" {
		log.Println("Warning: GATEWAY_API_KEY not set — internal FinTrack calls will be unprotected")
	}

	// 3. Build service clients
	ftClient := fintrackGW.NewClient(fintrackURL, cfg.GatewayAPIKey)
	homeClient := homeGW.NewClient(cfg.HomeServerURL, cfg.HomeServerAPIKey)

	if homeClient.IsEnabled() {
		log.Printf("Home Server enabled at %s", cfg.HomeServerURL)
	} else {
		log.Println("Home Server disabled (HOME_SERVER_URL not set)")
	}

	// 4. Build gateway router (orchestrator)
	router := gateway.NewGatewayRouter(ftClient, homeClient)

	// 5. Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Printf("Received signal: %v. Initiating graceful shutdown...", sig)
		cancel()
	}()

	// 6. Start long polling
	poller := telegram.NewBotPoller(cfg, router)
	poller.Start(ctx)

	log.Println("Bot Gateway stopped.")
}
