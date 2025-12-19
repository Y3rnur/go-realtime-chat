package ws

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
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
	userQ := r.URL.Query().Get("user_id")
	if userQ == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - missing user_id from %s", r.RemoteAddr)
		return
	}
	if _, err := uuid.Parse(userQ); err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - invalid user_id %q from %s", userQ, r.RemoteAddr)
		return
	}

	secret := os.Getenv("WS_SECRET")
	if secret != "" {
		auth := r.Header.Get("Authorization")
		tokenQ := r.URL.Query().Get("token")
		if auth != "Bearer "+secret && tokenQ != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			log.Printf("ws: upgrade unauthorized (missing/invalid auth/token) for user %s", userQ)
			return
		}
	}

	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		http.Error(w, "conversation_id required", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - missing conversation_id from %s", r.RemoteAddr)
		return
	}
	if _, err := uuid.Parse(convID); err != nil {
		http.Error(w, "invalid conversation_id", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - invalid conversation_id %q from %s", convID, r.RemoteAddr)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusInternalServerError)
		log.Printf("ws: upgrade failed for user %s conv %s: %v", userQ, convID, err)
		return
	}

	log.Printf("ws: connected user=%s conv=%s remote=%s", userQ, convID, r.RemoteAddr)
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
