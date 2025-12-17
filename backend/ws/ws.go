package ws

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *Client) send(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteMessage(websocket.TextMessage, b)
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		http.Error(w, "conversation_id required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusInternalServerError)
		return
	}

	client := &Client{conn: conn}
	h.AddClient(convID, client)

	go func() {
		defer func() {
			h.RemoveClient(convID, client)
			_ = conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}
