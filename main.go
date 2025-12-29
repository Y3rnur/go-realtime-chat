package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/Y3rnur/go-realtime-chat/backend"
	"github.com/Y3rnur/go-realtime-chat/backend/store"
	"github.com/Y3rnur/go-realtime-chat/backend/ws"
	"github.com/redis/go-redis/v9"
)

func main() {
	fs := http.FileServer(http.Dir("./frontend"))

	ctx := context.Background()
	pool, err := store.NewPool(ctx)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	// creating redis client
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisOpt := &redis.Options{
		Addr: redisAddr,
	}
	if p := os.Getenv("REDIS_PASSWORD"); p != "" {
		redisOpt.Password = p
	}
	redisClient := redis.NewClient(redisOpt)

	hub := ws.NewHub(redisClient, pool)
	defer hub.Close()

	mux := http.NewServeMux()

	// websocket endpoint
	mux.HandleFunc("/ws", hub.ServeWS)

	// auth: login (returns token + sets httpOnly cookie)
	mux.Handle("/api/login", backend.LoginHandler(pool))
	// auth: logout (clears cookie)
	mux.Handle("/api/logout", backend.LogoutHandler(pool))
	// auth: refresh (rotates refresh token & issues new access token)
	mux.Handle("/api/refresh", backend.RefreshHandler(pool))

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "API Status: OK")
	})

	// GET /api/ws_check?conversation_id=<uuid>
	// requires auth (JWT in Authorization header or cookie)
	mux.Handle("/api/ws_check", backend.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("ws_check: incoming %s %s", r.Method, r.URL.String())

		convQ := r.URL.Query().Get("conversation_id")
		if convQ == "" {
			log.Printf("ws_check: missing param conv=%q", convQ)
			http.Error(w, "conversation_id required", http.StatusBadRequest)
			return
		}
		cid, err := uuid.Parse(convQ)
		if err != nil {
			log.Printf("ws_check: invalid conv id %q", convQ)
			http.Error(w, "invalid conversation_id", http.StatusBadRequest)
			return
		}
		uidStr := backend.GetUserIDFromCtx(r.Context())
		uid, err := uuid.Parse(uidStr)
		if err != nil {
			http.Error(w, "invalid user", http.StatusUnauthorized)
			return
		}

		ok, err := store.IsUserInConversation(r.Context(), pool, cid, uid)
		if err != nil {
			log.Printf("ws_check: membership check error conv=%s user=%s: %v", cid, uid, err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			log.Printf("ws_check: forbidden conv=%s user=%s", cid, uid)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		log.Printf("ws_check: allowed conv=%s user=%s", cid, uid)
		w.WriteHeader(http.StatusOK)
	})))

	// GET /api/conversations?user_id=<uuid>
	mux.Handle("/api/conversations", backend.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uidStr := backend.GetUserIDFromCtx(r.Context())
		uid, err := uuid.Parse(uidStr)
		if err != nil {
			http.Error(w, "invalid user", http.StatusUnauthorized)
			return
		}
		convs, err := store.GetConversationsForUser(r.Context(), pool, uid, 50)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(convs)
	})))

	// GET /api/messages?conversation_id=<uuid>&limit=50
	// POST /api/messages { conversation_id, author_id, body }
	mux.Handle("/api/messages", backend.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cq := r.URL.Query().Get("conversation_id")
			if cq == "" {
				http.Error(w, "conversation_id required", http.StatusBadRequest)
				return
			}
			cid, err := uuid.Parse(cq)
			if err != nil {
				http.Error(w, "invalid conversation_id", http.StatusBadRequest)
				return
			}
			limit := 50
			msgs, err := store.GetMessagesForConversation(r.Context(), pool, cid, limit)
			if err != nil {
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(msgs)
			return

		case http.MethodPost:
			// decoding client payload; author will be taken from JWT
			var req struct {
				ConversationID string `json:"conversation_id"`
				Body           string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
				return
			}

			body := strings.TrimSpace(req.Body)
			if body == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "body required"})
				return
			}
			if len(body) > 4000 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "body too long"})
				return
			}

			convID, err := uuid.Parse(req.ConversationID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid conversation_id"})
				return
			}
			// author must match authenticated user
			uidStr := backend.GetUserIDFromCtx(r.Context())
			authorID, err := uuid.Parse(uidStr)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid user"})
				return
			}
			ok, err := store.IsUserInConversation(r.Context(), pool, convID, authorID)
			if err != nil {
				log.Printf("ws_check: membership check error conv=%s user=%s: %v", convID, authorID, err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "server error"})
				return
			}
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
				return
			}

			saved, err := store.SaveMessage(r.Context(), pool, convID, authorID, req.Body)
			if err != nil {
				log.Printf("save message error: %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
				return
			}

			// publishing to redis
			if err := hub.PublishMessage(convID.String(), saved); err != nil {
				log.Printf("redis publish error: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(saved)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

	})))

	mux.Handle("/", fs)

	handler := backend.LoggingMiddleware(mux)

	const port = ":8080"
	log.Printf("Server starting on http://localhost%s", port)

	err = http.ListenAndServe(port, handler)
	if err != nil {
		log.Fatal(err)
	}
}
