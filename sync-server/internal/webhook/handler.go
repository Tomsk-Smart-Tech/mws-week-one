// Package webhook handles incoming notifications from MWS Tables API
// and routes table-change events to active WebSocket rooms.
package webhook

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// TableUpdatePayload is the JSON body expected from the Kotlin backend
// when MWS Tables sends an update notification.
type TableUpdatePayload struct {
	TableID string `json:"table_id"`
	Action  string `json:"action,omitempty"`  // e.g. "row_updated", "row_deleted"
	Source  string `json:"source,omitempty"`  // e.g. "mws-api"
}

// SystemEvent is the JSON message sent through WebSocket to frontends
// so they know to reload a specific table widget.
type SystemEvent struct {
	Type    string `json:"type"`     // always "system"
	Action  string `json:"action"`   // e.g. "reload_table"
	TableID string `json:"table_id"`
}

// RoomBroadcaster is the interface the webhook handler needs from the Hub
// to push system events into active rooms.
type RoomBroadcaster interface {
	// BroadcastSystemEvent sends a JSON system event to all clients
	// in rooms whose documents contain the given table.
	// For MVP (hackathon), we broadcast to ALL active rooms.
	BroadcastSystemEvent(data []byte)
}

// HandleMWSUpdate returns an http.HandlerFunc for POST /webhooks/mws-update.
// It parses the incoming table update notification and fans it out
// as a system JSON event to all connected WebSocket clients.
func HandleMWSUpdate(broadcaster RoomBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16)) // 64 KB max
		if err != nil {
			http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var payload TableUpdatePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}

		if payload.TableID == "" {
			http.Error(w, `{"error":"table_id is required"}`, http.StatusBadRequest)
			return
		}

		// Build the system event for frontends.
		evt := SystemEvent{
			Type:    "system",
			Action:  "reload_table",
			TableID: payload.TableID,
		}
		evtBytes, err := json.Marshal(evt)
		if err != nil {
			log.Printf("[ERROR] webhook: failed to marshal system event: %v", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		// Fan out to all active WebSocket rooms.
		broadcaster.BroadcastSystemEvent(evtBytes)

		log.Printf("[INFO] webhook: table update broadcasted table_id=%s", payload.TableID)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
