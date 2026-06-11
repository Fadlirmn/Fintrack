# Decision Log — FinTrack

Dokumen ini mencatat keputusan teknis penting dan rasionalisasinya.

## Keputusan 1: Cloudflare Tunnel vs Port Forwarding Nginx (VPS)
*   **Keputusan:** Menggunakan Cloudflare Tunnel (`cloudflared`) dan menutup port masuk 80 & 443 pada firewall publik VPS Debian.
*   **Rasionalisasi:** Mencegah pemindaian port publik secara acak (random port scanning) pada VPS, mengeliminasi kebutuhan mengelola sertifikat SSL lokal dengan Certbot, dan mempercayakan SSL termination di edge Cloudflare.

## Keputusan 2: Database-backed Personalisation vs Local Storage
*   **Keputusan:** Memindahkan target tabungan (`wealth_goal`) dan nominal pendapatan bulanan (`monthly_income`) dari `localStorage` browser ke Firestore database per user.
*   **Rasionalisasi:** Data personalisasi keuangan harus bersifat persisten lintas perangkat. Jika pengguna masuk (login) dari browser atau perangkat lain, data target keuangan tetap terjaga karena terikat langsung ke document user di Firestore database yang diamankan lewat JWT HttpOnly cookie.

## Keputusan 3: Penghapusan Nginx Reverse Proxy
*   **Keputusan:** Menghapus container Nginx dari tumpukan Docker Compose, mengarahkan Cloudflare Tunnel client (`cloudflared`) secara langsung ke port `8080` kontainer Go backend.
*   **Rasionalisasi:** Setelah berpindah ke Cloudflare Tunnel, terminasi SSL dan manajemen traffic dilakukan secara langsung oleh Cloudflare Edge dan daemon `cloudflared`. Menjalankan Nginx di dalam VPS hanya untuk melakukan reverse proxy ke Go backend adalah redundansi infrastruktur yang menambah overhead memori dan kerumitan konfigurasi. CORS kini ditangani secara dinamis langsung oleh Go backend lewat `corsMiddleware`, sehingga peran Nginx sepenuhnya tidak lagi dibutuhkan.

## Keputusan 4: Pemangkasan Trailing Slash untuk ALLOWED_ORIGINS CORS
*   **Keputusan:** Menambahkan fungsi pemangkasan slash akhir (`strings.TrimSuffix(trimmed, "/")`) pada pembacaan daftar origin CORS di Go backend.
*   **Rasionalisasi:** Browser mengirimkan header `Origin` tanpa trailing slash. Jika user tidak sengaja mengonfigurasi origin dengan slash akhir (misalnya `https://fintrack-fronted.vercel.app/`), perbandingan string origin akan gagal, memblokir request CORS. Penanganan ini membuat pencocokan origin menjadi kebal terhadap kesalahan penulisan konfigurasi.

## Keputusan 5: Konfigurasi Cookie SameSite=None untuk Cross-Origin Session
*   **Keputusan:** Mengubah konfigurasi cookie session (`token`) pada backend menjadi `SameSite: http.SameSiteNoneMode` dan `Secure: true`.
*   **Rasionalisasi:** Karena frontend dihost di Vercel (`vercel.app`) dan backend dihost di VPS (`home-sumbul.my.id`), seluruh request API di browser bertindak sebagai cross-origin. Dengan SameSite=Lax (default), browser memblokir pengiriman cookie pada request fetch. Penambahan SameSite=None dan Secure=true memungkinkan cookie sesi terkirim dengan aman pada request cross-origin.
