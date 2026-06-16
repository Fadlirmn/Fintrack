package config

import (
	"log"
	"os"
	"strings"
)

// Config holds Home Server configuration loaded from environment variables.
type Config struct {
	Port       string
	APIKey     string
	AllowedIPs []string // Optional IP whitelist for /pc/* and /scripts/run
}

// Load reads configuration from environment variables.
func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Println("Warning: API_KEY not set — all endpoints are unprotected!")
	}

	var allowedIPs []string
	if raw := os.Getenv("ALLOWED_IPS"); raw != "" {
		for _, ip := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(ip); trimmed != "" {
				allowedIPs = append(allowedIPs, trimmed)
			}
		}
	}

	return &Config{
		Port:       port,
		APIKey:     apiKey,
		AllowedIPs: allowedIPs,
	}
}
