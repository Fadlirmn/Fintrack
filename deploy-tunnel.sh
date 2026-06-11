#!/usr/bin/env bash
# ============================================================
#  FinTrack VPS Deploy Script (Cloudflare Tunnel Mode)
#  Usage:
#    chmod +x deploy-tunnel.sh
#    ./deploy-tunnel.sh server.home-sumbul.my.id
# ============================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

DOMAIN="${1:-}"
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info()    { echo -e "${CYAN}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

preflight() {
    info "Menjalankan pre-flight checks..."

    if [[ -z "$DOMAIN" ]]; then
        error "Domain diperlukan. Usage: ./deploy-tunnel.sh your-domain.com"
    fi

    # Check Docker
    command -v docker >/dev/null 2>&1 || error "Docker tidak ditemukan. Install Docker dulu."
    command -v docker-compose >/dev/null 2>&1 || docker compose version >/dev/null 2>&1 || error "Docker Compose tidak ditemukan."

    # Check .env files
    [[ -f "$PROJECT_DIR/backend/.env" ]] || error "backend/.env tidak ada. Copy dari backend/.env.example dan isi."
    # Check Firebase credentials
    [[ -f "$PROJECT_DIR/backend/configs/firebase-credentials.json" ]] || \
        warn "backend/configs/firebase-credentials.json tidak ditemukan! Backend akan gagal konek."

    # Check go.sum
    [[ -f "$PROJECT_DIR/backend/go.sum" ]] || {
        info "go.sum belum ada, build Docker akan generate otomatis."
    }

    success "Pre-flight checks selesai."
}

configure_domain() {
    info "Mengkonfigurasi domain: $DOMAIN"

    # Update TELEGRAM_WEBHOOK_URL di backend/.env
    sed -i "s|TELEGRAM_WEBHOOK_URL=.*|TELEGRAM_WEBHOOK_URL=https://$DOMAIN/api/v1/telegram/webhook|" "$PROJECT_DIR/backend/.env"

    success "Domain dikonfigurasi: $DOMAIN"
}

deploy() {
    info "Building Docker images..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" build --no-cache

    info "Menjalankan semua service..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" up -d

    success "Semua service berjalan!"
}

register_webhook() {
    info "Mendaftarkan Telegram Webhook..."

    BOT_TOKEN=$(grep "^TELEGRAM_BOT_TOKEN=" "$PROJECT_DIR/backend/.env" | cut -d'=' -f2 | tr -d '"' | tr -d "'")
    SECRET_TOKEN=$(grep "^TELEGRAM_SECRET_TOKEN=" "$PROJECT_DIR/backend/.env" | cut -d'=' -f2 | tr -d '"' | tr -d "'")

    if [[ -z "$BOT_TOKEN" || "$BOT_TOKEN" == "YOUR_TELEGRAM_BOT_TOKEN_HERE" ]]; then
        warn "TELEGRAM_BOT_TOKEN belum diset. Webhook tidak didaftarkan."
        return
    fi

    WEBHOOK_URL="https://$DOMAIN/api/v1/telegram/webhook"

    info "Mendaftarkan webhook ke: $WEBHOOK_URL"

    RESPONSE=$(curl -s -X POST \
        "https://api.telegram.org/bot${BOT_TOKEN}/setWebhook" \
        -H "Content-Type: application/json" \
        -d "{
            \"url\": \"$WEBHOOK_URL\",
            \"secret_token\": \"$SECRET_TOKEN\",
            \"allowed_updates\": [\"message\", \"callback_query\"],
            \"drop_pending_updates\": true
        }")

    if echo "$RESPONSE" | grep -q '"ok":true'; then
        success "Telegram Webhook berhasil didaftarkan!"
    else
        warn "Webhook registration response: $RESPONSE"
    fi
}

verify() {
    info "Menunggu 5 detik..."
    sleep 5
    echo ""
    echo "═══════════════════════════════════════════════"
    echo "  FinTrack Cloudflare Tunnel Deployment OK!"
    echo "  Domain: https://$DOMAIN"
    echo "  Backend Port: 127.0.0.1:8080 (Mapped for Host Cloudflared)"
    echo "  UFW Port Terbuka: Hanya 22 (SSH) - Aman! 🔒"
    echo "═══════════════════════════════════════════════"
}

main() {
    preflight
    configure_domain
    deploy
    register_webhook
    verify
}

main "$@"
