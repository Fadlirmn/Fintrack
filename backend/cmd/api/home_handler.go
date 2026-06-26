package main

// home_handler.go — Proxy HTTP handler untuk meneruskan request dari frontend
// ke layanan Home Server. Diregistrasi di main.go dengan JWT middleware.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	homeClient "fintrack-backend/internal/gateway/home"
)

// homeProxyHandler menyimpan referensi ke home client agar bisa digunakan
// sebagai receiver method (dipakai oleh main.go).
type homeProxyHandler struct {
	client *homeClient.Client
}

// newHomeProxyHandler membuat handler baru dari environment variables.
func newHomeProxyHandler() *homeProxyHandler {
	return &homeProxyHandler{
		client: homeClient.NewClient(
			os.Getenv("HOME_SERVER_URL"),
			os.Getenv("HOME_SERVER_API_KEY"),
		),
	}
}

// GetStatus godoc
// GET /api/v1/home/status (JWT protected)
// Meneruskan request ke Home Server dan mengembalikan status server.
func (h *homeProxyHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	data, err := h.client.GetStatus(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// GetResources godoc
// GET /api/v1/home/resources (JWT protected)
// Meneruskan request ke Home Server dan mengembalikan data resource (CPU/RAM/Disk).
func (h *homeProxyHandler) GetResources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	data, err := h.client.GetResources(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
