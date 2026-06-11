# Deployment & Personalised Dashboard FinTrack

**Tanggal:** 2026-06-11  
**Status:** selesai  
**Versi:** v5

## Konteks
Migrasi arsitektur split deployment FinTrack (Vercel & VPS via Cloudflare Tunnel), integrasi sistem personalisasi keuangan di dashboard, dan perbaikan kegagalan CORS preflight.

## Keputusan & Hasil
- Mengganti Certbot dan menghapus Nginx dari Docker Compose; Cloudflare Tunnel diarahkan ke backend Go via localhost.
- Keamanan VPS ditingkatkan dengan menutup port publik HTTP/HTTPS (80/443) pada firewall UFW.
- Personalisasi keuangan terintegrasi dengan Firestore database.
- Memperbaiki kegagalan build Docker dengan memperbaiki `.dockerignore`.
- Mengatasi kegagalan CORS preflight dengan otomatis memangkas trailing slash pada `ALLOWED_ORIGINS` di backend Go.
- Memperbaiki `.gitignore` backend agar tidak mengabaikan direktori source `cmd/api` dan `cmd/bot`.

## Tindak Lanjut
- [ ] Jalankan pull dan rebuild kontainer backend di VPS.

---
*Dibuat otomatis oleh agent · maks. 200 kata*
