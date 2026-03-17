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
# Folosim Alpine în loc de scratch pentru a avea /bin/sh necesar Vault-ului
FROM alpine:3.19

# Instalăm certificatele CA pentru apeluri HTTPS (Ollama, Vault)
RUN apk --no-cache add ca-certificates

# Creăm userul non-root (65532) pentru a păstra securitatea imaginii
RUN addgroup -S -g 65532 nonroot && adduser -S -u 65532 -G nonroot nonroot

# Copiem binarul din builder
COPY --from=builder /app/kli-backend /kli-backend

# Expunem portul aplicației
EXPOSE 8080

# Rulăm ca non-root (userul creat mai sus)
USER 65532

# Păstrăm ENTRYPOINT-ul, dar acum Kubernetes va putea rula /bin/sh înainte
ENTRYPOINT ["/kli-backend"]
