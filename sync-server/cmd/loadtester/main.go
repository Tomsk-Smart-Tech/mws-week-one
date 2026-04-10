// Package main provides a WebSocket load-testing utility for sync-server.
// It spawns N bots that join a single room, continuously send random binary
// payloads, and drain incoming broadcasts. Useful for detecting memory leaks,
// race conditions, and mutex contention under heavy concurrency.
//
// Usage:
//
//	go run ./cmd/loadtester -bots=100 -interval=100ms -room=stress-test -addr=localhost:8081
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	// ---------- CLI flags ----------
	bots := flag.Int("bots", 100, "number of concurrent bot connections")
	interval := flag.Duration("interval", 100*time.Millisecond, "send interval per bot")
	room := flag.String("room", "stress-test", "document/room ID to join")
	addr := flag.String("addr", "localhost:8081", "sync-server host:port")
	payloadSize := flag.Int("size", 1024, "payload size in bytes")
	flag.Parse()

	log.Printf("[LOAD] starting %d bots → ws://%s/ws/doc/%s (interval=%v, payload=%d bytes)",
		*bots, *addr, *room, *interval, *payloadSize)

	// ---------- Metrics ----------
	var (
		totalSent atomic.Int64
		totalRecv atomic.Int64
		errors    atomic.Int64
	)

	// ---------- Signal handling ----------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	stopCh := make(chan struct{})
	go func() {
		<-sigCh
		log.Println("[LOAD] shutting down bots...")
		close(stopCh)
	}()

	// ---------- Spawn bots ----------
	var wg sync.WaitGroup

	for i := 0; i < *bots; i++ {
		wg.Add(1)
		go func(botID int) {
			defer wg.Done()
			runBot(botID, *addr, *room, *interval, *payloadSize, stopCh, &totalSent, &totalRecv, &errors)
		}(i)

		// Stagger connections slightly to avoid thundering herd.
		time.Sleep(5 * time.Millisecond)
	}

	// ---------- Stats printer ----------
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				log.Printf("[STATS] sent=%d  recv=%d  errors=%d",
					totalSent.Load(), totalRecv.Load(), errors.Load())
			case <-stopCh:
				return
			}
		}
	}()

	wg.Wait()
	log.Printf("[LOAD] done. total sent=%d  recv=%d  errors=%d",
		totalSent.Load(), totalRecv.Load(), errors.Load())
}

// runBot simulates a single user: connects to the WS room, sends random payloads,
// and drains incoming messages until stopCh is closed.
func runBot(
	id int,
	addr, room string,
	interval time.Duration,
	payloadSize int,
	stopCh <-chan struct{},
	sent, recv, errs *atomic.Int64,
) {
	token := fmt.Sprintf("fake-jwt-token-for-bot-%d", id)
	u := url.URL{
		Scheme:   "ws",
		Host:     addr,
		Path:     "/ws/doc/" + room,
		RawQuery: "token=" + token,
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("[BOT-%d] connect failed: %v", id, err)
		errs.Add(1)
		return
	}
	defer conn.Close()

	// Reader goroutine: drain broadcast messages.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
			recv.Add(1)
		}
	}()

	// Writer loop: send random payloads at the configured interval.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	payload := make([]byte, payloadSize)

	for {
		select {
		case <-stopCh:
			// Graceful close.
			closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bot stopping")
			_ = conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
			return

		case <-readerDone:
			// Server closed connection.
			return

		case <-ticker.C:
			// Generate random payload.
			if _, err := rand.Read(payload); err != nil {
				errs.Add(1)
				continue
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				errs.Add(1)
				return
			}
			sent.Add(1)
		}
	}
}
