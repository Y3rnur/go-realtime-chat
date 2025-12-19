package ws

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

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
	log.Printf("hub: publishing conv=%s to redis channel=%s payload_type=%T", convID, chanName, v)
	return h.redis.Publish(h.ctx, chanName, b).Err()
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
	pubsub := h.redis.PSubscribe(h.ctx, "messages:conversation:*")
	log.Printf("hub: started redis psubscribe to messages:conversation:*")
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
			parts := strings.SplitN(msg.Channel, "messages:conversation:", 2)
			if len(parts) != 2 {
				continue
			}
			convID := parts[1]
			var payload json.RawMessage = json.RawMessage(msg.Payload)
			h.mu.RLock()
			clients := h.clients[convID]
			h.mu.RUnlock()
			if len(clients) == 0 {
				log.Printf("hub: received message for conv %s but no local clients", convID)
				continue
			}
			log.Printf("hub: relaying message for conv %s to %d client(s)", convID, len(clients))
			for c := range clients {
				c.send(payload)
			}
		}
	}
}
