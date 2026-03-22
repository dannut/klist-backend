package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── Gemini API client ─────────────────────────────────────────────────────────

var geminiClient = &http.Client{
	Timeout: 15 * time.Second,
}

func geminiAPIKey() string {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		log.Fatal("GEMINI_API_KEY environment variable is not set")
	}
	return key
}

// ── Prompt injection protection ───────────────────────────────────────────────

func sanitizeForLLM(q string) string {
	replacer := strings.NewReplacer(
		"\n", " ",
		"\r", " ",
		"\t", " ",
		"\"", "",
		"'", "",
		"`", "",
		";", "",
	)
	q = replacer.Replace(q)

	// Limit to first 5 words to prevent prompt stuffing
	words := strings.Fields(q)
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, " ")
}

// ── Known CLI tools (matches slugs in DB) ─────────────────────────────────────
// If LLM returns a tool not in this list, we discard it to prevent hallucination.
// Keep in sync with database/02_seed.sql tools table.
var knownTools = map[string]bool{
	"docker": true, "kubectl": true, "kubernetes": true, "k8s": true,
	"git": true, "linux": true, "bash": true, "vim": true, "nano": true,
	"ssh": true, "curl": true, "wget": true, "grep": true, "awk": true,
	"sed": true, "find": true, "tar": true, "rsync": true, "systemctl": true,
	"nginx": true, "helm": true, "terraform": true, "ansible": true,
	"python": true, "pip": true, "npm": true, "node": true,
	"mysql": true, "psql": true, "redis-cli": true, "mongo": true,
	"aws": true, "gcloud": true, "az": true,
}

func validateTool(tool string) string {
	if tool == "" {
		return ""
	}
	if knownTools[tool] {
		return tool
	}
	var exists bool
	if err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM tools WHERE slug = $1 OR name ILIKE $1)", tool,
	).Scan(&exists); err == nil && exists {
		return tool
	}
	log.Printf("gemini returned unknown tool %q — discarding", tool)
	return ""
}

// ── Gemini quota guards (Redis) ───────────────────────────────────────────────
//
// Two independent counters, both reset at midnight UTC (TTL = remaining seconds
// of the current day so the key expires exactly at the next day boundary):
//
//   kli:gemini:global       — total calls today across all IPs (hard cap: 900)
//   kli:gemini:ip:<ip>      — calls today from a single IP   (hard cap: 20)
//
// When either cap is reached we fall back to SQL search transparently.
// The caller (interpretQuery) receives an error and handlers.go already falls
// back to performVectorSearch → searchDB, so the user always gets a result.

const (
	geminiDailyGlobalCap = 900 // out of 1000 free RPD — 100 buffer
	geminiDailyPerIPCap  = 20  // generous for a normal user (typical: 3-5/day)
)

// secondsUntilMidnightUTC returns the TTL to set on quota keys so they expire
// at the start of the next UTC day (aligning with Google's quota reset).
func secondsUntilMidnightUTC() time.Duration {
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return time.Until(midnight)
}

// geminiQuotaAllow checks both the global and per-IP daily caps.
// Returns (true, nil) when the call is allowed.
// Returns (false, nil) when a cap is reached — caller should fall back to SQL.
// Returns (false, err) only on unexpected Redis errors.
func geminiQuotaAllow(ip string) (bool, error) {
	if rdb == nil {
		// Redis unavailable — allow the call (fail open: better UX than blocking)
		return true, nil
	}

	ctx := context.Background()
	ttl := secondsUntilMidnightUTC()

	globalKey := "kli:gemini:global"
	ipKey := fmt.Sprintf("kli:gemini:ip:%s", ip)

	// Check global cap first (cheaper: avoids per-IP check when quota is full)
	globalCount, err := rdb.Get(ctx, globalKey).Int64()
	if err == nil && globalCount >= geminiDailyGlobalCap {
		log.Printf("gemini quota: global daily cap reached (%d/%d) — falling back to SQL",
			globalCount, geminiDailyGlobalCap)
		return false, nil
	}

	// Check per-IP cap
	ipCount, err := rdb.Get(ctx, ipKey).Int64()
	if err == nil && ipCount >= geminiDailyPerIPCap {
		log.Printf("gemini quota: per-IP cap reached for %s (%d/%d) — falling back to SQL",
			ip, ipCount, geminiDailyPerIPCap)
		return false, nil
	}

	// Both caps OK — increment both counters atomically
	pipe := rdb.Pipeline()
	incrGlobal := pipe.Incr(ctx, globalKey)
	pipe.ExpireNX(ctx, globalKey, ttl) // set TTL only if key is new
	incrIP := pipe.Incr(ctx, ipKey)
	pipe.ExpireNX(ctx, ipKey, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		// Redis write failed — allow the call (fail open)
		log.Printf("gemini quota: redis pipeline error: %v — allowing call", err)
		return true, nil
	}

	// Double-check after increment (race condition safety)
	if incrGlobal.Val() > geminiDailyGlobalCap {
		log.Printf("gemini quota: global cap exceeded after increment — falling back to SQL")
		return false, nil
	}
	if incrIP.Val() > geminiDailyPerIPCap {
		log.Printf("gemini quota: per-IP cap exceeded after increment for %s — falling back to SQL", ip)
		return false, nil
	}

	return true, nil
}

