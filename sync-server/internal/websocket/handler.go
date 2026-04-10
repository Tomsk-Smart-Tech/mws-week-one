package websocket

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/auth"
)

// upgrader configures the WebSocket upgrade, allowing all origins for local dev.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// jwtSecret is set from main.go via SetJWTSecret. Empty = skip verification (dev mode).
var jwtSecret string

// SetJWTSecret configures the HMAC secret used for JWT verification.
// If empty, signature verification is skipped (backward-compatible dev mode).
func SetJWTSecret(secret string) {
	jwtSecret = secret
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

		// ---------- Parse and verify JWT token ----------
		token := r.URL.Query().Get("token")
		if token == "" {
			// Also check Authorization header.
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}
		if token == "" {
			http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
			return
		}

		claims, err := auth.ParseJWT(token, jwtSecret)
		if err != nil {
			log.Printf("[WARN] jwt auth failed: %v", err)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		// ---------- Upgrade to WebSocket ----------
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[ERROR] ws upgrade failed: %v", err)
			return
		}

		client := &Client{
			hub:         hub,
			room:        &Room{docID: docID}, // temporary; will be replaced by Hub with the real Room
			conn:        conn,
			send:        make(chan []byte, 256),
			systemSend:  make(chan []byte, 16),
			userID:      claims.UserID,
			userName:    claims.Name,
			cursorColor: claims.CursorColor,
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
