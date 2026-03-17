//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// Test 5
func TestSearchEndpoint_ReturnEazeRezultate(t *testing.T) {
	baseURL := os.Getenv("TEST_BASE_URL")
	if baseURL == "" {
		t.Skip("Sarim testul: Variabila TEST_BASE_URL nu este setata.")
	}

	resp, err := http.Get(baseURL + "/api/search?q=docker")
	if err != nil {
		t.Fatalf("Eroare la apelul HTTP catre Staging: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Asteptam 200 OK, am primit %d", resp.StatusCode)
	}

	// Simplificat ca sa testam doar raspunsul E2E, fara structuri interne complexe
	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("response body invalid JSON: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("asteptam rezultate reale, am primit array gol")
	}
}

// Test 6
func TestSearchEndpoint_Paginare(t *testing.T) {
	baseURL := os.Getenv("TEST_BASE_URL")
	if baseURL == "" {
		t.Skip("Sarim testul: Variabila TEST_BASE_URL nu este setata.")
	}

	resp, err := http.Get(baseURL + "/api/search?q=docker&page=1&per_page=5")
	if err != nil {
		t.Fatalf("Eroare la apelul HTTP catre Staging: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Asteptam 200 OK, am primit %d", resp.StatusCode)
	}

	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("response body invalid JSON: %v", err)
	}

	if len(results) > 5 {
		t.Errorf("asteptam maxim 5 rezultate, am primit %d", len(results))
	}
}
