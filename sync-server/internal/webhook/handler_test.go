package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockBroadcaster captures BroadcastSystemEvent calls for assertions.
type mockBroadcaster struct {
	mu       sync.Mutex
	received [][]byte
}

func (m *mockBroadcaster) BroadcastSystemEvent(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.received = append(m.received, cp)
}

func (m *mockBroadcaster) events() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.received
}

// --- Group: Happy Path ---

func TestWebhook_ValidPayload(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	handler := HandleMWSUpdate(broadcaster)

	body := `{"table_id": "tbl_001", "action": "row_updated", "source": "mws-api"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mws-update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	events := broadcaster.events()
	if len(events) != 1 {
		t.Fatalf("broadcast count = %d, want 1", len(events))
	}

	var evt SystemEvent
	if err := json.Unmarshal(events[0], &evt); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if evt.Type != "system" {
		t.Errorf("Type = %q, want %q", evt.Type, "system")
	}
	if evt.Action != "reload_table" {
		t.Errorf("Action = %q, want %q", evt.Action, "reload_table")
	}
	if evt.TableID != "tbl_001" {
		t.Errorf("TableID = %q, want %q", evt.TableID, "tbl_001")
	}
}

func TestWebhook_MinimalPayload(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	handler := HandleMWSUpdate(broadcaster)

	body := `{"table_id": "tbl_999"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mws-update", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if len(broadcaster.events()) != 1 {
		t.Fatal("expected 1 broadcast event")
	}
}

// --- Group: Error Cases ---

func TestWebhook_WrongMethod(t *testing.T) {
	handler := HandleMWSUpdate(&mockBroadcaster{})

	req := httptest.NewRequest(http.MethodGet, "/webhooks/mws-update", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhook_InvalidJSON(t *testing.T) {
	handler := HandleMWSUpdate(&mockBroadcaster{})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/mws-update", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWebhook_MissingTableID(t *testing.T) {
	handler := HandleMWSUpdate(&mockBroadcaster{})

	body := `{"action": "row_updated"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mws-update", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWebhook_EmptyBody(t *testing.T) {
	handler := HandleMWSUpdate(&mockBroadcaster{})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/mws-update", strings.NewReader(""))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- Group: No Side Effects on Error ---

func TestWebhook_NoBroadcastOnError(t *testing.T) {
	broadcaster := &mockBroadcaster{}
	handler := HandleMWSUpdate(broadcaster)

	// Missing table_id — should NOT broadcast.
	body := `{"action": "row_updated"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mws-update", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if len(broadcaster.events()) != 0 {
		t.Errorf("broadcast count = %d, want 0 on error", len(broadcaster.events()))
	}
}
