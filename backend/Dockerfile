# ============================================================
# Stage 1: Builder — compile Go binary
# ============================================================
FROM golang:1.22-alpine AS builder

# Install git (needed for go mod download with private repos)
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy dependency manifests first (layer caching optimization)
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Build the API binary — statically linked, no CGO
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/api \
    ./cmd/api/main.go

# ============================================================
# Stage 2: Runner — minimal production image (~15MB)
# ============================================================
FROM alpine:3.19

# Security: run as non-root user
RUN addgroup -S fintrack && adduser -S fintrack -G fintrack

# Required for HTTPS and timezone
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy compiled binary from builder stage
COPY --from=builder /app/api .

# Copy Firebase credentials directory (mounted as volume in production)
RUN mkdir -p /app/configs

# Use non-root user
USER fintrack

EXPOSE 8080

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./api"]
