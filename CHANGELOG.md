# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- `backend/internal/telegram/handler.go` (Why: Added `handlePhoto` to fetch photo messages from Telegram, download image bytes, and upload to the Expense Tracker OCR/LLM API `/scan`).
- `docker-compose.yml` (Why: Configured `EXPENSE_TRACKER_API_URL` under the `bot-gateway` service environment to resolve the unified OCR service).

### Changed
- `backend/internal/telegram/handler.go` (Why: Increased webhook update context timeout from 15 seconds to 45 seconds to accommodate LLM & OCR processing times, updated `guideText` to include instructions for the photo scan OCR feature, and added a "Buka Dashboard" inline keyboard button in `mainMenuKeyboard` linking to the host URL).
- `backend/.env` (Why: Added `https://fintrack.home-sumbul.my.id` to `ALLOWED_ORIGINS` to allow CORS requests from the custom domain).
