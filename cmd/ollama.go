package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ── Ollama base URL ───────────────────────────────────────────────────────────

func ollamaBaseURL() string {
	if url := getenv("OLLAMA_URL", ""); url != "" {
		return url
	}
	return "http://ollama.kli.svc.cluster.local:11434"
}

// ollamaClient has explicit timeouts to prevent goroutine leaks on slow upstream
var ollamaClient = &http.Client{
	Timeout: 120 * time.Second,
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
	// Accept only known tools; discard LLM hallucinations
	if knownTools[tool] {
		return tool
	}
	// Also check DB directly for dynamic tools
	var exists bool
	if err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM tools WHERE slug = $1 OR name ILIKE $1)", tool,
	).Scan(&exists); err == nil && exists {
		return tool
	}
	log.Printf("llama3.2 returned unknown tool %q — discarding", tool)
	return ""
}

type QueryIntent struct {
	Tool    string `json:"tool"`
	Keyword string `json:"keyword"`
}

func interpretQuery(query string) (QueryIntent, error) {
	prompt := fmt.Sprintf(`You are a DevOps CLI expert. Given a natural language query, return a JSON object with:
- "tool": the CLI tool name if mentioned or implied (e.g. "docker", "kubernetes", "linux"), or empty string
- "keyword": the most specific technical keyword (e.g. "ps", "rm", "exec", "logs"), or empty string if query is just a tool name

Respond ONLY with valid JSON, no explanation, no markdown.

Query: %s`, query)

	body, _ := json.Marshal(map[string]interface{}{
		"model":  "qwen2.5:0.5b",
		"prompt": prompt,
		"stream": false,
	})

	resp, err := ollamaClient.Post(ollamaBaseURL()+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return QueryIntent{}, fmt.Errorf("ollama unreachable: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return QueryIntent{}, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var raw struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&raw); err != nil {
		return QueryIntent{}, fmt.Errorf("ollama decode error: %v", err)
	}

	cleaned := strings.NewReplacer("```json", "", "```", "").Replace(strings.TrimSpace(raw.Response))

	var intent QueryIntent
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &intent); err != nil {
		return QueryIntent{}, fmt.Errorf("invalid JSON from llama: %v — got: %s", err, cleaned)
	}

	intent.Tool = validateTool(strings.ToLower(strings.TrimSpace(intent.Tool)))
	intent.Keyword = strings.ToLower(strings.TrimSpace(intent.Keyword))

	// Log only metadata, not the query itself (PII protection)
	log.Printf("llama3.2 intent: tool=%q keyword=%q", intent.Tool, intent.Keyword)
	return intent, nil
}

// ── Embeddings via snowflake-arctic-embed:22m ──────────────────────────────────────────

func getEmbedding(text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":  "snowflake-arctic-embed:22m",
		"prompt": text,
	})

	resp, err := ollamaClient.Post(ollamaBaseURL()+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings unreachable: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embeddings returned status %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding decode error: %v", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}
	return result.Embedding, nil
}
