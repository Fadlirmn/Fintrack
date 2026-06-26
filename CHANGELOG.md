# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- `backend/internal/telegram/handler.go` (Why: Added `handlePhoto` to fetch photo messages from Telegram, download image bytes, and upload to the Expense Tracker OCR/LLM API `/scan`).
- `docker-compose.yml` (Why: Configured `EXPENSE_TRACKER_API_URL` under the `bot-gateway` service environment to resolve the unified OCR service).

### Changed
- `backend/internal/telegram/handler.go` (Why: Increased webhook update context timeout from 15 seconds to 45 seconds to accommodate LLM & OCR processing times, and updated `guideText` to include instructions for the photo scan OCR feature).
