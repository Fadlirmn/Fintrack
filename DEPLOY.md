# 🚀 FinTrack — Deploy Guide

Panduan deploy FinTrack dengan arsitektur split:
- **Frontend (Next.js)** → **Vercel** (gratis, CDN global)
- **Backend (Go API + Telegram Bot)** → **VPS Debian** (via Docker)

## Arsitektur

```
Browser / Telegram
       │
       ├──→ Vercel (Frontend Next.js)
       │         │
       │         └──→ HTTPS Request ke VPS Backend
       │
       └──→ VPS (Nginx → Go API :8080)
                    └──→ Telegram Webhook
                    └──→ Firebase Firestore
```

---

## BAGIAN 1: Deploy Frontend ke Vercel

### 1a. Push frontend ke GitHub

```bash
cd /path/to/FinTrack
git init   # jika belum
git add frontend/ .gitignore
git commit -m "chore: initial commit"
git remote add origin https://github.com/USERNAME/fintrack.git
git push -u origin main
```

### 1b. Import ke Vercel

1. Buka [vercel.com](https://vercel.com) → **Add New Project**
2. Import repository GitHub kamu
3. **Root Directory**: set ke `frontend`
4. Framework: **Next.js** (auto-detect)

### 1c. Set Environment Variables di Vercel

Di Vercel Project → **Settings → Environment Variables**, tambahkan:

| Variable | Value |
|----------|-------|
| `NEXT_PUBLIC_API_URL` | `https://YOUR_API_DOMAIN.COM` |

> Jika pakai Firebase client-side, tambahkan juga `NEXT_PUBLIC_FIREBASE_*` sesuai `.env.example`.

### 1d. Deploy

Klik **Deploy**. Vercel akan otomatis build dan assign domain `*.vercel.app`.

**Catat URL Vercel** (contoh: `https://fintrack-xyz.vercel.app`) — dibutuhkan untuk konfigurasi CORS backend.

---

## BAGIAN 2: Deploy Backend ke VPS

### 2a. Setup VPS (satu kali)

```bash
# Upload dan jalankan setup script
scp vps-setup.sh user@YOUR_VPS_IP:/tmp/
ssh user@YOUR_VPS_IP "sudo bash /tmp/vps-setup.sh"

# Re-login agar docker group efektif
exit && ssh user@YOUR_VPS_IP
```

### 2b. Clone Project

```bash
cd /opt/fintrack
git clone https://github.com/USERNAME/fintrack.git .
```

### 2c. Upload Firebase Credentials

```bash
mkdir -p backend/configs
scp path/to/firebase-credentials.json user@YOUR_VPS_IP:/opt/fintrack/backend/configs/
```

### 2d. Isi File Environment Backend

```bash
cp backend/.env.example backend/.env
nano backend/.env
```

Isi `backend/.env` (ganti semua `YOUR_*`):
```env
PORT=8080
ENV=production

# Security — minimal 32 karakter acak
JWT_SECRET=GANTI_DENGAN_STRING_ACAK_PANJANG_DI_SINI

# Telegram Bot
TELEGRAM_BOT_TOKEN=YOUR_TELEGRAM_BOT_TOKEN_HERE
TELEGRAM_WEBHOOK_URL=https://api.yourdomain.com/api/v1/telegram/webhook
TELEGRAM_SECRET_TOKEN=BUAT_STRING_RAHASIA_ACAK_UNTUK_WEBHOOK

# Firebase
FIREBASE_PROJECT_ID=your-firebase-project-id
GOOGLE_APPLICATION_CREDENTIALS=/app/configs/firebase-credentials.json

# CORS — URL frontend Vercel (dan localhost untuk dev)
# Wildcard *.vercel.app mengizinkan semua preview URLs
ALLOWED_ORIGINS=https://fintrack-xyz.vercel.app,*.vercel.app,http://localhost:3000
```

### 2e. Deploy Backend

```bash
cd /opt/fintrack
./deploy.sh api.yourdomain.com your@email.com
```

---

## BAGIAN 3: Konfigurasi CORS Nginx

Edit `nginx/conf.d/fintrack.conf` dan ganti placeholder:

```bash
# Ganti YOUR_DOMAIN.COM dengan domain API backend
sed -i 's/YOUR_DOMAIN\.COM/api.yourdomain.com/g' nginx/conf.d/fintrack.conf

# Ganti YOUR_VERCEL_APP dengan nama app Vercel kamu
sed -i 's/YOUR_VERCEL_APP/fintrack-xyz/g' nginx/conf.d/fintrack.conf

# Restart nginx
docker compose restart nginx
```

---

## Perintah Berguna (di VPS)

```bash
# Status semua service
docker compose ps

# Log real-time
docker compose logs -f backend

# Restart setelah update kode
git pull && docker compose up -d --build backend

# Verifikasi Telegram Webhook
curl "https://api.telegram.org/botYOUR_TOKEN/getWebhookInfo"

# Health check backend
curl https://api.yourdomain.com/health
```

---

## Troubleshooting

| Masalah | Cek |
|---------|-----|
| Frontend tidak bisa fetch API | Pastikan `NEXT_PUBLIC_API_URL` di Vercel sudah diset, cek CORS di backend `.env` (`ALLOWED_ORIGINS`) |
| CORS error di browser | Tambahkan URL Vercel ke `ALLOWED_ORIGINS` di `backend/.env`, restart backend |
| Telegram bot tidak balas | `docker compose logs backend`, pastikan webhook terdaftar |
| SSL gagal | Pastikan DNS sudah propagate ke IP VPS |
| Firebase error | Cek `backend/configs/firebase-credentials.json` ada dan valid |

---

## Struktur File

```
FinTrack/
├── docker-compose.yml         ← Backend + Nginx + Certbot (NO frontend)
├── deploy.sh                  ← Deploy backend otomatis
├── vps-setup.sh               ← Setup VPS Debian (satu kali)
├── backend/
│   ├── Dockerfile
│   ├── go.mod + go.sum
│   ├── .env.example           ← Template — isi ALLOWED_ORIGINS dengan Vercel URL
│   └── configs/
│       └── firebase-credentials.json  ← Upload manual, JANGAN commit!
├── frontend/
│   ├── vercel.json            ← Vercel config (region Singapore, security headers)
│   ├── next.config.js         ← Tanpa output:standalone (Vercel tidak butuh)
│   └── .env.example           ← Template — isi NEXT_PUBLIC_API_URL dengan URL VPS
└── nginx/
    └── conf.d/fintrack.conf   ← Hanya serve /api/ dan /health (frontend di Vercel)
```
