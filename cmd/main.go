package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func main() {
	// JSON structured logging — replaces log.Printf across the app
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	initDB()
	defer db.Close()

	initCache()

	go cleanupVisitors()

	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())

	// Trusted proxies — set to actual proxy IPs in production
	// TRUSTED_PROXIES=10.0.0.0/8 (Kubernetes pod CIDR)
	trustedProxies := getenv("TRUSTED_PROXIES", "")
	if trustedProxies != "" {
		if err := r.SetTrustedProxies(strings.Split(trustedProxies, ",")); err != nil {
			slog.Warn("invalid trusted proxies", "err", err)
		}
	} else {
		if err := r.SetTrustedProxies(nil); err != nil {
			slog.Warn("SetTrustedProxies failed", "err", err)
		}
	}

	r.Use(requestIDMiddleware())
	r.Use(requestLogger())
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	r.Use(securityHeaders())
	r.Use(corsMiddleware())

	r.GET("/api/health", healthHandler)
	r.GET("/api/version", versionHandler)
	r.GET("/api/search", rateLimitMiddleware(), searchHandler)
	r.GET("/install.sh", installScriptHandler)
	r.GET("/releases/:file", releasesHandler)

	// Internal cache invalidation — accessible only from within the cluster
	// NetworkPolicy blocks external access; clusterOnlyMiddleware is an extra safety check
	r.POST("/admin/cache/invalidate", clusterOnlyMiddleware(), adminCacheInvalidateHandler)

	// Serve frontend static files if STATIC_DIR is set (local dev only)
	if staticDir := getenv("STATIC_DIR", ""); staticDir != "" {
		r.Static("/", staticDir)
	}

	addr := port()
	slog.Info("kli.st backend started", "addr", addr)

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB
	}

	// Graceful shutdown — drain in-flight requests on SIGTERM/SIGINT
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "err", err)
	}
	slog.Info("server stopped")
}

// ── Port ──────────────────────────────────────────────────────────────────────

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

// ── Security headers ──────────────────────────────────────────────────────────

func securityHeaders() gin.HandlerFunc {
	// HSTS only in production (HTTPS via Cloudflare)
	hstsEnabled := getenv("HSTS_ENABLED", "false") == "true"

	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
		// API-only server: deny all resource loading
		c.Header("Content-Security-Policy", "default-src 'none'")
		if hstsEnabled {
			c.Header("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		c.Next()
	}
}

// ── CORS ──────────────────────────────────────────────────────────────────────

func corsMiddleware() gin.HandlerFunc {
	// CORS_ORIGIN must be set explicitly — no wildcard default in production.
	//   production:  CORS_ORIGIN=https://kli.st
	//   staging:     CORS_ORIGIN=https://test.kli.st
	//   local dev:   CORS_ORIGIN=* (must be set explicitly)
	rawOrigin := getenv("CORS_ORIGIN", "")
	if rawOrigin == "" {
		slog.Warn("CORS_ORIGIN not configured — cross-origin requests will be rejected")
	}
	allowedList := strings.Split(rawOrigin, ",")

	isAllowed := func(origin string) bool {
		if len(allowedList) == 1 && allowedList[0] == "*" {
			return true
		}
		for _, o := range allowedList {
			if strings.TrimSpace(o) == origin {
				return true
			}
		}
		return false
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && isAllowed(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// ── Request ID — propagate or generate X-Request-ID for distributed tracing ──

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		c.Next()
	}
}

// ── Request logger (structured, no query params — privacy) ───────────────────

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", c.GetString("request_id"),
		)
	}
}
