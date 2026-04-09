// Package snapshot provides a periodic worker that persists the latest CRDT
// document state to the backend gateway via HTTP POST.
package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Worker periodically flushes the latest document state to the backend gateway.
// It is created per-Room and runs in its own goroutine.
type Worker struct {
	docID       string
	gatewayURL  string // full base URL, e.g. "http://backend-gateway:8080"
	interval    time.Duration
	httpClient  *http.Client

	mu        sync.Mutex
	lastState []byte // latest merged CRDT state
	dirty     bool   // true if lastState changed since last flush

	stopCh chan struct{}
	done   chan struct{} // closed when the worker goroutine exits
}

// NewWorker creates a snapshot worker for the given document.
// gatewayBase example: "http://backend-gateway:8080"
func NewWorker(docID, gatewayBase string, interval time.Duration) *Worker {
	return &Worker{
		docID:      docID,
		gatewayURL: gatewayBase,
		interval:   interval,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Start launches the background flush loop. Call Stop() to terminate it.
func (w *Worker) Start() {
	go w.loop()
}

// UpdateState replaces the cached state with a new CRDT delta/snapshot.
// This is called from the Room's broadcast loop each time a binary message arrives.
func (w *Worker) UpdateState(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Copy the slice so we don't hold references to the WebSocket read buffer.
	cp := make([]byte, len(data))
	copy(cp, data)
	w.lastState = cp
	w.dirty = true
}

// FlushNow forces an immediate snapshot push regardless of the dirty flag.
// It is used during graceful shutdown and when the last client leaves.
// Returns an error if the HTTP POST fails.
func (w *Worker) FlushNow(ctx context.Context) error {
	w.mu.Lock()
	state := w.lastState
	w.dirty = false
	w.mu.Unlock()

	if len(state) == 0 {
		return nil // nothing to persist
	}
	return w.post(ctx, state)
}

// Stop signals the worker goroutine to exit and waits for it to finish.
func (w *Worker) Stop() {
	close(w.stopCh)
	<-w.done // block until the loop goroutine exits
}

// Done returns a channel that is closed when the worker goroutine exits.
func (w *Worker) Done() <-chan struct{} {
	return w.done
}

// loop is the main periodic flush loop.
func (w *Worker) loop() {
	defer close(w.done)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			dirty := w.dirty
			state := w.lastState
			if dirty {
				w.dirty = false
			}
			w.mu.Unlock()

			if !dirty || len(state) == 0 {
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := w.post(ctx, state); err != nil {
				log.Printf("[ERROR] snapshot flush failed room=%s: %v", w.docID, err)
				// Mark dirty again so the next tick retries.
				w.mu.Lock()
				w.dirty = true
				w.mu.Unlock()
			} else {
				log.Printf("[INFO] snapshot flushed room=%s size=%d bytes", w.docID, len(state))
			}
			cancel()

		case <-w.stopCh:
			return
		}
	}
}

// post sends the binary state to the backend gateway.
// POST /api/internal/docs/{doc_id}/snapshot  (application/octet-stream)
func (w *Worker) post(ctx context.Context, data []byte) error {
	url := fmt.Sprintf("%s/api/internal/docs/%s/snapshot", w.gatewayURL, w.docID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("backend returned status %d", resp.StatusCode)
	}
	return nil
}
