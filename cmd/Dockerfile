# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Download dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kli-backend .

# ── Stage 2: Run ──────────────────────────────────────────────────────────────
FROM scratch

# Copy CA certificates for HTTPS calls (Ollama, Vault)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy /etc/passwd from builder so we can run as non-root
# UID 65532 = "nonroot" (standard for distroless/scratch images)
COPY --from=builder /etc/passwd /etc/passwd

# Copy binary
COPY --from=builder /app/kli-backend /kli-backend

EXPOSE 8080

# Run as non-root
USER 65532

ENTRYPOINT ["/kli-backend"]
