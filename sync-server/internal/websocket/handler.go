package websocket

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// upgrader configures the WebSocket upgrade, allowing all origins for local dev.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// HandleWS returns an http.HandlerFunc that upgrades requests on /ws/doc/{doc_id}
// to WebSocket connections and registers them with the Hub.
func HandleWS(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ---------- Parse doc_id from URL path ----------
		// Expected path: /ws/doc/{doc_id}
		docID := extractDocID(r.URL.Path)
		if docID == "" {
			http.Error(w, `{"error":"missing doc_id in path"}`, http.StatusBadRequest)
			return
		}

		// ---------- Parse token (placeholder auth) ----------
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, `{"error":"missing token query parameter"}`, http.StatusUnauthorized)
			return
		}
		// TODO: validate JWT via Backend Gateway. For now accept any non-empty token.
		userID := parseUserFromToken(token)

		// ---------- Upgrade to WebSocket ----------
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[ERROR] ws upgrade failed: %v", err)
			return
		}

		client := &Client{
			hub:    hub,
			room:   &Room{docID: docID}, // temporary; will be replaced by Hub with the real Room
			conn:   conn,
			send:   make(chan []byte, 256),
			userID: userID,
		}

		hub.register <- client

		// Start read/write pumps in their own goroutines.
		go client.writePump()
		go client.readPump()
	}
}

// extractDocID pulls the document ID from a URL path like /ws/doc/{doc_id}.
func extractDocID(path string) string {
	const prefix = "/ws/doc/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	docID := strings.TrimPrefix(path, prefix)
	// Strip trailing slash if any.
	docID = strings.TrimRight(docID, "/")
	return docID
}

// parseUserFromToken extracts a user identifier from the token string.
// In production this would decode and verify a JWT; here we use a naive approach.
func parseUserFromToken(token string) string {
	// Convention: "fake-jwt-token-for-{username}"
	const prefix = "fake-jwt-token-for-"
	if strings.HasPrefix(token, prefix) {
		return strings.TrimPrefix(token, prefix)
	}
	return "anonymous"
}
