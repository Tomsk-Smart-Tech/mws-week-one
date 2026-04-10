// Package metrics provides Prometheus instrumentation for the sync-server.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ActiveWSConnections tracks the current number of open WebSocket connections.
	ActiveWSConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sync_server",
		Name:      "active_ws_connections",
		Help:      "Current number of active WebSocket connections.",
	})

	// ActiveRooms tracks the current number of active document rooms.
	ActiveRooms = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sync_server",
		Name:      "active_rooms",
		Help:      "Current number of active document rooms.",
	})

	// CRDTDeltasTotal counts the total number of CRDT binary deltas received.
	CRDTDeltasTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sync_server",
		Name:      "crdt_deltas_total",
		Help:      "Total number of CRDT binary deltas received from clients.",
	})

	// SnapshotSavesTotal counts the total number of snapshot flush operations.
	SnapshotSavesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sync_server",
		Name:      "snapshot_saves_total",
		Help:      "Total number of snapshot saves attempted to backend gateway.",
	})

	// SnapshotErrorsTotal counts snapshot flush failures.
	SnapshotErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sync_server",
		Name:      "snapshot_errors_total",
		Help:      "Total number of failed snapshot saves.",
	})

	// WebhookEventsTotal counts incoming webhook notifications from MWS.
	WebhookEventsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sync_server",
		Name:      "webhook_events_total",
		Help:      "Total number of MWS webhook events received.",
	})

	// MessagesBroadcastTotal counts total messages fanned out to clients.
	MessagesBroadcastTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sync_server",
		Name:      "messages_broadcast_total",
		Help:      "Total number of messages broadcast to WebSocket clients.",
	})

	// MessagesDroppedTotal counts messages dropped due to slow clients.
	MessagesDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sync_server",
		Name:      "messages_dropped_total",
		Help:      "Total number of messages dropped for slow WebSocket clients.",
	})
)
