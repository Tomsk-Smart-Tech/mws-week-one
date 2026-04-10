package websocket

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/metrics"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/redis"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/snapshot"
)

// Broker abstracts Redis pub/sub so the hub doesn't import the redis package directly.
type Broker interface {
	Publish(channel string, data []byte) error
	Subscribe(channel string, handler func([]byte)) (redis.Subscription, error)
}

// envelope wraps a binary payload with the identity of its sender so we can skip echoing.
type envelope struct {
	sender *Client
	data   []byte
}

// AwarenessInfo is the server-authoritative user presence info injected
// into the WebSocket stream on connect so clients can't spoof their identity.
type AwarenessInfo struct {
	Type        string `json:"type"`         // "awareness"
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	CursorColor string `json:"cursor_color"`
}

// Room is a set of clients editing the same document.
type Room struct {
	docID         string
	clients       map[*Client]struct{}
	broadcast     chan *envelope
	mu            sync.Mutex
	redisSub      redis.Subscription
	stopBroadcast chan struct{}
	snapWorker    *snapshot.Worker
}

// HubConfig holds tunables for the Hub.
type HubConfig struct {
	GatewayURL       string        // e.g. "http://backend-gateway:8080"
	SnapshotInterval time.Duration // e.g. 10 * time.Second
}

// Hub maintains the set of active Rooms and routes register/unregister events.
type Hub struct {
	rooms      map[string]*Room
	mu         sync.Mutex
	broker     Broker
	config     HubConfig
	register   chan *Client
	unregister chan *Client
	stop       chan struct{}
}

// NewHub creates a new Hub backed by the given Broker (Redis).
func NewHub(broker Broker, cfg HubConfig) *Hub {
	if cfg.SnapshotInterval == 0 {
		cfg.SnapshotInterval = 10 * time.Second
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://backend-gateway:8080"
	}
	return &Hub{
		rooms:      make(map[string]*Room),
		broker:     broker,
		config:     cfg,
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		stop:       make(chan struct{}),
	}
}

// Run processes register/unregister events in a single goroutine — the owner of rooms map.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.addClient(client)
		case client := <-h.unregister:
			h.removeClient(client)
		case <-h.stop:
			return
		}
	}
}

// Shutdown stops the Hub event loop.
func (h *Hub) Shutdown() {
	close(h.stop)
}

// ShutdownGraceful performs a full graceful shutdown:
//  1. Flush snapshots for every active room (in parallel).
//  2. Send 1001 Going Away close frame to every connected client.
//  3. Tear down all rooms (Redis unsub, stop workers).
//
// It respects the provided context deadline (e.g. 10s timeout).
func (h *Hub) ShutdownGraceful(ctx context.Context) {
	h.mu.Lock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.Unlock()

	if len(rooms) == 0 {
		return
	}

	// --- Phase 1: flush all snapshots in parallel ---
	var wg sync.WaitGroup
	for _, room := range rooms {
		wg.Add(1)
		go func(r *Room) {
			defer wg.Done()
			if r.snapWorker != nil {
				if err := r.snapWorker.FlushNow(ctx); err != nil {
					log.Printf("[ERROR] final snapshot failed room=%s: %v", r.docID, err)
				} else {
					log.Printf("[INFO] final snapshot saved room=%s", r.docID)
				}
			}
		}(room)
	}
	wg.Wait()

	// --- Phase 2: close all WebSocket connections with 1001 Going Away ---
	for _, room := range rooms {
		room.mu.Lock()
		for client := range room.clients {
			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
			_ = client.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(2*time.Second))
			client.conn.Close()
		}
		room.mu.Unlock()
	}

	// --- Phase 3: tear down all rooms ---
	h.mu.Lock()
	for _, room := range h.rooms {
		h.teardownRoom(room)
	}
	h.rooms = make(map[string]*Room)
	h.mu.Unlock()

	log.Println("[INFO] graceful shutdown complete: all rooms flushed and closed")
}

// BroadcastSystemEvent sends a JSON system event (e.g. reload_table)
// to ALL clients in ALL active rooms as a TextMessage.
// This implements the webhook.RoomBroadcaster interface.
func (h *Hub) BroadcastSystemEvent(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	metrics.WebhookEventsTotal.Inc()

	for _, room := range h.rooms {
		room.mu.Lock()
		for client := range room.clients {
			select {
			case client.systemSend <- data:
			default:
				log.Printf("[WARN] dropping system event for slow client user=%s room=%s", client.userID, room.docID)
			}
		}
		room.mu.Unlock()
	}
}

// addClient registers a client in its room, creating the room if needed.
func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, exists := h.rooms[c.room.docID]
	if !exists {
		room = h.createRoom(c.room.docID)
		h.rooms[c.room.docID] = room
	}

	c.room = room
	room.mu.Lock()
	room.clients[c] = struct{}{}
	clientCount := len(room.clients)
	room.mu.Unlock()

	metrics.ActiveWSConnections.Inc()
	log.Printf("[INFO] client joined room=%s user=%s (clients=%d)", room.docID, c.userID, clientCount)

	// Send server-authoritative awareness info to the new client.
	h.sendAwareness(c)
}

