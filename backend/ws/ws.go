package ws

import (
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/Y3rnur/go-realtime-chat/backend"
	"github.com/Y3rnur/go-realtime-chat/backend/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	userID string
	convID string
}

func (c *Client) send(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteMessage(websocket.TextMessage, b)
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// derive user from JWT (Authorization header, cookie, or ?token)
	uidStr, err := backend.GetUserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		log.Printf("ws: upgrade unauthorized (token) from %s: %v", r.RemoteAddr, err)
		return
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		http.Error(w, "invalid user", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - invalid user_id %q from %s", uidStr, r.RemoteAddr)
		return
	}

	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		http.Error(w, "conversation_id required", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - missing conversation_id from %s", r.RemoteAddr)
		return
	}
	cid, err := uuid.Parse(convID)
	if err != nil {
		http.Error(w, "invalid conversation_id", http.StatusBadRequest)
		log.Printf("ws: upgrade rejected - invalid conversation_id %q from %s", convID, r.RemoteAddr)
		return
	}

	if h.pool != nil {
		ok, err := store.IsUserInConversation(r.Context(), h.pool, cid, uid)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			log.Printf("ws: membership check error for user=%s conv=%s: %v", uidStr, convID, err)
			return
		}
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			log.Printf("ws: upgrade forbidden - user %s not participant of conv %s", uidStr, convID)
			return
		}
	} else {
		log.Printf("ws: no db pool configured, skipping membership check for user %s conv %s", uidStr, convID)
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusInternalServerError)
		log.Printf("ws: upgrade failed for user %s conv %s: %v", uidStr, convID, err)
		return
	}

	log.Printf("ws: connected user=%s conv=%s remote=%s", uidStr, convID, r.RemoteAddr)
	client := &Client{conn: conn, userID: uidStr, convID: convID}
	h.AddClient(convID, client)

	go func() {
		defer func() {
			h.RemoveClient(convID, client)
			_ = conn.Close()
		}()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// forwarding the client-sent messages (typing/read/presence) to hub
			h.HandleClientMessage(convID, uidStr, msg)
		}
	}()
}
