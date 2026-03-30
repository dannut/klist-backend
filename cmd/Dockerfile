# ── Stage 1: Build backend ────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Download dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build backend
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kli-backend .

# ── Stage 2: Build CLI binaries ───────────────────────────────────────────────
FROM golang:1.23-alpine AS cli-builder

WORKDIR /cli

COPY cli/go.mod cli/go.sum ./
RUN go mod download

COPY cli/ .

# for supported platforms
RUN CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o releases/kli-linux-amd64 .
RUN CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o releases/kli-linux-arm64 .
RUN CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags="-s -w" -o releases/kli-darwin-amd64 .
RUN CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o releases/kli-darwin-arm64 .

# generate SHA256SUMS
RUN cd releases && sha256sum kli-linux-amd64 kli-linux-arm64 kli-darwin-amd64 kli-darwin-arm64 > SHA256SUMS

# ── Stage 3: Run ──────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

RUN addgroup -S -g 65532 nonroot && adduser -S -u 65532 -G nonroot nonroot

# copy backend binary
COPY --from=builder /app/kli-backend /kli-backend

# Copiem binarele CLI
COPY --from=cli-builder /cli/releases /releases

EXPOSE 8080

USER 65532

ENTRYPOINT ["/kli-backend"]