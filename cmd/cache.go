package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// ── Redis cache ───────────────────────────────────────────────────────────────

const cacheTTL = 24 * time.Hour

var rdb *redis.Client
var ctx = context.Background()

func initCache() {
	addr := getenv("REDIS_URL", "redis.kli.svc.cluster.local:6379")
	rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: getenv("REDIS_PASSWORD", ""),
		DB:       0,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		// Cache is optional — app runs without it, just slower
		slog.Warn("Redis unavailable — running without cache", "addr", addr, "err", err)
		rdb = nil
		return
	}
	slog.Info("Connected to Redis", "addr", addr)
}

// cacheKey builds a privacy-safe cache key using SHA256 of the query.
// The raw query never appears in Redis keys, metrics, or logs.
func cacheKey(q string, page, perPage int) string {
	h := sha256.Sum256([]byte(q))
	return fmt.Sprintf("kli:search:%x:p%d:pp%d", h[:8], page, perPage)
}

// cacheGet retrieves cached results. Returns nil if miss or Redis unavailable.
func cacheGet(key string) []Command {
	if rdb == nil {
		return nil
	}
	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return nil // cache miss
	}
	var results []Command
	if err := json.Unmarshal([]byte(val), &results); err != nil {
		return nil
	}
	return results
}

// cacheSet stores results in Redis with TTL. Silently fails if Redis unavailable.
func cacheSet(key string, results []Command) {
	if rdb == nil {
		return
	}
	data, err := json.Marshal(results)
	if err != nil {
		return
	}
	if err := rdb.Set(ctx, key, data, cacheTTL).Err(); err != nil {
		slog.Warn("cache write error", "err", err)
	}
}

// cacheInvalidate clears all kli:search:* keys using SCAN (safe at scale).
// Called when DB data is updated (e.g. after running generate_embeddings.py).
func cacheInvalidate() {
	if rdb == nil {
		return
	}
	var cursor uint64
	var deleted int
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, "kli:search:*", 100).Result()
		if err != nil {
			slog.Warn("cache invalidate scan error", "err", err)
			return
		}
		if len(keys) > 0 {
			rdb.Del(ctx, keys...)
			deleted += len(keys)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	slog.Info("Cache invalidated", "keys_removed", deleted)
}
