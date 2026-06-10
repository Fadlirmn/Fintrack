#!/usr/bin/env bash
# ============================================================
#  FinTrack VPS Deploy Script
#  Referensi: Leo-bot-new/run_auto.sh + DreamAPI patterns
#
#  Usage:
#    chmod +x deploy.sh
#    ./deploy.sh [domain] [email]
#
#  Contoh:
#    ./deploy.sh fintrack.kamu.id admin@kamu.id
# ============================================================

set -euo pipefail

# ── Colors ───────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# ── Config ───────────────────────────────────────────────────
DOMAIN="${1:-}"
EMAIL="${2:-}"
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Helper functions ─────────────────────────────────────────
info()    { echo -e "${CYAN}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ── Pre-flight checks ────────────────────────────────────────
preflight() {
    info "Menjalankan pre-flight checks..."

    # Check domain argument
    if [[ -z "$DOMAIN" ]]; then
        error "Domain diperlukan. Usage: ./deploy.sh your-domain.com your@email.com"
    fi
    if [[ -z "$EMAIL" ]]; then
        error "Email diperlukan untuk SSL certificate. Usage: ./deploy.sh your-domain.com your@email.com"
    fi

    # Check Docker
    command -v docker >/dev/null 2>&1 || error "Docker tidak ditemukan. Install Docker dulu."
    command -v docker-compose >/dev/null 2>&1 || docker compose version >/dev/null 2>&1 || error "Docker Compose tidak ditemukan."

    # Check .env files
    [[ -f "$PROJECT_DIR/backend/.env" ]] || error "backend/.env tidak ada. Copy dari backend/.env.example dan isi."
    [[ -f "$PROJECT_DIR/frontend/.env" ]] || error "frontend/.env tidak ada. Copy dari frontend/.env.example dan isi."

    # Check Firebase credentials
    [[ -f "$PROJECT_DIR/backend/configs/firebase-credentials.json" ]] || \
        warn "backend/configs/firebase-credentials.json tidak ditemukan! Backend akan gagal konek ke Firebase."

    # Check go.sum ada (diperlukan untuk Docker build)
    [[ -f "$PROJECT_DIR/backend/go.sum" ]] || {
        info "go.sum belum ada, generate dulu..."
        generate_gosum
    }

    success "Pre-flight checks selesai."
}

# ── Generate go.sum ──────────────────────────────────────────
generate_gosum() {
    if command -v go >/dev/null 2>&1; then
        info "Generating go.sum dengan 'go mod tidy'..."
        cd "$PROJECT_DIR/backend"
        go mod tidy
        cd "$PROJECT_DIR"
        success "go.sum berhasil dibuat."
    else
        warn "Go tidak terinstall di server. Build Docker akan generate go.sum otomatis."
    fi
}

# ── Update domain di config ──────────────────────────────────
configure_domain() {
    info "Mengkonfigurasi domain: $DOMAIN"

    # Replace YOUR_DOMAIN.COM di nginx config
    sed -i "s/YOUR_DOMAIN\.COM/$DOMAIN/g" "$PROJECT_DIR/nginx/conf.d/fintrack.conf"

    # Update TELEGRAM_WEBHOOK_URL di backend/.env
    if grep -q "TELEGRAM_WEBHOOK_URL=.*your-domain" "$PROJECT_DIR/backend/.env" 2>/dev/null; then
        sed -i "s|TELEGRAM_WEBHOOK_URL=.*|TELEGRAM_WEBHOOK_URL=https://$DOMAIN/api/v1/telegram/webhook|" "$PROJECT_DIR/backend/.env"
    fi

    success "Domain dikonfigurasi: $DOMAIN"
}

# ── Step 1: Dapatkan SSL certificate ─────────────────────────
init_ssl() {
    info "Memulai proses SSL certificate untuk $DOMAIN..."

    mkdir -p "$PROJECT_DIR/certbot/conf"
    mkdir -p "$PROJECT_DIR/certbot/www"

    # Jalankan nginx hanya dengan HTTP dulu (untuk ACME challenge)
    info "Menjalankan Nginx (HTTP-only mode) untuk verifikasi domain..."

    # Buat config temporary yang hanya HTTP
    cat > "$PROJECT_DIR/nginx/conf.d/fintrack-init.conf" << EOF
server {
    listen 80;
    server_name $DOMAIN www.$DOMAIN;
    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }
    location / {
        return 200 'FinTrack SSL Init';
        add_header Content-Type text/plain;
    }
}
EOF

    # Backup HTTPS config
    mv "$PROJECT_DIR/nginx/conf.d/fintrack.conf" "$PROJECT_DIR/nginx/conf.d/fintrack.conf.bak" 2>/dev/null || true
    mv "$PROJECT_DIR/nginx/conf.d/fintrack-init.conf" "$PROJECT_DIR/nginx/conf.d/fintrack.conf"

    docker compose -f "$PROJECT_DIR/docker-compose.yml" up -d nginx

    sleep 3

    # Request certificate
    info "Meminta SSL certificate dari Let's Encrypt..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" run --rm certbot \
        certonly \
        --webroot \
        --webroot-path=/var/www/certbot \
        --email "$EMAIL" \
        --agree-tos \
        --no-eff-email \
        -d "$DOMAIN" \
        -d "www.$DOMAIN" || {
            warn "www.$DOMAIN mungkin tidak terkonfigurasi. Mencoba hanya $DOMAIN..."
            docker compose -f "$PROJECT_DIR/docker-compose.yml" run --rm certbot \
                certonly \
                --webroot \
                --webroot-path=/var/www/certbot \
                --email "$EMAIL" \
                --agree-tos \
                --no-eff-email \
                -d "$DOMAIN"
        }

    # Restore HTTPS config
    mv "$PROJECT_DIR/nginx/conf.d/fintrack.conf.bak" "$PROJECT_DIR/nginx/conf.d/fintrack.conf" 2>/dev/null || true

    docker compose -f "$PROJECT_DIR/docker-compose.yml" stop nginx
    success "SSL certificate berhasil didapat."
}

