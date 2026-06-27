# 🚀 Panduan Deployment Home Server Lokal

Panduan ini menjelaskan langkah-langkah untuk mendeploy seluruh stack aplikasi secara lokal di Home Server Anda, meliputi:
1. **FinTrack Stack** (Postgres multi-db, Go API, Bot Gateway, Home Server, Frontend).
2. **Expense Tracker Agent Stack** (Streamlit Dashboard, Python API, local Ollama integration).
3. **Ollama Dispatcher** (Watchdog, WOL, SSH Tunnel).

---

## 🛠️ Prasyarat (Prerequisites)

1. **Docker & Docker Compose** terinstal di Home Server.
2. **Node Ryzen 5600g** terhubung ke jaringan lokal yang sama, memiliki:
   - IP statis: `192.168.100.22` (atau sesuaikan).
   - MAC address untuk Wake-on-LAN: `9c:6b:00:07:66:cd`.
   - Layanan **Ollama** terinstal dan memiliki model `moondream:latest` dan `llama3.1:latest`.
3. **SSH Access**: Home Server dapat melakukan SSH tanpa password (menggunakan SSH Key) ke user `sumbul` di Ryzen node.

---

## 📋 Langkah-Langkah Konfigurasi & Deployment

### Langkah 1: Setup & Jalankan Ollama Dispatcher
Layanan dispatcher bertanggung jawab memonitor folder backup dan menjembatani koneksi SSH tunnel ke port Ollama Ryzen node.

1. Masuk ke folder Ollama:
   ```bash
   cd /home/sumbul/HomeServer/Ollama
   ```
2. Pastikan file `.env` di folder ini sudah memiliki parameter Telegram dan SSH yang sesuai.
3. Jalankan container dispatcher:
   ```bash
   docker compose up --build -d
   ```
   *Layanan ini akan otomatis berjalan di mode `host` network untuk mengontrol Wake-on-LAN dan membuka port local tunnel `11435` ke Ryzen node.*

---

### Langkah 2: Konfigurasi & Jalankan FinTrack Stack
Stack utama yang menyimpan data transaksi dan menyediakan dashboard web utama.

1. Masuk ke folder FinTrack:
   ```bash
   cd /home/sumbul/HomeServer/FinTrack
   ```
2. Salin `.env.example` ke `.env` di folder `backend/` dan `home-server/` jika belum, kemudian sesuaikan isinya.
3. Pada `.env` utama/backend, tambahkan variabel database ganda agar Postgres membuat database untuk proyek lain (opsional):
   ```env
   POSTGRES_MULTIPLE_DATABASES=fintrack,expense_tracker
   ```
4. Jalankan stack utama:
   ```bash
   ./deploy-tunnel.sh fintrack.home-sumbul.my.id
   ```
   *(Atau secara manual menggunakan: `docker compose up --build -d`)*

---

### Langkah 3: Konfigurasi & Jalankan Expense Tracker Agent
Layanan OCR struk belanja lokal yang terintegrasi dengan Ollama Ryzen node dan FinTrack backend.

1. Masuk ke folder Expense Tracker Agent:
   ```bash
   cd /home/sumbul/HomeServer/Expense_Tracker_Agent
   ```
2. Pastikan file `.env` Anda sudah terisi konfigurasi lokal Ollama dan Ryzen node:
   ```env
   # API internal FinTrack
   FINTRACK_API_URL="http://fintrack-api:8080"
   GATEWAY_API_KEY="api_key_kamu_di_fintrack"
   DEFAULT_FINTRACK_USER_ID="uuid_user_fintrack_kamu"

   # Local Ollama & Ryzen Node Configuration
   OLLAMA_API_URL="http://localhost:11435"
   OLLAMA_MODEL="llama3.1:latest"
   RYZEN_IP="192.168.100.22"
   RYZEN_MAC="9c:6b:00:07:66:cd"
   ```
3. Jalankan container Expense Tracker:
   ```bash
   docker compose up --build -d
   ```
   *Streamlit dashboard akan aktif di port `8501`, dan Python API (Flask) aktif di port `8000` di dalam jaringan internal `fintrack-network`.*

---

## 🔍 Verifikasi Status Deployment

Jalankan perintah berikut di Home Server untuk memastikan semua container aktif dengan benar:

```bash
# Cek stack FinTrack
docker compose -f /home/sumbul/HomeServer/FinTrack/docker-compose.yml ps

# Cek stack Expense Tracker
docker compose -f /home/sumbul/HomeServer/Expense_Tracker_Agent/docker-compose.yml ps

# Cek log dispatcher untuk memastikan tunnel aktif ke Ryzen
docker compose -f /home/sumbul/HomeServer/Ollama/docker-compose.yml logs -f
```

## 🛠️ Penyelesaian Masalah (Troubleshooting)

- **Gagal Terhubung ke Ollama**: Pastikan Ryzen node dalam keadaan menyala, atau periksa apakah magic packet WOL dikirim dengan benar. Jika Ryzen baru saja bangun, tunggu sekitar 30-45 detik hingga SSH tunnel terbentuk secara otomatis oleh dispatcher.
- **Database Postgres Kosong**: Jika database lain tidak terbuat, pastikan Anda menghapus volume pgdata lama terlebih dahulu (`docker compose down -v` lalu jalankan kembali) agar skrip inisialisasi di `docker-entrypoint-initdb.d` dieksekusi ulang.
