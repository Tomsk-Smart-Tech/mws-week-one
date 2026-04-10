// Package main is the entry point for the sync-server signaling server.
// It wires together WebSocket hub, Redis pub/sub, mock REST API handlers,
// Prometheus metrics, MWS webhook router, JWT auth, and graceful shutdown.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/api"
	iredis "github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/redis"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/webhook"
	"github.com/tomsk-smart-tech/mws-week-one/sync-server/internal/websocket"
)

func main() {
	// --------------- Configuration from ENV ---------------
	port := envOrDefault("PORT", "8081")
	redisURL := envOrDefault("REDIS_URL", "redis://localhost:6379")
	gatewayURL := envOrDefault("GATEWAY_URL", "http://backend-gateway:8080")
	snapshotSec := envOrDefault("SNAPSHOT_INTERVAL_SEC", "10")
	jwtSecret := envOrDefault("JWT_SECRET", "") // empty = skip verification (dev mode)

	snapshotInterval, err := time.ParseDuration(snapshotSec + "s")
	if err != nil {
		snapshotInterval = 10 * time.Second
	}

	// --------------- JWT Secret ---------------
	websocket.SetJWTSecret(jwtSecret)
	if jwtSecret == "" {
		log.Println("[WARN] JWT_SECRET is empty — running in dev mode (signature verification disabled)")
	}

	// --------------- Redis Broker ---------------
	broker, err := iredis.NewBroker(redisURL)
	if err != nil {
		log.Fatalf("[FATAL] failed to connect to Redis: %v", err)
	}
	log.Println("[INFO] connected to Redis")

	// --------------- WebSocket Hub ---------------
	hub := websocket.NewHub(broker, websocket.HubConfig{
		GatewayURL:       gatewayURL,
		SnapshotInterval: snapshotInterval,
	})
	go hub.Run()

	// --------------- HTTP Mux ---------------
	mux := http.NewServeMux()

	// WebSocket endpoint: /ws/doc/{doc_id}?token={jwt}
	mux.HandleFunc("/ws/doc/", websocket.HandleWS(hub))

	// Mock REST API
	mux.HandleFunc("/api/login", api.HandleLogin)
	mux.HandleFunc("/api/tables", api.HandleTables)

	// Webhook: MWS table update notifications
	mux.HandleFunc("/webhooks/mws-update", webhook.HandleMWSUpdate(hub))

	// Prometheus metrics
	mux.Handle("/metrics", promhttp.Handler())

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// --------------- HTTP Server ---------------
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      withCORS(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// --------------- Start listening ---------------
	go func() {
		log.Printf("[INFO] sync-server listening on :%s (gateway=%s, snapshot=%v)", port, gatewayURL, snapshotInterval)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] server error: %v", err)
		}
	}()

	// --------------- Graceful Shutdown ---------------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	sig := <-sigCh
	log.Printf("[INFO] received signal %v — starting graceful shutdown...", sig)

	// Create a deadline for the entire shutdown sequence.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Step 1: Stop accepting new connections.
	log.Println("[INFO] step 1/4: stopping HTTP listener...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[ERROR] http shutdown: %v", err)
	}

	// Step 2: Stop the Hub event loop (no more register/unregister processing).
	log.Println("[INFO] step 2/4: stopping hub event loop...")
	hub.Shutdown()

	// Step 3: Flush all snapshots, send 1001 Going Away, tear down rooms.
	log.Println("[INFO] step 3/4: flushing snapshots & closing WebSockets...")
	hub.ShutdownGraceful(shutdownCtx)

	// Step 4: Close Redis.
	log.Println("[INFO] step 4/4: closing Redis connection...")
	broker.Close()

	log.Println("[INFO] sync-server stopped cleanly")
}

// envOrDefault reads an environment variable or returns a fallback.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// withCORS wraps a handler with permissive CORS headers (suitable for local dev).
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
