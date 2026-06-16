# ============================================================
# Backend Dockerfile — Multi-stage, Multi-binary
# Produces two targets:
#   api → fintrack REST API server (cmd/api)
#   bot → bot gateway / long-polling (cmd/bot)
# ============================================================

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Cache dependency layer
COPY go.mod go.sum ./
RUN go mod download

# Copy all source
COPY . .

# Build API binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/api \
    ./cmd/api/main.go

# Build Bot binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/bot \
    ./cmd/bot/main.go

# ── API Runner ────────────────────────────────────────────
FROM alpine:3.19 AS api

RUN addgroup -S fintrack && adduser -S fintrack -G fintrack
RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app
COPY --from=builder /app/api .
USER fintrack

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./api"]

# ── Bot Gateway Runner ─────────────────────────────────────
FROM alpine:3.19 AS bot

RUN addgroup -S fintrack && adduser -S fintrack -G fintrack
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/bot .
USER fintrack

# Bot gateway doesn't expose a port — it connects outbound only
CMD ["./bot"]
