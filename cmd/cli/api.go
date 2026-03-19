package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Command mirrors backend/cmd/models.go — keep in sync if fields change.
type Command struct {
	Tool        string  `json:"tool"`
	Syntax      string  `json:"syntax"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

var httpClient = &http.Client{
	Timeout: 8 * time.Second,
}

const maxBodySize = 5 * 1024 * 1024 // 5MB

func fetchCommands(apiURL, query string) ([]Command, error) {
	endpoint := fmt.Sprintf("%s/api/search?q=%s", apiURL, url.QueryEscape(query))

	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("cannot reach backend at %s\nMake sure the server is running", apiURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limit exceeded — please wait a moment")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	// Limit response body to prevent memory exhaustion
	limited := io.LimitReader(resp.Body, maxBodySize)

	var cmds []Command
	if err := json.NewDecoder(limited).Decode(&cmds); err != nil {
		return nil, fmt.Errorf("invalid response: %v", err)
	}
	return cmds, nil
}
