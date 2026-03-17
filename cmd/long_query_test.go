package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Test 2
// Nume: TestSearchHandler_QueryPreLung
// Ce verifica: ca un query mai lung de 200 caractere returneaza 400 Bad Request
// De ce: protectie impotriva abuzurilor — un user nu trebuie sa poata trimite
//        un query de 10.000 caractere catre DB sau catre llama3.2
// Tip: unit test, nu necesita DB
func TestSearchHandler_QueryPreLung(t *testing.T) {
	r   := setupRouter()
	q   := strings.Repeat("a", 201)
	req := httptest.NewRequest("GET", "/api/search?q="+q, nil)
	w   := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("asteptam 400, am primit %d", w.Code)
	}
}
