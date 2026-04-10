package snapshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// --- Group: State Management ---

func TestWorker_UpdateState(t *testing.T) {
	w := NewWorker("doc-1", "http://fake:8080", 1*time.Hour)

	w.UpdateState([]byte("hello"))

	w.mu.Lock()
	defer w.mu.Unlock()
	if string(w.lastState) != "hello" {
		t.Errorf("lastState = %q, want %q", w.lastState, "hello")
	}
	if !w.dirty {
		t.Error("dirty = false, want true after UpdateState")
	}
}

func TestWorker_UpdateState_CopiesData(t *testing.T) {
	w := NewWorker("doc-1", "http://fake:8080", 1*time.Hour)

	original := []byte("original")
	w.UpdateState(original)

	// Mutate the original — worker's copy should be unaffected.
	original[0] = 'X'

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastState[0] == 'X' {
		t.Error("UpdateState should copy data, not hold reference to original slice")
	}
}

// --- Group: FlushNow ---

func TestWorker_FlushNow_Empty(t *testing.T) {
	w := NewWorker("doc-1", "http://fake:8080", 1*time.Hour)

	// No state – FlushNow should return nil (nothing to persist).
	err := w.FlushNow(context.Background())
	if err != nil {
		t.Fatalf("FlushNow with empty state should return nil, got: %v", err)
	}
}

func TestWorker_FlushNow_PostsToGateway(t *testing.T) {
	var called atomic.Int32
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		called.Add(1)
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		rw.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWorker("test-doc", srv.URL, 1*time.Hour)
	w.UpdateState([]byte("crdt-snapshot-data"))

	err := w.FlushNow(context.Background())
	if err != nil {
		t.Fatalf("FlushNow error: %v", err)
	}
	if called.Load() != 1 {
		t.Errorf("POST called %d times, want 1", called.Load())
	}
	if string(receivedBody) != "crdt-snapshot-data" {
		t.Errorf("body = %q, want %q", receivedBody, "crdt-snapshot-data")
	}
}

func TestWorker_FlushNow_ClearsDirty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWorker("doc-1", srv.URL, 1*time.Hour)
	w.UpdateState([]byte("data"))

	_ = w.FlushNow(context.Background())

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dirty {
		t.Error("dirty should be false after successful FlushNow")
	}
}

func TestWorker_FlushNow_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	w := NewWorker("doc-1", srv.URL, 1*time.Hour)
	w.UpdateState([]byte("data"))

	err := w.FlushNow(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// --- Group: Worker Lifecycle ---

func TestWorker_StartStop(t *testing.T) {
	w := NewWorker("doc-1", "http://fake:8080", 50*time.Millisecond)
	w.Start()

	// Let it tick a couple times.
	time.Sleep(120 * time.Millisecond)

	// Stop should not hang.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Worker.Stop() hung — possible goroutine leak")
	}
}

func TestWorker_PeriodicFlush(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWorker("doc-1", srv.URL, 50*time.Millisecond)
	w.UpdateState([]byte("data"))
	w.Start()

	// Wait for at least 2 flushes.
	time.Sleep(180 * time.Millisecond)
	w.Stop()

	// First tick should flush (dirty=true). Second tick should NOT (dirty=false since no new updates).
	if calls.Load() < 1 {
		t.Errorf("expected at least 1 periodic flush, got %d", calls.Load())
	}
}
