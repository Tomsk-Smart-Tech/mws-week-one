// Package api provides mock REST handlers that unblock frontend development
// before the Java/Kotlin Backend Gateway is ready.
package api

import (
	"encoding/json"
	"net/http"
)

// HandleLogin returns a fake JWT token for the requested user.
// GET /api/login?user=denis → {"token":"fake-jwt-token-for-denis"}
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing 'user' query parameter",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token": "fake-jwt-token-for-" + user,
	})
}

// HandleTables returns a static list of mock MWS tables.
// GET /api/tables → [...]
func HandleTables(w http.ResponseWriter, r *http.Request) {
	tables := []map[string]interface{}{
		{
			"id":      "tbl_001",
			"name":    "Employees",
			"columns": []string{"id", "full_name", "department", "email"},
			"rowCount": 142,
		},
		{
			"id":      "tbl_002",
			"name":    "Projects",
			"columns": []string{"id", "title", "status", "owner_id"},
			"rowCount": 37,
		},
		{
			"id":      "tbl_003",
			"name":    "Knowledge Base Articles",
			"columns": []string{"id", "title", "body", "author_id", "created_at"},
			"rowCount": 583,
		},
	}

	writeJSON(w, http.StatusOK, tables)
}

// writeJSON is a small helper that serializes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
