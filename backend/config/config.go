package config

import (
	"log"
	"os"
)

type Config struct {
	Port                string
	Env                 string
	JWTSecret           string
	TelegramBotToken    string
	TelegramWebhookURL  string
	TelegramSecretToken string
	DatabaseURL         string // PostgreSQL DSN
	AllowedOrigins      string // comma-separated CORS whitelist

	// Inter-service communication
	GatewayAPIKey    string // API key protecting /internal/v1/* endpoints
	HomeServerURL    string // URL of the home-server service
	HomeServerAPIKey string // API key for calling home-server
	N8NURL           string // URL of the n8n instance (e.g. http://n8n:5678)
	N8NAPIKey        string // n8n API key (Settings → API → Create API Key)
}

// LoadConfig reads configuration values from environment variables
func LoadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default_fallback_secret_key_please_change_in_production"
		log.Println("Warning: JWT_SECRET is not set, using default fallback secret")
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Println("Warning: TELEGRAM_BOT_TOKEN is not set")
	}

	webhookURL := os.Getenv("TELEGRAM_WEBHOOK_URL")
	secretToken := os.Getenv("TELEGRAM_SECRET_TOKEN")

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://fintrack:fintrack@localhost:5432/fintrack?sslmode=disable"
		log.Println("Warning: DATABASE_URL is not set, using default local connection")
	}

	gatewayAPIKey := os.Getenv("GATEWAY_API_KEY")
	if gatewayAPIKey == "" {
		log.Println("Warning: GATEWAY_API_KEY is not set — internal endpoints are unprotected!")
	}

	homeServerURL := os.Getenv("HOME_SERVER_URL")
	if homeServerURL == "" {
		homeServerURL = "http://localhost:8090"
	}

	return &Config{
		Port:                port,
		Env:                 env,
		JWTSecret:           jwtSecret,
		TelegramBotToken:    botToken,
		TelegramWebhookURL:  webhookURL,
		TelegramSecretToken: secretToken,
		DatabaseURL:         databaseURL,
		AllowedOrigins:      os.Getenv("ALLOWED_ORIGINS"),
		GatewayAPIKey:       gatewayAPIKey,
		HomeServerURL:       homeServerURL,
		HomeServerAPIKey:    os.Getenv("HOME_SERVER_API_KEY"),
		N8NURL:              os.Getenv("N8N_URL"),
		N8NAPIKey:           os.Getenv("N8N_API_KEY"),
	}
}
