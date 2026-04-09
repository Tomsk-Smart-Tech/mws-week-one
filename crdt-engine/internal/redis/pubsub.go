// Package redis provides a Pub/Sub broker backed by Redis for cross-instance
// broadcast of CRDT deltas between multiple crdt-engine replicas.
package redis

import (
	"context"
	"log"

	goredis "github.com/redis/go-redis/v9"
)

// Subscription represents an active Redis Pub/Sub subscription that can be closed.
type Subscription interface {
	Close() error
}

// redisSub wraps a go-redis PubSub and its cancel function.
type redisSub struct {
	ps     *goredis.PubSub
	cancel context.CancelFunc
}

func (s *redisSub) Close() error {
	s.cancel()
	return s.ps.Close()
}

// Broker wraps a Redis client and exposes publish/subscribe operations.
type Broker struct {
	client *goredis.Client
}

// NewBroker creates a Broker from a Redis URL (e.g. "redis://localhost:6379").
func NewBroker(redisURL string) (*Broker, error) {
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := goredis.NewClient(opts)

	// Verify connectivity.
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &Broker{client: client}, nil
}

// Publish sends a binary payload to the given Redis channel.
func (b *Broker) Publish(channel string, data []byte) error {
	return b.client.Publish(context.Background(), channel, data).Err()
}

// Subscribe starts a goroutine that listens on the given Redis channel
// and calls handler for each incoming message. Returns a Subscription
// that must be closed when the room is torn down to prevent goroutine leaks.
func (b *Broker) Subscribe(channel string, handler func([]byte)) (Subscription, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ps := b.client.Subscribe(ctx, channel)

	// Wait for confirmation that subscription is active.
	if _, err := ps.Receive(ctx); err != nil {
		cancel()
		_ = ps.Close()
		return nil, err
	}

	go func() {
		ch := ps.Channel()
		for msg := range ch {
			handler([]byte(msg.Payload))
		}
		log.Printf("[INFO] redis subscription closed channel=%s", channel)
	}()

	return &redisSub{ps: ps, cancel: cancel}, nil
}

// Close shuts down the underlying Redis connection.
func (b *Broker) Close() error {
	return b.client.Close()
}
