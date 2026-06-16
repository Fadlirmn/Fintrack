# 🚀 FinTrack — Deploy Guide

Arsitektur deployment dengan **2 repo** + **4 service container** + **stack services opsional**:

| Komponen | Lokasi | Deploy ke |
|----------|--------|-----------|
| `fintrack-frontend` | `~/Dokumen/FinTrack Fronted/` | **Vercel** (auto-deploy) |
| `fintrack-api` | `backend/cmd/api/` | **VPS** — container `fintrack-api` |
| `bot-gateway` | `backend/cmd/bot/` | **VPS** — container `bot-gateway` |
| `home-server` | `home-server/` | **VPS** — container `home-server` |
| `n8n` *(opsional)* | `services/` | **VPS** — container `n8n` |

---

## Arsitektur

```
Browser ──────────────────────→ Vercel (frontend)
                                      │ HTTPS fetch
                              Cloudflare Tunnel
                                      │
                            ┌─────────▼──────────┐
                            │  VPS (Docker)       │
                            │                     │
                            │  fintrack-api :8080 │──→ PostgreSQL
                            │  home-server  :8090 │
                            │  n8n          :5678 │ (opsional)
                            │                     │
         Telegram ──────────→  bot-gateway        │
                            │  (no port exposed)  │
                            └─────────────────────┘
                            Semua di jaringan: fintrack-network
```

---

## Struktur Repo

```
~/Dokumen/FinTrack/
├── backend/
│   ├── cmd/
│   │   ├── api/main.go         ← REST API server
│   │   └── bot/main.go         ← Bot gateway (Telegram long-poll)
│   ├── config/config.go
│   ├── internal/
│   │   ├── auth/               ← JWT auth, register, login
│   │   ├── gateway/
│   │   │   ├── fintrack/       ← HTTP client → fintrack-api
│   │   │   ├── home/           ← HTTP client → home-server
│   │   │   ├── n8n/            ← HTTP client → n8n
│   │   │   └── router.go       ← Orchestrator semua commands
│   │   ├── middleware/         ← API-key middleware
│   │   ├── telegram/           ← Bot handler + poller + parser
│   │   └── transaction/        ← CRUD transaksi + internal endpoints
│   ├── .env                    ← (gitignored 🔒)
│   ├── .env.example
│   └── Dockerfile              ← Multi-stage: target api + bot
├── home-server/
│   ├── cmd/main.go             ← Home server entrypoint
│   ├── internal/
│   │   ├── config/
│   │   ├── handlers/           ← status, resources, discovery, pc, scripts
│   │   └── middleware/         ← API-key + IP whitelist
│   ├── scripts/                ← Script yang boleh dieksekusi via bot
│   ├── .env                    ← (gitignored 🔒)
│   ├── .env.example
│   └── Dockerfile
├── services/
│   ├── docker-compose.yml      ← Stack terpisah: n8n, Portainer, Uptime Kuma
│   └── .env.example
├── docker-compose.yml          ← Stack utama: postgres, api, bot, home-server
├── deploy-tunnel.sh            ← Script deploy VPS
└── vps-setup.sh                ← Script setup awal VPS
```

---

## BAGIAN 1 — Persiapan Lokal

### 1a. Generate semua API key

```bash
cd ~/Dokumen/FinTrack

# GATEWAY_API_KEY  — untuk komunikasi bot-gateway ↔ fintrack-api
openssl rand -hex 32

# HOME_SERVER_API_KEY — untuk bot-gateway ↔ home-server
openssl rand -hex 32

# JWT_SECRET — untuk autentikasi user
openssl rand -hex 32

# POSTGRES_PASSWORD — password database
openssl rand -hex 16

# Simpan semua output di atas!
```

### 1b. Isi .env backend

```bash
cp backend/.env.example backend/.env
nano backend/.env
```

```env
# ── Server ──────────────────────────────────────────────────
PORT=8080
ENV=production

# ── Security ─────────────────────────────────────────────────
JWT_SECRET=<hasil openssl rand -hex 32>

# ── Telegram ─────────────────────────────────────────────────
TELEGRAM_BOT_TOKEN=<token dari @BotFather>
TELEGRAM_WEBHOOK_URL=https://api.DOMAIN_KAMU.com/api/v1/telegram/webhook
TELEGRAM_SECRET_TOKEN=<openssl rand -hex 16>

# ── Database ──────────────────────────────────────────────────
DATABASE_URL=postgres://fintrack:<POSTGRES_PASSWORD>@postgres:5432/fintrack?sslmode=disable
POSTGRES_PASSWORD=<hasil openssl rand -hex 16>

# ── CORS ──────────────────────────────────────────────────────
ALLOWED_ORIGINS=https://fintrack-abc123.vercel.app,http://localhost:3000

# ── Inter-service ─────────────────────────────────────────────
GATEWAY_API_KEY=<hasil openssl rand -hex 32>
FINTRACK_API_URL=http://fintrack-api:8080
HOME_SERVER_URL=http://home-server:8090
HOME_SERVER_API_KEY=<hasil openssl rand -hex 32>

# ── n8n (opsional) ────────────────────────────────────────────
N8N_URL=http://n8n:5678
N8N_API_KEY=<dibuat di UI n8n setelah deploy>
```

