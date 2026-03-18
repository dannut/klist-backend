package main

import (
	"context"
	"fmt"
	"log"
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
	rateBurst    = 10
	rateRequests = 5 // max requests per rateWindow per IP
)

// ── Redis rate limit ──────────────────────────────────────────────────────────

func redisRateAllow(ip string) (bool, error) {
	if rdb == nil {
		return false, fmt.Errorf("redis unavailable")
	}

	key := fmt.Sprintf("kli:rate:%s", ip)
	ctx := context.Background()

	// Sliding window using INCR + EXPIRE
	var pipe redis.Pipeliner = rdb.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, rateWindow)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	return incr.Val() <= int64(rateRequests), nil
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

func cleanupVisitors() {
	for {
		time.Sleep(5 * time.Minute)
		visitorsMu.Lock()
		for ip, v := range visitors {
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(visitors, ip)
			}
		}
		visitorsMu.Unlock()
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
			log.Printf("rate limit: redis fallback for %s: %v", ip, err)
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
