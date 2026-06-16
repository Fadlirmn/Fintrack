# ============================================================
# Home Server Dockerfile
# Multi-stage build — produces a minimal Alpine image (~10MB)
# ============================================================

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/home-server \
    ./cmd/main.go

# ── Runner stage ──────────────────────────────────────────
FROM alpine:3.19

RUN addgroup -S homeserver && adduser -S homeserver -G homeserver
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
# Copy scripts directory (whitelist of allowed scripts)
COPY --chown=homeserver:homeserver scripts/ ./scripts/
COPY --from=builder /app/home-server .

USER homeserver

EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8090/healthz || exit 1

CMD ["./home-server"]
