# Decision Log

## 1. Context Timeout Boost for Telegram Updates
- **Decision:** Boost context timeout from 15 seconds to 45 seconds inside `processUpdate` in the Go backend.
- **Rationale:** Processing OCR via Tesseract and running analysis with Llama 3 on Groq takes more time than a standard chat message handler (typically between 5 to 25 seconds). Increasing the timeout prevents client/network timeouts from interrupting ongoing transaction saves.

## 2. Bot Gateway OCR Delegation
- **Decision:** Delegate receipt image parsing directly to the Expense Tracker Agent Flask server instead of integrating OCR and LLM APIs directly in Go.
- **Rationale:** Reuses the existing Python OCR/LLM pipeline without rewriting the entire extraction, image processing, and prompting logic in Go, which accelerates development and reduces potential bugs.
