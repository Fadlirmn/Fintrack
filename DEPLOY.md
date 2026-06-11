# 🚀 FinTrack — Deploy Guide

Arsitektur deployment dengan **2 repo terpisah**:

| Repo | Lokasi lokal | Deploy ke |
|------|-------------|-----------|
| `fintrack-backend` | `~/Dokumen/FinTrack/` | **VPS** (Docker + Cloudflare Tunnel) |
| `fintrack-frontend` | `~/Dokumen/FinTrack Fronted/` | **Vercel** |

```
Browser ──→ Vercel (fintrack-frontend)
                │  fetch API
                ▼
      Cloudflare Tunnel (Edge)
                │
                ▼
          VPS cloudflared ──→ Go Backend :8080
                                    │
                              Telegram Webhook
                              Firebase Firestore
```

---

## ✅ Status Repo Saat Ini

```
~/Dokumen/FinTrack/           ← backend repo (git init ✅, commit ✅)
├── backend/                  ← Go REST API & Telegram Bot
│   ├── cmd/
│   │   ├── api/
│   │   │   └── main.go       ← REST API server entrypoint
│   │   └── bot/
│   │       └── main.go       ← Poller bot entrypoint (opsional)
│   ├── config/
│   │   └── config.go         ← Loader konfigurasi Go
│   ├── configs/
│   │   └── firebase-credentials.json ← Credentials Firebase (gitignored 🔒)
│   ├── internal/
│   │   ├── auth/
│   │   │   ├── handler.go    ← Register, Login, Me, UpdateProfile
│   │   │   ├── jwt.go        ← Token generation/validation
│   │   │   └── middleware.go ← Auth middleware
│   │   ├── db/
│   │   │   └── firestore.go  ← Firestore client initializer
│   │   ├── telegram/
│   │   │   ├── handler.go    ← Webhook message handler & linking
│   │   │   ├── parser.go     ← Parser pesan pengeluaran
│   │   │   └── poller.go     ← Long polling implementation
│   │   └── transaction/
│   │       └── handler.go    ← CRUD transaksi & kategori, dashboard summary
│   ├── .dockerignore
│   ├── .env                  ← Konfigurasi env lokal (gitignored 🔒)
│   ├── .env.example
│   ├── .gitignore
│   ├── Dockerfile            ← Multi-stage build Go backend
│   ├── go.mod
│   ├── go.sum
│   └── railway.toml
├── discussions/              ← Dokumentasi ringkasan diskusi
│   └── 2026-06-11_deploy-fintrack-split/
│       ├── changelog.md
│       └── summary.md
├── CHANGELOG.md              ← Riwayat versi rilis proyek
├── DECISION_LOG.md           ← Riwayat keputusan teknis arsitektur
├── DEPLOY.md                 ← Panduan deployment (file ini)
├── README.md                 ← Deskripsi umum proyek
├── deploy-tunnel.sh          ← Script deploy VPS (Cloudflare Tunnel)
├── docker-compose.yml        ← Konfigurasi container backend & cloudflared
└── vps-setup.sh              ← Script setup awal VPS (Docker & Firewall)

~/Dokumen/FinTrack Fronted/   ← frontend repo (git init ✅, commit ✅)
├── src/                      ← Next.js source code
│   ├── app/                  ← Page routing & view
│   ├── components/           ← Reusable UI components
│   └── services/             ← Client API integration (api.ts)
├── next.config.js
├── vercel.json
├── package.json
└── .env.example
```

---

## BAGIAN 1 — Push ke GitHub

### Backend repo

```bash
# 1. Buat repo baru di GitHub: "fintrack-backend" (private)
# 2. Sambungkan dan push:
cd ~/Dokumen/FinTrack
git remote add origin https://github.com/USERNAME/fintrack-backend.git
git push -u origin main
```

### Frontend repo

```bash
# 1. Buat repo baru di GitHub: "fintrack-frontend" (private atau public)
# 2. Sambungkan dan push:
cd ~/Dokumen/FinTrack\ Fronted
git remote add origin https://github.com/USERNAME/fintrack-frontend.git
git push -u origin main
```

---

## BAGIAN 2 — Deploy Frontend ke Vercel

### 2a. Import di Vercel

