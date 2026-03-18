package main

import (
	_ "embed"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

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

	results, err := search(q, page, perPage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search error"})
		return
	}

	// Store in cache
	cacheSet(key, results)

	c.Header("X-Cache", "MISS")
	c.JSON(http.StatusOK, results)
}

// ── Search entrypoint ─────────────────────────────────────────────────────────

func search(q string, page, perPage int) ([]Command, error) {
	// Single word → direct SQL, no LLM needed
	if len(strings.Fields(q)) == 1 {
		return searchDB(q, page, perPage)
	}

	// Sanitize before sending to LLM
	sanitized := sanitizeForLLM(q)

	intent, err := interpretQuery(sanitized)
	if err != nil {
		log.Printf("llama3.2 failed, falling back to vector search: %v", err)
		return searchVector(q, page, perPage)
	}

	// Tool only → all commands for that tool
	if intent.Tool != "" && intent.Keyword == "" {
		return searchDB(intent.Tool, page, perPage)
	}

	// Tool + keyword → FTS
	if intent.Tool != "" && intent.Keyword != "" {
		results, err := searchByToolAndKeyword(intent.Tool, intent.Keyword, page, perPage)
		if err != nil {
			log.Printf("fts error: %v, falling back to tool", err)
			return searchDB(intent.Tool, page, perPage)
		}
		if len(results) > 0 {
			return results, nil
		}
		return searchDB(intent.Tool, page, perPage)
	}

	// Keyword only → try SQL first, fallback to vector
	if intent.Keyword != "" {
		results, err := searchDB(intent.Keyword, page, perPage)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		return searchVector(q, page, perPage)
	}

	// No clear intent → vector search
	return searchVector(q, page, perPage)
}

// ── Admin: cache invalidation ─────────────────────────────────────────────────
// Internal only — call via kubectl exec, not exposed through Cloudflare

func adminCacheInvalidateHandler(c *gin.Context) {
	cacheInvalidate()
	c.JSON(http.StatusOK, gin.H{"status": "cache invalidated"})
}

// clusterOnlyMiddleware blocks requests that don't come from pod CIDR (10.x.x.x)
func clusterOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !strings.HasPrefix(ip, "10.") && !strings.HasPrefix(ip, "127.") {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ── Install script ────────────────────────────────────────────────────────────
// Serves the kli CLI install script. Proxied by nginx from /install.sh.

//go:embed install.sh
var installScript string

func installScriptHandler(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "inline; filename=\"install.sh\"")
	c.String(http.StatusOK, installScript)
}

// ── CLI releases ──────────────────────────────────────────────────────────────
// Serves pre-compiled CLI binaries and SHA256SUMS from /releases directory.
// Built at Docker image build time — see Dockerfile cli-builder stage.

func releasesHandler(c *gin.Context) {
	filename := c.Param("file")

	// Whitelist allowed files to prevent path traversal
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