# ── Step 2: Build dan Deploy semua service ───────────────────
deploy() {
    info "Building Docker images... (ini mungkin makan waktu beberapa menit)"
    docker compose -f "$PROJECT_DIR/docker-compose.yml" build --no-cache

    info "Menjalankan semua service..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" up -d

    success "Semua service berjalan!"
}

# ── Step 3: Register Telegram Webhook ────────────────────────
register_webhook() {
    info "Mendaftarkan Telegram Webhook..."

    # Ambil token dari .env
    BOT_TOKEN=$(grep "^TELEGRAM_BOT_TOKEN=" "$PROJECT_DIR/backend/.env" | cut -d'=' -f2 | tr -d '"' | tr -d "'")
    SECRET_TOKEN=$(grep "^TELEGRAM_SECRET_TOKEN=" "$PROJECT_DIR/backend/.env" | cut -d'=' -f2 | tr -d '"' | tr -d "'")

    if [[ -z "$BOT_TOKEN" || "$BOT_TOKEN" == "YOUR_TELEGRAM_BOT_TOKEN_HERE" ]]; then
        warn "TELEGRAM_BOT_TOKEN belum diset di backend/.env. Webhook tidak didaftarkan."
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
        info "Webhook URL: $WEBHOOK_URL"
    else
        warn "Webhook registration response: $RESPONSE"
        warn "Cek bot token dan domain Anda."
    fi
}

# ── Step 4: Verifikasi deployment ────────────────────────────
verify() {
    info "Menunggu service ready (10 detik)..."
    sleep 10

    echo ""
    echo "═══════════════════════════════════════════════"
    echo "  VERIFIKASI DEPLOYMENT"
    echo "═══════════════════════════════════════════════"

    # Health check backend
    if curl -sf "https://$DOMAIN/health" >/dev/null 2>&1; then
        success "✅ Backend API: https://$DOMAIN/health → OK"
    else
        warn "⚠️  Backend API belum merespons. Cek: docker compose logs backend"
    fi

    # Frontend check
    if curl -sf "https://$DOMAIN" >/dev/null 2>&1; then
        success "✅ Frontend: https://$DOMAIN → OK"
    else
        warn "⚠️  Frontend belum merespons. Cek: docker compose logs frontend"
    fi

    # Webhook info
    BOT_TOKEN=$(grep "^TELEGRAM_BOT_TOKEN=" "$PROJECT_DIR/backend/.env" | cut -d'=' -f2 | tr -d '"' | tr -d "'")
    if [[ -n "$BOT_TOKEN" && "$BOT_TOKEN" != "YOUR_TELEGRAM_BOT_TOKEN_HERE" ]]; then
        WEBHOOK_INFO=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/getWebhookInfo")
        WEBHOOK_SET_URL=$(echo "$WEBHOOK_INFO" | grep -o '"url":"[^"]*"' | head -1)
        success "✅ Telegram Webhook: $WEBHOOK_SET_URL"
    fi

    echo ""
    echo "═══════════════════════════════════════════════"
    echo -e "  ${GREEN}FinTrack berhasil di-deploy!${NC}"
    echo "  URL: https://$DOMAIN"
    echo "  Logs: docker compose logs -f"
    echo "═══════════════════════════════════════════════"
}

# ── Main flow ────────────────────────────────────────────────
main() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════╗${NC}"
    echo -e "${BLUE}║     FinTrack VPS Deploy Script    ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════╝${NC}"
    echo ""

    preflight
    configure_domain
    init_ssl
    deploy
    register_webhook
    verify
}

main "$@"