// ── Intent parsing via Gemini 2.5 Flash-Lite ─────────────────────────────────

type QueryIntent struct {
	Tool    string `json:"tool"`
	Keyword string `json:"keyword"`
}

// Gemini generateContent request/response structures
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerateRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGenConfig `json:"generationConfig"`
}

type geminiGenConfig struct {
	Temperature     float32 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

// interpretQuery parses a multi-word query into tool + keyword using Gemini.
// ip is the caller's IP, used for per-IP quota tracking.
// Falls back to SQL search if quota is reached or Gemini is unavailable.
func interpretQuery(ctx context.Context, query, ip string) (QueryIntent, error) {
	// Check quota before calling Gemini
	allowed, err := geminiQuotaAllow(ip)
	if err != nil {
		return QueryIntent{}, fmt.Errorf("quota check error: %v", err)
	}
	if !allowed {
		return QueryIntent{}, fmt.Errorf("gemini quota reached — falling back to SQL")
	}

	prompt := fmt.Sprintf(`You are a DevOps CLI expert. Given a natural language query, return a JSON object with:
- "tool": the CLI tool name if mentioned or implied (e.g. "docker", "kubernetes", "linux"), or empty string
- "keyword": the most specific technical keyword (e.g. "ps", "rm", "exec", "logs"), or empty string if query is just a tool name

Respond ONLY with valid JSON, no explanation, no markdown.

Query: %s`, query)

	reqBody, _ := json.Marshal(geminiGenerateRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: prompt}}},
		},
		GenerationConfig: geminiGenConfig{
			Temperature:     0.0,
			MaxOutputTokens: 64,
		},
	})

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-lite:generateContent?key=%s",
		geminiAPIKey(),
	)

	// Use context-aware request — if client disconnects, HTTP call is cancelled
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return QueryIntent{}, fmt.Errorf("gemini request build error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := geminiClient.Do(req)
	if err != nil {
		return QueryIntent{}, fmt.Errorf("gemini unreachable: %v", err)
	}
	defer resp.Body.Close()

	// 429 = quota exceeded at Google side — fall back gracefully
	if resp.StatusCode == http.StatusTooManyRequests {
		log.Printf("gemini: 429 received — falling back to SQL")
		return QueryIntent{}, fmt.Errorf("gemini rate limited (429) — falling back to SQL")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return QueryIntent{}, fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, body)
	}

	var result geminiGenerateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&result); err != nil {
		return QueryIntent{}, fmt.Errorf("gemini decode error: %v", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return QueryIntent{}, fmt.Errorf("empty response from Gemini")
	}

	raw := result.Candidates[0].Content.Parts[0].Text
	cleaned := strings.NewReplacer("```json", "", "```", "").Replace(strings.TrimSpace(raw))

	var intent QueryIntent
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &intent); err != nil {
		return QueryIntent{}, fmt.Errorf("invalid JSON from Gemini: %v — got: %s", err, cleaned)
	}

	intent.Tool = validateTool(strings.ToLower(strings.TrimSpace(intent.Tool)))
	intent.Keyword = strings.ToLower(strings.TrimSpace(intent.Keyword))

	log.Printf("gemini intent: tool=%q keyword=%q", intent.Tool, intent.Keyword)
	return intent, nil
}

// ── Embeddings via Gemini text-embedding-004 (768 dimensions) ─────────────────
// text-embedding-004 is free tier with no meaningful RPD cap — no quota guard needed.

type geminiEmbedRequest struct {
	Model   string        `json:"model"`
	Content geminiContent `json:"content"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

func getEmbedding(ctx context.Context, text string) ([]float32, error) {
	reqBody, _ := json.Marshal(geminiEmbedRequest{
		Model:                "models/gemini-embedding-001",
		Content:              geminiContent{Parts: []geminiPart{{Text: text}}},
		OutputDimensionality: 768,
	})

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:embedContent?key=%s",
		geminiAPIKey(),
	)

	// Use context-aware request — cancelled if client disconnects
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("gemini embed request build error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := geminiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embeddings unreachable: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gemini embeddings returned status %d: %s", resp.StatusCode, body)
	}

	var result geminiEmbedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding decode error: %v", err)
	}
	if len(result.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding returned from Gemini")
	}
	return result.Embedding.Values, nil
}
