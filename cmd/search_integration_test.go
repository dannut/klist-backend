//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupSearchRouter(t *testing.T) *gin.Engine {
	t.Helper()
	initDB()
	initCache()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/search", searchHandler)
	return r
}

// Test 5
// Nume: TestSearchEndpoint_ReturnEazeRezultate
// Ce verifica: ca /api/search?q=docker returneaza rezultate reale din DB
//              (array nevid, fiecare element are tool, syntax, description)
// De ce: verifica integrarea reala backend → PostgreSQL → rezultate corecte
//        Daca DB-ul nu are date sau query-ul e gresit, testul pica
// Tip: integration test — necesita PostgreSQL pornit cu date reale (02_seed.sql)
func TestSearchEndpoint_ReturnEazeRezultate(t *testing.T) {
	r   := setupSearchRouter(t)
	req := httptest.NewRequest("GET", "/api/search?q=docker", nil)
	w   := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("asteptam 200, am primit %d", w.Code)
	}

	var results []Command
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("response body invalid JSON: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("asteptam rezultate reale din DB, am primit array gol")
	}

	for i, cmd := range results {
		if cmd.Tool == "" {
			t.Errorf("rezultatul %d: camp 'tool' gol", i)
		}
		if cmd.Syntax == "" {
			t.Errorf("rezultatul %d: camp 'syntax' gol", i)
		}
		if cmd.Description == "" {
			t.Errorf("rezultatul %d: camp 'description' gol", i)
		}
	}
}

// Test 6
// Nume: TestSearchEndpoint_Paginare
// Ce verifica: ca /api/search?q=docker&page=1&per_page=5 returneaza
//              cel mult 5 rezultate din DB
// De ce: verifica ca paginarea functioneaza corect end-to-end —
//        backend → DB → raspuns paginat. Fara paginare corecta,
//        frontend-ul ar primi prea multe rezultate si UI-ul s-ar rupe
// Tip: integration test — necesita PostgreSQL pornit cu date reale (02_seed.sql)
func TestSearchEndpoint_Paginare(t *testing.T) {
	r   := setupSearchRouter(t)
	req := httptest.NewRequest("GET", "/api/search?q=docker&page=1&per_page=5", nil)
	w   := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("asteptam 200, am primit %d", w.Code)
	}

	var results []Command
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("response body invalid JSON: %v", err)
	}

	if len(results) > 5 {
		t.Errorf("asteptam maxim 5 rezultate, am primit %d", len(results))
	}
}
