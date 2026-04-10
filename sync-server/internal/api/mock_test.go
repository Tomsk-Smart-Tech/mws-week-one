package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Group: Login Endpoint ---

func TestLogin_ValidUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/login?user=denis", nil)
	w := httptest.NewRecorder()

	HandleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["token"] != "fake-jwt-token-for-denis" {
		t.Errorf("token = %q, want %q", resp["token"], "fake-jwt-token-for-denis")
	}
}

func TestLogin_MissingUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
	w := httptest.NewRecorder()

	HandleLogin(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- Group: Tables Endpoint ---

func TestTables_ReturnsList(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/tables", nil)
	w := httptest.NewRecorder()

	HandleTables(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var tables []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&tables); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(tables) != 3 {
		t.Errorf("table count = %d, want 3", len(tables))
	}

	// Verify first table has required fields.
	first := tables[0]
	for _, field := range []string{"id", "name", "columns"} {
		if _, ok := first[field]; !ok {
			t.Errorf("first table missing field %q", field)
		}
	}
}

func TestTables_ContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/tables", nil)
	w := httptest.NewRecorder()

	HandleTables(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}
}
