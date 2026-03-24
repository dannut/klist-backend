package main

import (
	_ "embed"
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var errNoResults = errors.New("no results found")

// ── Version ───────────────────────────────────────────────────────────────────

func versionHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":  getenv("APP_VERSION", "dev"),
		"status":   getenv("APP_STATUS", "Development"),
		"released": getenv("APP_RELEASED", ""),
		"upcoming": getenv("APP_UPCOMING", ""),
	})
}

// ── Health ────────────────────────────────────────────────────────────────────

type healthCache struct {
	mu      sync.Mutex
	status  bool
	checked time.Time
}

var health = &healthCache{}

func healthHandler(c *gin.Context) {
	health.mu.Lock()
	defer health.mu.Unlock()

	if time.Since(health.checked) > 5*time.Second {
		health.status = db.Ping() == nil
		health.checked = time.Now()
	}

	if !health.status {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "DOWN", "service": "kli.st", "error": "database unreachable",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "UP", "service": "kli.st"})
}

// ── Search handler ────────────────────────────────────────────────────────────

const (
	defaultPage    = 1
	defaultPerPage = 10
	maxPerPage     = 50
)

func searchHandler(c *gin.Context) {
	// Query param
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		q = strings.TrimSpace(c.Query("tool"))
	}
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing search term"})
		return
	}
	q = strings.TrimPrefix(q, "tool=")
	if len(q) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query too long"})
		return
	}

	// Pagination params
	page := parseIntParam(c.Query("page"), defaultPage)
	perPage := parseIntParam(c.Query("per_page"), defaultPerPage)

	if page < 1 {
		page = defaultPage
	}
	if perPage < 1 || perPage > maxPerPage {
		perPage = defaultPerPage
	}

	// Check cache first
	key := cacheKey(q, page, perPage)
	if cached := cacheGet(key); cached != nil {
		c.Header("X-Cache", "HIT")
		c.JSON(http.StatusOK, cached)
		return
	}

	results, err := search(c.Request.Context(), q, c.ClientIP(), page, perPage)
	if err != nil {
		if errors.Is(err, errNoResults) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no commands found matching your search criteria"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search error"})
		return
	}

	// Store in cache
	cacheSet(key, results)

	c.Header("X-Cache", "MISS")
	c.JSON(http.StatusOK, results)
}

// ── Search entrypoint ─────────────────────────────────────────────────────────

func search(ctx context.Context, q, ip string, page, perPage int) ([]Command, error) {
	// 1. Single word → direct SQL, no LLM needed
	if len(strings.Fields(q)) == 1 {
		return searchDB(ctx, q, page, perPage)
	}

	// Sanitize before sending to LLM
	sanitized := sanitizeForLLM(q)

	intent, err := interpretQuery(ctx, sanitized, ip)
	if err != nil {
		log.Printf("AI interpret failed, falling back to vector: %v", err)
		return performVectorSearch(ctx, q, page, perPage)
	}

	// 2. Tool only → all commands for that tool
	if intent.Tool != "" && intent.Keyword == "" {
		return searchDB(ctx, intent.Tool, page, perPage)
	}

	// 3. Tool + keyword → FTS (Full Text Search)
	if intent.Tool != "" && intent.Keyword != "" {
		results, err := searchByToolAndKeyword(ctx, intent.Tool, intent.Keyword, page, perPage)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		return searchDB(ctx, intent.Tool, page, perPage)
	}

	// 4. Keyword only OR No clear intent → Vector Search Fallback
	if intent.Keyword != "" {
		results, err := searchDB(ctx, intent.Keyword, page, perPage)
		if err == nil && len(results) > 0 {
			return results, nil
		}
	}

	return performVectorSearch(ctx, q, page, perPage)
}

// performVectorSearch handles the final AI-powered fallback with a safety threshold
func performVectorSearch(ctx context.Context, q string, page, perPage int) ([]Command, error) {
	res, err := searchVector(ctx, q, page, perPage)
	if err != nil {
		return nil, err
	}

	if len(res) == 0 || res[0].Score < 0.6 {
		return nil, errNoResults
	}

	return res, nil
}

// ── Admin: cache invalidation ─────────────────────────────────────────────────

func adminCacheInvalidateHandler(c *gin.Context) {
	cacheInvalidate()
	c.JSON(http.StatusOK, gin.H{"status": "cache invalidated"})
}

func clusterOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !strings.HasPrefix(ip, "10.") && ip != "127.0.0.1" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ── Install script ────────────────────────────────────────────────────────────

//go:embed install.sh
var installScript string

func installScriptHandler(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "inline; filename=\"install.sh\"")
	c.String(http.StatusOK, installScript)
}

// ── CLI releases ──────────────────────────────────────────────────────────────

func releasesHandler(c *gin.Context) {
	filename := c.Param("file")

	allowed := map[string]string{
		"kli-linux-amd64":  "application/octet-stream",
		"kli-linux-arm64":  "application/octet-stream",
		"kli-darwin-amd64": "application/octet-stream",
		"kli-darwin-arm64": "application/octet-stream",
		"SHA256SUMS":       "text/plain; charset=utf-8",
	}

	contentType, ok := allowed[filename]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	filepath := "/releases/" + filename
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.File(filepath)
}

func parseIntParam(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