// sendAwareness pushes the server-injected user identity to the client
// so the frontend can trust user_id, name, and cursor_color.
func (h *Hub) sendAwareness(c *Client) {
	info := AwarenessInfo{
		Type:        "awareness",
		UserID:      c.userID,
		Name:        c.userName,
		CursorColor: c.cursorColor,
	}
	data, err := json.Marshal(info)
	if err != nil {
		log.Printf("[ERROR] failed to marshal awareness info: %v", err)
		return
	}
	select {
	case c.systemSend <- data:
	default:
	}
}

// removeClient unregisters a client and cleans up empty rooms.
func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, exists := h.rooms[c.room.docID]
	if !exists {
		return
	}

	room.mu.Lock()
	if _, ok := room.clients[c]; ok {
		delete(room.clients, c)
		close(c.send)
		close(c.systemSend)
	}
	remaining := len(room.clients)
	room.mu.Unlock()

	metrics.ActiveWSConnections.Dec()
	log.Printf("[INFO] client left room=%s user=%s (remaining=%d)", room.docID, c.userID, remaining)

	// Last client left → final snapshot + room teardown.
	if remaining == 0 {
		if room.snapWorker != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := room.snapWorker.FlushNow(ctx); err != nil {
				log.Printf("[ERROR] final snapshot on room close failed room=%s: %v", room.docID, err)
			} else {
				log.Printf("[INFO] final snapshot on room close room=%s", room.docID)
			}
			cancel()
		}
		h.teardownRoom(room)
		delete(h.rooms, room.docID)
		metrics.ActiveRooms.Dec()
		log.Printf("[INFO] room destroyed room=%s", room.docID)
	}
}

// createRoom initializes a Room, starts its broadcast loop, Redis subscription,
// and snapshot worker.
func (h *Hub) createRoom(docID string) *Room {
	room := &Room{
		docID:         docID,
		clients:       make(map[*Client]struct{}),
		broadcast:     make(chan *envelope, 256),
		stopBroadcast: make(chan struct{}),
	}

	// Start snapshot worker for periodic state persistence.
	worker := snapshot.NewWorker(docID, h.config.GatewayURL, h.config.SnapshotInterval)
	worker.Start()
	room.snapWorker = worker

	// Subscribe to Redis channel for this document — enables horizontal scaling.
	sub, err := h.broker.Subscribe(docID, func(data []byte) {
		// Message from another sync-server instance → broadcast to local clients.
		room.mu.Lock()
		defer room.mu.Unlock()
		for client := range room.clients {
			select {
			case client.send <- data:
				metrics.MessagesBroadcastTotal.Inc()
			default:
				metrics.MessagesDroppedTotal.Inc()
				log.Printf("[WARN] dropping message for slow client user=%s room=%s", client.userID, docID)
			}
		}
	})
	if err != nil {
		log.Printf("[ERROR] redis subscribe failed for room=%s: %v", docID, err)
	} else {
		room.redisSub = sub
	}

	// Start broadcast loop.
	go room.broadcastLoop(h.broker)

	metrics.ActiveRooms.Inc()
	log.Printf("[INFO] room created room=%s (snapshot every %v)", docID, h.config.SnapshotInterval)
	return room
}

// broadcastLoop fans out incoming envelopes to all clients except the sender,
// publishes to Redis for cross-instance delivery, and updates the snapshot state.
func (r *Room) broadcastLoop(broker Broker) {
	for {
		select {
		case env, ok := <-r.broadcast:
			if !ok {
				return
			}

			metrics.CRDTDeltasTotal.Inc()

			// Update cached state for snapshot persistence.
			if r.snapWorker != nil {
				r.snapWorker.UpdateState(env.data)
			}

			// Publish to Redis so other sync-server instances receive the delta.
			if broker != nil {
				if err := broker.Publish(r.docID, env.data); err != nil {
					log.Printf("[ERROR] redis publish failed room=%s: %v", r.docID, err)
				}
			}

			// Fan out to local clients, skip sender.
			r.mu.Lock()
			for client := range r.clients {
				if client == env.sender {
					continue
				}
				select {
				case client.send <- env.data:
					metrics.MessagesBroadcastTotal.Inc()
				default:
					metrics.MessagesDroppedTotal.Inc()
					log.Printf("[WARN] dropping message for slow client user=%s room=%s", client.userID, r.docID)
				}
			}
			r.mu.Unlock()

		case <-r.stopBroadcast:
			return
		}
	}
}

// teardownRoom shuts down a room's broadcast loop, snapshot worker, and Redis sub.
func (h *Hub) teardownRoom(room *Room) {
	// Stop broadcast loop.
	close(room.stopBroadcast)

	// Stop snapshot worker goroutine.
	if room.snapWorker != nil {
		room.snapWorker.Stop()
	}

	// Unsubscribe from Redis.
	if room.redisSub != nil {
		room.redisSub.Close()
	}

	// Close all remaining client send channels.
	room.mu.Lock()
	for client := range room.clients {
		close(client.send)
		close(client.systemSend)
		delete(room.clients, client)
	}
	room.mu.Unlock()
}
