# Deployment & Personalised Dashboard FinTrack

**Tanggal:** 2026-06-11  
**Status:** selesai  
**Versi:** v3

## Konteks
Migrasi arsitektur split deployment FinTrack (Vercel & VPS via Cloudflare Tunnel) dan integrasi sistem personalisasi keuangan di dashboard.

## Keputusan & Hasil
- Mengganti Certbot dengan Cloudflare Tunnel (`cloudflared`) untuk akses SSL aman internal tanpa membuka port publik VPS.
- Integrasi sistem login dashboard berbasis HttpOnly Cookie JWT.
- Personalisasi keuangan terintegrasi: data pendapatan bulanan (`monthly_income`) dan target tabungan (`wealth_goal`) kini disimpan permanen di database Firestore per user.
- Menambahkan route `PUT /api/v1/auth/profile` di backend Go dan interaksi form simpan di frontend Next.js.
- Clean up file `.env.example` frontend dari referensi Firebase SDK client.

## Tindak Lanjut
- [ ] Daftarkan webhook Telegram menggunakan domain `server.home-sumbul.my.id`.
- [ ] Deploy frontend di Vercel dengan setting env `NEXT_PUBLIC_API_URL`.
- [ ] Deploy backend di VPS dengan `TUNNEL_TOKEN` Zero Trust.

---
*Dibuat otomatis oleh agent · maks. 200 kata*
