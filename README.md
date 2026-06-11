# FinTrack — Backend & Deployment

Repositori ini berisi backend API (Go) dan modul bot Telegram untuk aplikasi pelacakan keuangan **FinTrack**, serta seluruh konfigurasi deployment VPS menggunakan Docker Compose & Cloudflare Tunnel.

## 🛠️ Tech Stack
- **Language**: Go (Golang)
- **Database**: Firebase Firestore
- **Framework**: Standard Go HTTP Server (Mux)
- **Deployment**: Docker Compose, Cloudflare Tunnel (SSL Termination)
- **Integration**: Telegram Bot Webhook API

---

## 📁 Struktur Folder
- `backend/` — Source code utama Go backend API & Telegram Bot
- `vps-setup.sh` — Script setup awal VPS (Docker & Firewall)
- `deploy-tunnel.sh` — Script deployment & registrasi Webhook satu-klik menggunakan Cloudflare Tunnel

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
./deploy-tunnel.sh fintrack.home-sumbul.my.id
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
- Cloudflare Tunnel melindungi VPS dengan menutup seluruh port masuk umum, menyisakan hanya port outbound aman untuk komunikasi dengan Cloudflare Edge.
