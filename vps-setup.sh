#!/usr/bin/env bash
# ============================================================
#  FinTrack VPS Setup Script — Debian/Ubuntu
#  Jalankan ini SEKALI di VPS baru sebagai root/sudo
#
#  Usage: sudo bash vps-setup.sh
# ============================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ── Check root ───────────────────────────────────────────────
[[ "$EUID" -eq 0 ]] || error "Jalankan sebagai root: sudo bash vps-setup.sh"

echo ""
echo "╔══════════════════════════════════════╗"
echo "║   FinTrack VPS Setup (Debian/Ubuntu) ║"
echo "╚══════════════════════════════════════╝"
echo ""

# ── 1. Update system ─────────────────────────────────────────
info "Update system packages..."
apt-get update -qq
apt-get upgrade -y -qq
apt-get install -y -qq \
    curl \
    wget \
    git \
    ufw \
    ca-certificates \
    gnupg \
    lsb-release \
    apt-transport-https \
    software-properties-common
success "System packages updated."

# ── 2. Install Docker ────────────────────────────────────────
if ! command -v docker >/dev/null 2>&1; then
    info "Installing Docker..."

    # Official Docker GPG key
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/debian/gpg | \
        gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg

    # Docker repository
    echo \
        "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
        https://download.docker.com/linux/debian \
        $(lsb_release -cs) stable" | \
        tee /etc/apt/sources.list.d/docker.list > /dev/null

    apt-get update -qq
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    # Enable Docker service
    systemctl enable docker
    systemctl start docker

    success "Docker installed: $(docker --version)"
else
    success "Docker already installed: $(docker --version)"
fi

# ── 3. Install Docker Compose standalone (jika perlu) ────────
if ! docker compose version >/dev/null 2>&1; then
    info "Installing Docker Compose plugin..."
    apt-get install -y docker-compose-plugin
fi
success "Docker Compose: $(docker compose version)"

# ── 4. Configure UFW Firewall ────────────────────────────────
info "Configuring UFW firewall..."
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh
ufw --force enable
success "Firewall configured: Only SSH (22) allowed (Cloudflare Tunnel operates outbound)."

# ── 5. Create project directory ──────────────────────────────
info "Creating project directory /opt/fintrack..."
mkdir -p /opt/fintrack
chown -R "$SUDO_USER:$SUDO_USER" /opt/fintrack 2>/dev/null || true
success "Project directory ready: /opt/fintrack"

# ── 6. Add current user to docker group ──────────────────────
if [[ -n "${SUDO_USER:-}" ]]; then
    usermod -aG docker "$SUDO_USER"
    success "User $SUDO_USER added to docker group. Re-login untuk efek."
fi

echo ""
echo "══════════════════════════════════════════"
echo -e "  ${GREEN}VPS Setup Selesai!${NC}"
echo "══════════════════════════════════════════"
echo ""
echo "Langkah selanjutnya:"
echo "  1. Re-login ke VPS (agar docker group efektif)"
echo "  2. Clone project ke /opt/fintrack:"
echo "       cd /opt/fintrack"
echo "       git clone <your-repo-url> ."
echo "  3. Copy file credentials:"
echo "       cp /path/to/firebase-credentials.json backend/configs/"
echo "  4. Copy dan isi .env file:"
echo "       cp backend/.env.example backend/.env"
# (removed frontend instructions since frontend is deployed on Vercel now)
echo "       nano backend/.env"
echo "  5. Jalankan deploy (Cloudflare Tunnel):"
echo "       ./deploy-tunnel.sh your-domain.com"
echo ""
