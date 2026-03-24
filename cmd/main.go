package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	gzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func main() {
	initDB()
	defer db.Close()

	initCache()

	go cleanupVisitors()

	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())

	// Trusted proxies — set to actual proxy IPs in production
	// TRUSTED_PROXIES=10.0.0.0/8 (Kubernetes pod CIDR)
	// Empty string disables proxy trust (safe default)
	trustedProxies := getenv("TRUSTED_PROXIES", "")
	if trustedProxies != "" {
		if err := r.SetTrustedProxies(strings.Split(trustedProxies, ",")); err != nil {
			log.Printf("WARNING: invalid trusted proxies: %v", err)
		}
	} else {
		if err := r.SetTrustedProxies(nil); err != nil {
			log.Printf("WARNING: SetTrustedProxies failed: %v", err)
		} // disables X-Forwarded-For trust
	}

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
	// NetworkPolicy blocks external access; this is an extra safety check
	r.POST("/admin/cache/invalidate", clusterOnlyMiddleware(), adminCacheInvalidateHandler)

	// Serve frontend static files if STATIC_DIR is set (local dev only)
	if staticDir := getenv("STATIC_DIR", ""); staticDir != "" {
		r.Static("/", staticDir)
	}

	addr := port()
	log.Printf("kli.st backend running on %s", addr)

	// Use http.Server with explicit timeouts to prevent Slowloris attacks
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
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
	// Set HSTS_ENABLED=true in Kubernetes deployment
	hstsEnabled := getenv("HSTS_ENABLED", "false") == "true"

	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		// X-XSS-Protection is legacy and can introduce vulnerabilities — omitted intentionally
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
	// CORS_ORIGIN supports comma-separated list of allowed origins:
	//   production:  CORS_ORIGIN=https://kli.st
	//   staging:     CORS_ORIGIN=https://test.kli.st
	//   local dev:   CORS_ORIGIN=* (default)
	rawOrigin := getenv("CORS_ORIGIN", "*")
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
		} else if rawOrigin == "*" {
			c.Header("Access-Control-Allow-Origin", "*")
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

// ── Request logger (no query params — privacy) ────────────────────────────────

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s %d %s",
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			time.Since(start),
		)
	}
}
