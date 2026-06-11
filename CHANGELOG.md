# Changelog — FinTrack

Semua perubahan penting pada proyek FinTrack dicatat di sini.

## [v1.2.0] - 2026-06-11

### Removed
- `[Removed]` Container Nginx dari tumpukan Docker Compose. Cloudflare Tunnel langsung berkomunikasi ke Go backend (`backend:8080`).
- `[Removed]` Skrip deployment tradisional `deploy.sh` yang menggunakan Certbot & Nginx.
- `[Removed]` Direktori konfigurasi `nginx/` beserta berkas `nginx.conf` dan virtual host `conf.d/fintrack.conf`.

### Changed
- `[Changed]` Konfigurasi firewall UFW di `vps-setup.sh` agar tidak membuka port masuk `80` dan `443` karena koneksi Cloudflare Tunnel bersifat outbound, menyisakan hanya port `22` (SSH) yang terbuka.
- `[Changed]` Memindahkan berkas kredensial Firebase `firebase-credentials.json` lokal ke `backend/configs/` agar sinkron dengan volume mount kontainer Docker.

## [v1.1.0] - 2026-06-11

### Added
- `[Added]` Handler `PUT /api/v1/auth/profile` di backend Go untuk memperbarui data target tabungan dan pendapatan bulanan di Firestore.
- `[Added]` Fungsi client-side `updateProfile` pada `services/api.ts` di frontend Next.js.
- `[Added]` Tombol "Simpan Pengaturan Finansial" di tab profil dashboard Next.js untuk menyimpan perubahan ke server database.

### Changed
- `[Changed]` Modifikasi default registrasi user baru untuk menyertakan `monthly_income` (default Rp 10.000.000) dan `wealth_goal` (default 30%).
- `[Changed]` Mengubah logic data flow target tabungan dan pendapatan di frontend Next.js agar menggunakan data terpersonalisasi yang dibaca langsung dari `api.me()` sebagai single source of truth, alih-alih `localStorage` murni.
- `[Changed]` Menghapus config Firebase client-side di `.env` dan `.env.example` frontend Next.js.