### 1c. Isi .env home-server

```bash
cp home-server/.env.example home-server/.env
nano home-server/.env
```

```env
PORT=8090
API_KEY=<HOME_SERVER_API_KEY yang sama dengan di backend/.env>
ALLOWED_IPS=172.16.0.0/12,127.0.0.1   # IP Docker internal
SCRIPTS_DIR=./scripts
```

### 1d. (Opsional) Isi .env services untuk n8n

```bash
cp services/.env.example services/.env
nano services/.env
```

```env
N8N_USER=admin
N8N_PASSWORD=<password_kuat>
N8N_WEBHOOK_URL=https://n8n.DOMAIN_KAMU.com
N8N_API_KEY=<dibuat di UI n8n setelah deploy>
```

---

## BAGIAN 2 — Deploy Frontend ke Vercel

1. Buka [vercel.com/new](https://vercel.com/new)
2. Import repo **fintrack-frontend** dari GitHub
3. Setting:
   - **Root Directory**: `.`
   - **Framework**: Next.js (auto-detect)
4. **Environment Variables**:

   | Name | Value |
   |------|-------|
   | `NEXT_PUBLIC_API_URL` | `https://api.DOMAIN_KAMU.com` |

5. Klik **Deploy** → catat URL yang didapat (contoh: `https://fintrack-abc123.vercel.app`)
6. Tambahkan URL ini ke `ALLOWED_ORIGINS` di `backend/.env`

---

## BAGIAN 3 — Setup VPS (satu kali)

### 3a. Upload dan jalankan setup script

```bash
# Upload dari lokal:
scp ~/Dokumen/FinTrack/vps-setup.sh user@IP_VPS:/tmp/

# Jalankan di VPS (install Docker, UFW, dll):
ssh user@IP_VPS "sudo bash /tmp/vps-setup.sh"

# Re-login:
exit && ssh user@IP_VPS
```

### 3b. Clone repo ke VPS

```bash
ssh user@IP_VPS
mkdir -p /opt/fintrack && cd /opt/fintrack
git clone https://github.com/Fadlirmn/Fintrack.git .
```

### 3c. Upload file .env ke VPS

```bash
# Dari lokal — upload semua .env yang sudah diisi
scp ~/Dokumen/FinTrack/backend/.env      user@IP_VPS:/opt/fintrack/backend/.env
scp ~/Dokumen/FinTrack/home-server/.env  user@IP_VPS:/opt/fintrack/home-server/.env

# Opsional: n8n
scp ~/Dokumen/FinTrack/services/.env     user@IP_VPS:/opt/fintrack/services/.env
```

---

## BAGIAN 4 — Deploy Stack Utama

### 4a. Jalankan deploy

```bash
ssh user@IP_VPS
cd /opt/fintrack
./deploy-tunnel.sh DOMAIN_KAMU.com
```

Script ini otomatis:
- Build Docker images (`api` + `bot` target dari Dockerfile multi-stage)
- Jalankan semua 4 container (`postgres`, `fintrack-api`, `bot-gateway`, `home-server`)
- Setup Cloudflare Tunnel
- Register Telegram webhook

### 4b. Verifikasi semua container jalan

```bash
docker compose ps

# Output yang diharapkan:
# fintrack-postgres     running
# fintrack-api          running (healthy)
# fintrack-bot-gateway  running
# fintrack-home-server  running (healthy)
```

### 4c. Cek logs masing-masing service

```bash
# API server
docker compose logs -f fintrack-api

# Bot gateway (harus tampil: "FinTrack API ✓", "Home Server ✓")
docker compose logs -f fintrack-bot-gateway

# Home server
docker compose logs -f fintrack-home-server
```

---

## BAGIAN 5 — Deploy n8n (opsional, stack terpisah)

### 5a. Jalankan services stack

```bash
ssh user@IP_VPS
cd /opt/fintrack

# Pastikan stack utama sudah jalan dulu (network harus sudah ada)
docker compose ps

# Baru jalankan n8n
docker compose -f services/docker-compose.yml up -d n8n
```

### 5b. Buat API key di n8n

1. Buka n8n UI di: `http://IP_VPS:5678` (atau via tunnel)
2. **Settings → API → Create API Key**
3. Salin key → tambahkan ke `backend/.env`:
   ```env
   N8N_API_KEY=<key yang baru dibuat>
   ```
4. Restart bot-gateway:
   ```bash
   docker compose restart fintrack-bot-gateway
   ```

### 5c. Buat webhook workflow di n8n

1. Buat workflow baru di n8n UI
2. Tambah node **Webhook** → set path (contoh: `backup`)
3. Aktivasi workflow
4. Test dari Telegram: `/n8n run backup`

---

## BAGIAN 6 — Verifikasi Akhir

```bash
# 1. Health check API
curl https://api.DOMAIN_KAMU.com/health
# → {"status":"ok"}

# 2. Test internal endpoint (pakai GATEWAY_API_KEY)
curl -H "X-API-Key: <GATEWAY_API_KEY>" \
     "https://api.DOMAIN_KAMU.com/internal/v1/balance?user_id=test"

# 3. Test home server (pakai HOME_SERVER_API_KEY)
curl -H "X-API-Key: <HOME_SERVER_API_KEY>" \
     http://localhost:8090/status

# 4. Test Telegram webhook terdaftar
curl "https://api.telegram.org/bot<TOKEN>/getWebhookInfo"
```

Di Telegram, test semua fitur:
- `/start` → menu muncul
- `/link <kode>` → hubungkan akun FinTrack
- `Beli kopi 25000 #makanan` → transaksi dicatat
- `/saldo` → lihat saldo
- `/server status` → info home server
- `/n8n list` → daftar workflow (jika n8n aktif)

---

## Update Kode

### Update frontend (otomatis via Vercel)
```bash
cd ~/Dokumen/FinTrack\ Fronted
git add . && git commit -m "fix: ..." && git push
# Vercel auto-deploy ✅
```

### Update backend / bot-gateway
```bash
# Di lokal: commit dan push
cd ~/Dokumen/FinTrack
git add . && git commit -m "fix: ..." && git push

# Di VPS: pull dan rebuild service yang berubah
ssh user@IP_VPS
cd /opt/fintrack
git pull

# Rebuild hanya yang diubah (lebih cepat)
docker compose up -d --build fintrack-api        # jika ubah API
docker compose up -d --build fintrack-bot-gateway # jika ubah bot
```

### Update home-server
```bash
# Di VPS setelah git pull:
docker compose up -d --build fintrack-home-server
```

### Tambah script baru untuk bot
```bash
# Di lokal: tambah file script di home-server/scripts/
nano home-server/scripts/nama-script
chmod +x home-server/scripts/nama-script
git add . && git commit -m "feat: add nama-script" && git push

# Di VPS:
git pull
# Mount scripts langsung dari host, tidak perlu rebuild
# (volume di docker-compose sudah map ./home-server/scripts → /app/scripts)
```

---

## Perintah Berguna (di VPS)

```bash
# ── Stack Utama ───────────────────────────────────────────
# Status semua container
docker compose ps

# Log real-time
docker compose logs -f fintrack-api
docker compose logs -f fintrack-bot-gateway
docker compose logs -f fintrack-home-server

# Restart satu service
docker compose restart fintrack-bot-gateway

# Rebuild dan restart satu service
docker compose up -d --build fintrack-api

# Stop semua
docker compose down

# ── Services Stack (n8n, dll) ─────────────────────────────
docker compose -f services/docker-compose.yml ps
docker compose -f services/docker-compose.yml logs -f n8n
docker compose -f services/docker-compose.yml restart n8n
docker compose -f services/docker-compose.yml down

# ── Optional: monitoring stack ────────────────────────────
docker compose -f services/docker-compose.yml --profile monitoring up -d
```

---

## Troubleshooting

| Masalah | Solusi |
|---------|--------|
| CORS error di browser | Tambah URL Vercel ke `ALLOWED_ORIGINS` → `docker compose restart fintrack-api` |
| Vercel build gagal | Cek `NEXT_PUBLIC_API_URL` di Vercel env vars |
| Bot tidak balas | `docker compose logs fintrack-bot-gateway` → cek token |
| Bot reply "❌ Gagal" | Cek `GATEWAY_API_KEY` sama di bot dan api, cek `fintrack-api` healthy |
| Home server tidak response | Cek `HOME_SERVER_API_KEY` sama di kedua `.env`, cek container running |
| n8n tidak bisa di-trigger | Cek workflow sudah **Active**, path webhook sesuai, `N8N_API_KEY` valid |
| SSL / akses gagal | Cek tunnel aktif: `docker compose logs cloudflared` |
| PostgreSQL error | `docker compose logs postgres` → cek `POSTGRES_PASSWORD` sesuai |
| Container restart loop | `docker compose logs <nama>` → lihat error startup |

---

## Environment Variables — Ringkasan

| Variable | Service | Keterangan |
|----------|---------|------------|
| `JWT_SECRET` | fintrack-api | Generate: `openssl rand -hex 32` |
| `TELEGRAM_BOT_TOKEN` | bot-gateway | Dari @BotFather |
| `POSTGRES_PASSWORD` | postgres, fintrack-api | Generate: `openssl rand -hex 16` |
| `GATEWAY_API_KEY` | fintrack-api + bot-gateway | **Harus sama** di keduanya |
| `HOME_SERVER_URL` | bot-gateway | `http://home-server:8090` |
| `HOME_SERVER_API_KEY` | bot-gateway + home-server | **Harus sama** di keduanya |
| `N8N_URL` | bot-gateway | `http://n8n:5678` (opsional) |
| `N8N_API_KEY` | bot-gateway | Dibuat di n8n UI (opsional) |
| `ALLOWED_ORIGINS` | fintrack-api | URL frontend Vercel + localhost |
