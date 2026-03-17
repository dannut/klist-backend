//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupHealthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	initDB()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", healthHandler)
	return r
}

// Test 4
// Nume: TestHealthEndpoint_ReturneazaUP
// Ce verifica: ca endpoint-ul /health returneaza status "UP" si HTTP 200
//
//	cand baza de date este accesibila
//
// De ce: pipeline-ul CI/CD foloseste /health pentru smoke tests — daca
//
//	returneaza DOWN, deployment-ul esueaza automat si se face rollback
//
// Tip: integration test — necesita PostgreSQL pornit cu variabilele de mediu:
//
//	DB_HOST, DB_USER, DB_PASSWORD, DB_NAME
func TestHealthEndpoint_ReturneazaUP(t *testing.T) {
	r := setupHealthRouter(t)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("asteptam 200, am primit %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body invalid JSON: %v", err)
	}

	if body["status"] != "UP" {
		t.Errorf("asteptam status=UP, am primit status=%s", body["status"])
	}
}
