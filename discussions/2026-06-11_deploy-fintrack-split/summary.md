# Deployment FinTrack (Split Vercel & VPS)

**Tanggal:** 2026-06-11  
**Status:** dalam proses  
**Versi:** v1

## Konteks
Pemisahan deployment FinTrack: Backend Go & Bot di VPS, sedangkan Frontend Next.js di Vercel demi efisiensi dan keamanan.

## Keputusan & Hasil
- Docker Compose di VPS disederhanakan (hanya backend Go, Nginx, Certbot).
- CORS di Go backend dikonfigurasi dinamis menggunakan env variable `ALLOWED_ORIGINS` untuk mendukung domain Vercel.
- Perbaikan kompatibilitas Firestore API (`iter.Stop()`, commit batch).
- Script automasi `deploy.sh` dan `vps-setup.sh` disiapkan di VPS.

## Tindak Lanjut
- [ ] Push repository backend & frontend ke GitHub masing-masing.
- [ ] Deploy frontend di Vercel dengan env `NEXT_PUBLIC_API_URL`.
- [ ] Jalankan `vps-setup.sh` dan `deploy.sh` di VPS untuk backend.
- [ ] Masukkan URL Vercel ke `ALLOWED_ORIGINS` backend di VPS.

---
*Dibuat otomatis oleh agent · maks. 200 kata*
