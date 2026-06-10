# Deployment FinTrack (Split Vercel & VPS via Cloudflare Tunnel)

**Tanggal:** 2026-06-11  
**Status:** dalam proses  
**Versi:** v2

## Konteks
Migrasi setup deployment VPS menggunakan Cloudflare Tunnel (`cloudflared`) dengan domain `server.home-sumbul.my.id`.

## Keputusan & Hasil
- Menghapus service `certbot` dari Docker Compose; SSL diurus oleh Cloudflare Edge.
- Menutup port publik Nginx (`80` & `443`) untuk meminimalkan attack surface pada VPS.
- Menambahkan service `cloudflared` ke dalam network Docker Compose internal.
- Membuat script deployment otomatis `deploy-tunnel.sh` yang langsung membuild backend, me-route traffic, dan mendaftarkan webhook.
- Memperbarui file `.env` dan Nginx virtual host sesuai domain `server.home-sumbul.my.id`.

## Tindak Lanjut
- [ ] Hubungkan connector tunnel Cloudflare Zero Trust dan salin `TUNNEL_TOKEN`.
- [ ] Buat file `.env` di root VPS dengan variabel `TUNNEL_TOKEN`.
- [ ] Jalankan `./deploy-tunnel.sh server.home-sumbul.my.id` di VPS.

---
*Dibuat otomatis oleh agent · maks. 200 kata*
