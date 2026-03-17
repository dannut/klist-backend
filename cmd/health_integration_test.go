//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// Test 4
// Nume: TestHealthEndpoint_ReturneazaUP
// Ce verifica: ca endpoint-ul /health de pe Staging returneaza status "UP"
// De ce: pipeline-ul CI/CD foloseste /health pentru smoke tests.
// Tip: True Integration/E2E test — face un request HTTP real catre TEST_BASE_URL.
func TestHealthEndpoint_ReturneazaUP(t *testing.T) {
	// 1. Luam URL-ul din GitHub Actions (ex: https://test.kli.st)
	baseURL := os.Getenv("TEST_BASE_URL")
	if baseURL == "" {
		t.Skip("Sarim testul: Variabila TEST_BASE_URL nu este setata.")
	}

	// 2. Facem o cerere HTTP reala catre mediul de Staging
	resp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("Eroare la apelul HTTP catre Staging: %v", err)
	}
	defer resp.Body.Close()

	// 3. Verificam ca primim HTTP 200 OK
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Asteptam 200 OK de la Staging, am primit %d", resp.StatusCode)
	}

	// 4. Citim si decodam JSON-ul returnat
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Response body invalid JSON: %v", err)
	}

	// 5. Validam ca aplicatia e UP
	if body["status"] != "UP" {
		t.Errorf("Asteptam status=UP de la Staging, am primit status=%s", body["status"])
	}
}
