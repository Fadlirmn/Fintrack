# FinTrack Sub‑System (Microservice) Plan

## Goal Description
Create a dedicated FinTrack microservice that runs independently from the Telegram chatbot. It will expose REST endpoints (protected by API‑key) for all core FinTrack functionality (transactions, reporting, user management). The chatbot gateway will call this service via HTTP when a user invokes FinTrack‑related commands.

## User Review Required
> [!IMPORTANT]
> Confirm the list of FinTrack features to expose and any versioning strategy for the API (e.g., `/api/v1`).

## Open Questions
> [!WARNING]
> 1. **Feature Set** – Should we include *all* FinTrack features now (transactions, reports, user mgmt) or start with a minimal set?
> 2. **Data Store Access** – Will the microservice use the existing PostgreSQL DB directly, or a separate read‑replica?
> 3 **Health Checks** – Preferred health‑check endpoint (`/healthz` standard JSON OK).

## Proposed Changes
---
### New Service

#### [NEW] `backend/internal/fintrack/`
- `service.go` – starts an HTTP server with routers for each feature.
- `handlers/` – individual files: `transaction.go`, `report.go`, `user.go`.
- `client/` – Go client library used by the chatbot gateway to call the service (wraps HTTP + API‑key).

#### [NEW] `backend/internal/config/fintrack.go`
- Config struct with `Port`, `APIKey`, `DatabaseURL`.

#### [NEW] `backend/internal/auth/apikey.go`
- Simple middleware that checks `X-API-Key` header against configured key.

#### [MODIFY] `backend/cmd/api/main.go`
- Replace current monolithic start‑up with launching the new FinTrack service as a separate goroutine **only if** `FINTRACK_MODE=service` env var is set; otherwise continue legacy mode for backward compatibility.

#### [MODIFY] `backend/cmd/bot/main.go`
- Import the FinTrack client package and register command handlers that forward `/fintrack` commands to the new microservice via HTTP.

#### Docker
- Add a new Dockerfile stage `fintrack-service` building the FinTrack binary.
- Update `docker-compose.yml` to add a service `fintrack` with its own container, network alias `fintrack`.

#### Dependencies
- Add `github.com/gorilla/mux` for routing.
- Add `github.com/joho/godotenv` for env loading (if not present).

---
### Testing
- Unit tests for each handler using httptest.
- End‑to‑end test: Bot issues a `/fintrack status` command → gateway → FinTrack service → returns JSON.
- Load test using `hey` to ensure latency < 100 ms for typical requests.

---
### Documentation
- Add a mermaid diagram showing the flow: Telegram → Bot Gateway → FinTrack Service → DB.
- Document API endpoints, request/response schemas, and required `X‑API‑Key` header.

## Verification Plan

### Automated Tests
- `go test ./backend/internal/fintrack/...` with coverage ≥ 85%.
- CI runs `golangci-lint` and `staticcheck`.

### Manual Verification
- Deploy via `docker-compose up fintrack gateway bot`.
- Use `curl -H "X-API-Key: <key>" http://localhost:8081/api/v1/transactions` to verify responses.
- Check logs for request tracing and metrics at `/metrics`.

---
**Next Steps**
- Await your confirmation on the open questions and any additional constraints.
- Then create/extend `task.md` with concrete implementation items.
