package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// ── Rate limiter ──────────────────────────────────────────────────────────────
// Two-tier strategy:
//   1. Redis sliding window — consistent across all pods (preferred)
//   2. In-memory fallback  — used only when Redis is unavailable

const (
	rateWindow   = time.Second
	rateBurst    = 5 // must match rateRequests for consistent behaviour
	rateRequests = 5 // max requests per rateWindow per IP
)

// rateLimitScript atomically increments the counter and sets the TTL only
// on the first request. Using a Lua script ensures INCR + EXPIRE are
// executed as a single atomic operation, eliminating the race condition
// where the TTL could be missing if the server restarts between the two calls.
var rateLimitScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return current
`)

// ── Redis rate limit ──────────────────────────────────────────────────────────

func redisRateAllow(ip string) (bool, error) {
	if rdb == nil {
		return false, fmt.Errorf("redis unavailable")
	}

	key := fmt.Sprintf("kli:rate:%s", ip)
	ctx := context.Background()

	// Atomic INCR + EXPIRE via Lua script — eliminates the race condition
	// where a crash between INCR and EXPIRE would leave a key without TTL,
	// permanently blocking a user IP.
	val, err := rateLimitScript.Run(ctx, rdb, []string{key}, int(rateWindow.Seconds())).Int64()
	if err != nil {
		return false, err
	}

	return val <= int64(rateRequests), nil
}

// ── In-memory fallback ────────────────────────────────────────────────────────

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors   = make(map[string]*visitor)
	visitorsMu sync.Mutex
)

func getVisitor(ip string) *rate.Limiter {
	visitorsMu.Lock()
	defer visitorsMu.Unlock()
	v, exists := visitors[ip]
	if !exists {
		lim := rate.NewLimiter(rate.Every(rateWindow), rateBurst)
		visitors[ip] = &visitor{lim, time.Now()}
		return lim
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// cleanupVisitors removes stale in-memory rate limiters.
// Fix: collect stale IPs under a short lock, then delete in batches
// without holding the lock during iteration — prevents blocking all
// requests during cleanup when map is large (e.g. under DDoS attack).
func cleanupVisitors() {
	for {
		time.Sleep(5 * time.Minute)

		// Phase 1: collect stale IPs under lock (fast — just reads)
		visitorsMu.Lock()
		stale := make([]string, 0)
		for ip, v := range visitors {
			if time.Since(v.lastSeen) > 10*time.Minute {
				stale = append(stale, ip)
			}
		}
		visitorsMu.Unlock()

		// Phase 2: delete in small batches — lock released between batches
		// so incoming requests are never blocked during cleanup
		const batchSize = 100
		for i := 0; i < len(stale); i += batchSize {
			end := i + batchSize
			if end > len(stale) {
				end = len(stale)
			}
			visitorsMu.Lock()
			for _, ip := range stale[i:end] {
				if v, ok := visitors[ip]; ok && time.Since(v.lastSeen) > 10*time.Minute {
					delete(visitors, ip)
				}
			}
			visitorsMu.Unlock()

			if end < len(stale) {
				time.Sleep(time.Millisecond)
			}
		}

		if len(stale) > 0 {
			slog.Info("rate limit cleanup: removed stale visitors", "count", len(stale))
		}
	}
}

// ── Middleware ────────────────────────────────────────────────────────────────

func rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		// Try Redis first (consistent across pods)
		allowed, err := redisRateAllow(ip)
		if err != nil {
			// Redis unavailable — fall back to in-memory
			slog.Warn("rate limit: redis fallback to in-memory", "ip", ip, "err", err)
			if !getVisitor(ip).Allow() {
				tooManyRequests(c)
				return
			}
			c.Next()
			return
		}

		if !allowed {
			tooManyRequests(c)
			return
		}

		c.Next()
	}
}

func tooManyRequests(c *gin.Context) {
	c.Header("Retry-After", "1")
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error": "too many requests — please slow down",
	})
	c.Abort()
}
