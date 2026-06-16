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
	n8nGW "fintrack-backend/internal/gateway/n8n"
	"fintrack-backend/internal/telegram"
)

func main() {
	log.Println("Starting FinTrack Bot Gateway...")

	// 1. Load configuration
	cfg := config.LoadConfig()

	if cfg.TelegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required but not set")
	}

	// 2. FinTrack API config
	fintrackURL := os.Getenv("FINTRACK_API_URL")
	if fintrackURL == "" {
		fintrackURL = "http://localhost:8080"
		log.Println("Warning: FINTRACK_API_URL not set, using http://localhost:8080")
	}
	if cfg.GatewayAPIKey == "" {
		log.Println("Warning: GATEWAY_API_KEY not set")
	}

	// 3. Build service clients
	ftClient := fintrackGW.NewClient(fintrackURL, cfg.GatewayAPIKey)
	homeClient := homeGW.NewClient(cfg.HomeServerURL, cfg.HomeServerAPIKey)
	n8nClient := n8nGW.NewClient(cfg.N8NURL, cfg.N8NAPIKey)

	// Log which services are active
	log.Printf("FinTrack API:  %s", fintrackURL)
	if homeClient.IsEnabled() {
		log.Printf("Home Server:   %s ✓", cfg.HomeServerURL)
	} else {
		log.Println("Home Server:   disabled (HOME_SERVER_URL not set)")
	}
	if n8nClient.IsEnabled() {
		log.Printf("n8n:           %s ✓", cfg.N8NURL)
	} else {
		log.Println("n8n:           disabled (N8N_URL not set)")
	}

	// 4. Build gateway router
	router := gateway.NewGatewayRouter(ftClient, homeClient, n8nClient)

	// 5. Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("Received signal: %v. Shutting down...", sig)
		cancel()
	}()

	// 6. Start long polling
	poller := telegram.NewBotPoller(cfg, router)
	poller.Start(ctx)

	log.Println("Bot Gateway stopped.")
}
