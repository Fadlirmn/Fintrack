# Deployment & Personalised Dashboard FinTrack

**Tanggal:** 2026-06-11  
**Status:** selesai  
**Versi:** v4

## Konteks
Migrasi arsitektur split deployment FinTrack (Vercel & VPS via Cloudflare Tunnel) dan integrasi sistem personalisasi keuangan di dashboard.

## Keputusan & Hasil
- Mengganti Certbot dan menghapus Nginx dari Docker Compose; Cloudflare Tunnel langsung diarahkan ke backend Go (`127.0.0.1:8080`) via localhost.
- Keamanan VPS ditingkatkan dengan menutup port publik HTTP/HTTPS (80/443) pada firewall UFW.
- Personalisasi keuangan terintegrasi: target tabungan (`wealth_goal`) dan nominal pendapatan bulanan (`monthly_income`) disimpan permanen di Firestore database per user.
- Memperbaiki error build Docker (.dockerignore untuk `go.sum` dan `cmd/api` folder).
- Subdomain diubah dari `server` menjadi `fintrack.home-sumbul.my.id`.

## Tindak Lanjut
- [ ] Sesuaikan rute Public Hostname di tunnel lokal server ke `http://localhost:8080`.
- [ ] Jalankan `./deploy-tunnel.sh fintrack.home-sumbul.my.id` di server.
- [ ] Perbarui `NEXT_PUBLIC_API_URL` di Vercel ke domain baru dan redeploy.

---
*Dibuat otomatis oleh agent · maks. 200 kata*
