# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Fixed
- `FinTrack-Fronted/src/app/dashboard/page.tsx` (Why: Pisahkan `telegramLoading` state dari shared `loading` state — tombol "Generate Kode Tautan Baru" ikut disabled saat operasi lain (tambah transaksi, simpan profil) sedang loading, menyebabkan tidak bisa diklik.)

### Added
- `FinTrack-Fronted/src/app/dashboard/page.tsx` (Why: Tambah tombol Scan Struk (ScanLine FAB) di atas tombol + untuk Home & Calendar tab. Tambah `ScanModal` yang menjelaskan cara scan struk via Telegram bot dan langsung bisa buka bot dari dashboard.)


### Added
- `backend/internal/telegram/handler.go` (Why: Added `handlePhoto` to fetch photo messages from Telegram, download image bytes, and upload to the Expense Tracker OCR/LLM API `/scan`).
- `docker-compose.yml` (Why: Configured `EXPENSE_TRACKER_API_URL` under the `bot-gateway` service environment to resolve the unified OCR service).

### Changed
- `backend/internal/telegram/handler.go` (Why: Increased webhook update context timeout from 15 seconds to 45 seconds to accommodate LLM & OCR processing times, updated `guideText` to include instructions for the photo scan OCR feature, and added a "Buka Dashboard" inline keyboard button in `mainMenuKeyboard` linking to the host URL).
- `backend/.env` (Why: Added `https://fintrack.home-sumbul.my.id` to `ALLOWED_ORIGINS` to allow CORS requests from the custom domain).
