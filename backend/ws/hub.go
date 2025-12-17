package ws

import (
	"encoding/json"
	"sync"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]map[*Client]struct{}),
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

func (h *Hub) Broadcast(convID string, v interface{}) {
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
