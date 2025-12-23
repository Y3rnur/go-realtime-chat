package ws

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}

	// Introduction of Redis
	redis  *redis.Client
	ctx    context.Context
	cancel context.CancelFunc

	pool *pgxpool.Pool
}

func NewHub(redisClient *redis.Client, dbPool *pgxpool.Pool) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		clients: make(map[string]map[*Client]struct{}),
		redis:   redisClient,
		ctx:     ctx,
		cancel:  cancel,
		pool:    dbPool,
	}
	if redisClient != nil {
		go h.runPubSub()
	}
	return h
}

func (h *Hub) Close() {
	if h.cancel != nil {
		h.cancel()
	}
}

func (h *Hub) AddClient(convID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[convID]; !ok {
		h.clients[convID] = make(map[*Client]struct{})
	}
	h.clients[convID][c] = struct{}{}
}

func (h *Hub) RemoveClient(convID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.clients[convID]; ok {
		delete(m, c)
		if len(m) == 0 {
			delete(h.clients, convID)
		}
	}
}

// publish message to redis channel for conversation
func (h *Hub) PublishMessage(convID string, v interface{}) error {
	if h.redis == nil {
		log.Printf("hub: publishLocal conv=%s (no redis configured)", convID)
		h.broadcastLocal(convID, v)
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	chanName := "messages:conversation:" + convID

	// Retrying the publish with exponential backoff + jitter.
	const maxAttempts = 5
	backoff := 100 * time.Millisecond
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = h.redis.Publish(h.ctx, chanName, b).Err()
		if err == nil {
			log.Printf("hub: published conv=%s attempt=%d", convID, attempt)
			return nil
		}

		log.Printf("hub: redis publish error conv=%s attempt=%d err=%v", convID, attempt, err)

		if attempt == maxAttempts {
			log.Printf("hub: publish failed after %d attempts; falling back to local broadcast conv=%s", maxAttempts, convID)
			h.broadcastLocal(convID, v)
			return err
		}

		jitter := time.Duration(rand.Intn(200)) * time.Millisecond
		time.Sleep(backoff + jitter)
		backoff *= 2
	}
	// local broadcast (should not reach here, but backoff for safety)
	h.broadcastLocal(convID, v)
	return err
}

// internal local broadcast
func (h *Hub) broadcastLocal(convID string, v interface{}) {
	h.mu.RLock()
	clients := h.clients[convID]
	h.mu.RUnlock()
	if len(clients) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	for c := range clients {
		c.send(b)
	}
}

// run a pattern subscription and relay to local clients
func (h *Hub) runPubSub() {
	pubsub := h.redis.PSubscribe(h.ctx, "messages:conversation:*", "events:conversation:*", "presence:conversation:*")
	log.Printf("hub: started redis psubscribe to messages:conversation:*, events:conversation:*, presence:conversation:*")
	ch := pubsub.Channel()
	for {
		select {
		case <-h.ctx.Done():
			_ = pubsub.Close()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// extracting convID from channel name
			parts := strings.SplitN(msg.Channel, ":", 3)
			if len(parts) < 3 {
				continue
			}
			chanType := parts[0]
			convID := parts[2]
			var payload json.RawMessage = json.RawMessage(msg.Payload)
			h.mu.RLock()
			clients := h.clients[convID]
			h.mu.RUnlock()
			if len(clients) == 0 {
				log.Printf("hub: received %s for conv %s but no local clients", chanType, convID)
				continue
			}
			log.Printf("hub: relaying %s for conv %s to %d client(s)", chanType, convID, len(clients))
			for c := range clients {
				c.send(payload)
			}
		}
	}
}

// publishes typing/read events to events channel
func (h *Hub) PublishEvent(convID string, v interface{}) error {
	if h.redis == nil {
		// broadcast to local clients
		h.broadcastLocal(convID, v)
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	chanName := "events:conversation:" + convID
	return h.redis.Publish(h.ctx, chanName, b).Err()
}

// sets a Redis TTL key and publishes presence delta
func (h *Hub) UpdatePresence(convID, userID, status string) error {
	if h.redis == nil {
		// broadcast presence locally
		payload := map[string]string{
			"type":            "presence",
			"conversation_id": convID,
			"user_id":         userID,
			"status":          status,
		}
		h.broadcastLocal(convID, payload)
		return nil
	}
	key := "presence:" + convID + ":" + userID
	// setting with TTL
	if err := h.redis.Set(h.ctx, key, status, 60*time.Second).Err(); err != nil {
		return err
	}
	// publishing presence delta
	payload := map[string]string{
		"type":            "presence",
		"conversation_id": convID,
		"user_id":         userID,
		"status":          status,
	}
	b, _ := json.Marshal(payload)
	chanName := "presence:conversation:" + convID
	return h.redis.Publish(h.ctx, chanName, b).Err()
}

// parses incoming WS client messages (typing/read/presence)
func (h *Hub) HandleClientMessage(convID string, userID string, raw []byte) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		log.Printf("hub: invalid client message: %v", err)
		return
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "typing":
		payload := map[string]any{
			"type":            "typing",
			"conversation_id": convID,
			"user_id":         userID,
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		}
		if err := h.PublishEvent(convID, payload); err != nil {
			log.Printf("hub: publish typing error: %v", err)
		}
	case "read":
		payload := map[string]any{
			"type":            "read",
			"conversation_id": convID,
			"user_id":         userID,
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		}
		if lr, ok := m["last_read_id"]; ok {
			payload["last_read_id"] = lr
		}
		if err := h.PublishEvent(convID, payload); err != nil {
			log.Printf("hub: publish read error: %v", err)
		}
	case "presence":
		status, _ := m["status"].(string)
		if status == "" {
			status = "online"
		}
		if err := h.UpdatePresence(convID, userID, status); err != nil {
			log.Printf("hub: update presence error: %v", err)
		}
	default:
		// unknown type (just logging it)
		log.Printf("hub: unknown client message type: %q", typ)
	}
}