1. Buka [vercel.com/new](https://vercel.com/new)
2. Import repo **fintrack-frontend** dari GitHub
3. Setting:
   - **Root Directory**: `.` (sudah benar, repo ini = frontend)
   - **Framework**: Next.js (auto-detect)
4. **Environment Variables** — tambahkan:

   | Name | Value |
   |------|-------|
   | `NEXT_PUBLIC_API_URL` | `https://api.DOMAIN_KAMU.com` |

5. Klik **Deploy**

### 2b. Catat URL yang didapat

Setelah deploy selesai, Vercel beri URL seperti:
```
https://fintrack-abc123.vercel.app
```
**Simpan URL ini** — dibutuhkan untuk konfigurasi CORS di backend.

---

## BAGIAN 3 — Deploy Backend ke VPS

### 3a. Setup VPS (satu kali, jalankan sebagai root)

```bash
# Upload script dari lokal:
scp ~/Dokumen/FinTrack/vps-setup.sh user@IP_VPS:/tmp/

# Jalankan di VPS:
ssh user@IP_VPS "sudo bash /tmp/vps-setup.sh"

# Re-login setelah setup:
exit && ssh user@IP_VPS
```

Script menginstall: Docker, Docker Compose, UFW firewall (hanya buka port 22 untuk SSH outbound).

### 3b. Clone backend repo ke VPS

```bash
ssh user@IP_VPS
mkdir -p /opt/fintrack && cd /opt/fintrack
git clone https://github.com/USERNAME/fintrack-backend.git .
```

### 3c. Upload Firebase credentials

```bash
# Dari lokal ke VPS (firebase-credentials.json dari Firebase Console):
scp /path/ke/firebase-credentials.json \
    user@IP_VPS:/opt/fintrack/backend/configs/firebase-credentials.json
```

### 3d. Isi .env backend di VPS

```bash
ssh user@IP_VPS
cd /opt/fintrack
cp backend/.env.example backend/.env
nano backend/.env
```

Isi semua value (ganti yang `YOUR_*`):

```env
PORT=8080
ENV=production

# Generate: openssl rand -base64 32
JWT_SECRET=isi_dengan_string_acak_32_karakter_atau_lebih

# Dari BotFather Telegram
TELEGRAM_BOT_TOKEN=1234567890:AAHxxxxxxxxxxxxxxxx
TELEGRAM_WEBHOOK_URL=https://api.DOMAIN_KAMU.com/api/v1/telegram/webhook

# Generate: openssl rand -hex 16
TELEGRAM_SECRET_TOKEN=string_rahasia_acak_untuk_webhook

FIREBASE_PROJECT_ID=nama-project-firebase-kamu
GOOGLE_APPLICATION_CREDENTIALS=/app/configs/firebase-credentials.json

# URL frontend Vercel (dari Bagian 2b)
ALLOWED_ORIGINS=https://fintrack-abc123.vercel.app,*.vercel.app,http://localhost:3000
```

### 3e. Jalankan deploy

```bash
cd /opt/fintrack
./deploy-tunnel.sh server.home-sumbul.my.id
```

Script otomatis: build Docker → jalankan container (backend + cloudflared) → register Telegram Webhook.

### 3f. Verifikasi backend berjalan

```bash
# Cek logs container backend
docker compose logs backend

# Cek Telegram webhook
curl "https://api.telegram.org/bot<TOKEN>/getWebhookInfo"
# Harus ada: "url": "https://server.home-sumbul.my.id/api/v1/telegram/webhook"
```

---

## BAGIAN 4 — Verifikasi Akhir

1. **Buka frontend** di browser: `https://fintrack-abc123.vercel.app`
2. **Register akun** → harusnya berhasil (fetch ke VPS backend)
3. **Test bot Telegram** → kirim `/start` ke bot → harus balas
4. **Test catat pengeluaran** → kirim `Beli kopi 25000 #makanan` → bot konfirmasi

---

## Perintah Berguna (di VPS)

```bash
# Lihat status semua container
docker compose ps

# Log real-time backend
docker compose logs -f backend

# Update setelah push kode baru ke GitHub
git pull && docker compose up -d --build backend

# Stop semua
docker compose down
```

---

## Update Kode

### Update frontend (otomatis via Vercel)
```bash
# Di lokal:
cd ~/Dokumen/FinTrack\ Fronted
git add . && git commit -m "fix: ..." && git push
# Vercel auto-deploy setelah push ke main ✅
```

### Update backend
```bash
# Di lokal:
cd ~/Dokumen/FinTrack
git add . && git commit -m "fix: ..." && git push

# Di VPS:
ssh user@IP_VPS
cd /opt/fintrack
git pull
docker compose up -d --build backend
```

---

## Troubleshooting

| Masalah | Solusi |
|---------|--------|
| CORS error di browser | Tambahkan URL Vercel ke `ALLOWED_ORIGINS` di `backend/.env`, lalu `docker compose restart backend` |
| Vercel build gagal | Cek `NEXT_PUBLIC_API_URL` sudah diset di Vercel env vars |
| Bot tidak balas | `docker compose logs backend` → cek token + webhook terdaftar |
| SSL / Akses gagal | Pastikan status tunnel aktif dan konfigurasi Public Hostname di dasbor Cloudflare Zero Trust sudah benar |
| Firebase error | Cek `backend/configs/firebase-credentials.json` ada dan valid |
