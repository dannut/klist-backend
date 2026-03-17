package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/search", searchHandler)
	return r
}

// Test 1
// Nume: TestSearchHandler_QueryLipseste
// Ce verifica: ca un request fara parametrul "q" returneaza 400 Bad Request
// De ce: protectie de baza — serverul nu trebuie sa faca query la DB cu un string gol
// Tip: unit test, nu necesita DB
func TestSearchHandler_QueryLipseste(t *testing.T) {
	r   := setupRouter()
	req := httptest.NewRequest("GET", "/api/search", nil)
	w   := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("asteptam 400, am primit %d", w.Code)
	}
}
