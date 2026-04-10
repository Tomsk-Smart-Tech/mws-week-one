package websocket

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
	// Maximum message size allowed from peer (1 MB).
	maxMessageSize = 1 << 20
)

// Client represents a single WebSocket connection within a Room.
type Client struct {
	hub         *Hub
	room        *Room
	conn        *websocket.Conn
	send        chan []byte // binary CRDT deltas
	systemSend  chan []byte // JSON system/awareness events (text messages)
	userID      string
	userName    string
	cursorColor string
}

// readPump pumps messages from the WebSocket connection to the Room broadcast.
// It runs in its own goroutine and guarantees that at most one reader exists per connection.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[WARN] ws read error (user=%s, room=%s): %v", c.userID, c.room.docID, err)
			}
			return
		}
		// We only care about binary messages (CRDT deltas).
		if mt == websocket.BinaryMessage {
			c.room.broadcast <- &envelope{sender: c, data: data}
		}
		// Text messages from clients are intentionally ignored —
		// system events flow server→client only.
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
// It runs in its own goroutine and guarantees that at most one writer exists per connection.
// It multiplexes binary CRDT data (from send) and JSON system events (from systemSend).
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel — send a close frame and exit.
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				log.Printf("[WARN] ws write error (user=%s, room=%s): %v", c.userID, c.room.docID, err)
				return
			}

		case sysMsg, ok := <-c.systemSend:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			// System events (awareness, reload_table) are sent as text/JSON.
			if err := c.conn.WriteMessage(websocket.TextMessage, sysMsg); err != nil {
				log.Printf("[WARN] ws system write error (user=%s, room=%s): %v", c.userID, c.room.docID, err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
