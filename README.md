# FinTrack — Backend & Deployment

Repositori ini berisi backend API (Go) dan modul bot Telegram untuk aplikasi pelacakan keuangan **FinTrack**, serta seluruh konfigurasi deployment VPS menggunakan Docker Compose & Nginx.

## 🛠️ Tech Stack
- **Language**: Go (Golang)
- **Database**: Firebase Firestore
- **Framework**: Standard Go HTTP Server (Mux)
- **Deployment**: Docker Compose, Nginx (Reverse Proxy), Certbot (SSL)
- **Integration**: Telegram Bot Webhook API

---

## 📁 Struktur Folder
- `backend/` — Source code utama Go backend API & Telegram Bot
- `nginx/` — Konfigurasi server Nginx (SSL, Reverse Proxy, CORS whitelist)
- `vps-setup.sh` — Script setup awal VPS (Docker & Firewall)
- `deploy.sh` — Script deployment & registrasi SSL/Webhook satu-klik

---

## 🚀 Panduan Deployment VPS
Untuk panduan detail mengenai cara deploy backend dan setup server VPS, baca:
👉 **[DEPLOY.md](DEPLOY.md)**

### Quick Command (di VPS):
```bash
# 1. Setup server (satu kali)
sudo bash vps-setup.sh

# 2. Upload credentials & isi .env di folder backend/

# 3. Jalankan deploy script
./deploy.sh api.domain.com email@kamu.com
```

---

## 💻 Pengembangan Lokal (Development)

1. Pastikan Anda memiliki Go terinstall (v1.21+)
2. Buat file `.env` di folder `backend/` (lihat `.env.example`)
3. Taruh file `firebase-credentials.json` ke folder `backend/configs/`
4. Jalankan aplikasi:
   ```bash
   cd backend
   go run cmd/api/main.go
   ```

---

## 🔒 Security
- Seluruh kredensial sensitif Firebase (`firebase-credentials.json`) dan `.env` tidak di-commit ke Git.
- Endpoint webhook Telegram diamankan dengan verifikasi token rahasia `X-Telegram-Bot-Api-Secret-Token`.
- Nginx mengimplementasikan *rate limiting* untuk mencegah serangan brute-force.
