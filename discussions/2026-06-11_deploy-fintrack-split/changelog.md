# Changelog — Deployment FinTrack (Split Vercel & VPS)

| Tanggal | Versi | Perubahan |
|---------|-------|-----------|
| 2026-06-11 | v1 | Dibuat pertama kali |
| 2026-06-11 | v2 | Setup Cloudflare Tunnel integration (server.home-sumbul.my.id) |
| 2026-06-11 | v3 | Implementasi personalisasi keuangan (monthly_income, wealth_goal) ke database Firestore per user & form simpan profil di frontend Next.js |
| 2026-06-11 | v4 | Penghapusan Nginx, pengalihan tunnel langsung ke backend:8080, perubahan subdomain ke fintrack.home-sumbul.my.id, dan perbaikan .dockerignore |
| 2026-06-11 | v5 | Perbaikan bug preflight CORS dan perbaikan berkas .gitignore agar direktori cmd terindeks |
| 2026-06-11 | v6 | Konfigurasi cookie sesi SameSite=None untuk mendukung otentikasi cross-origin |
