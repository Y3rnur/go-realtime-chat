package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/Y3rnur/go-realtime-chat/backend"
	"github.com/Y3rnur/go-realtime-chat/backend/store"
)

func main() {
	fs := http.FileServer(http.Dir("./frontend"))

	ctx := context.Background()
	pool, err := store.NewPool(ctx)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.Handle("/", fs)

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "API Status: OK")
	})

	// GET /api/conversations?user_id=<uuid>
	mux.HandleFunc("/api/conversations", func(w http.ResponseWriter, r *http.Request) {
		userQ := r.URL.Query().Get("user_id")
		if userQ == "" {
			http.Error(w, "user_id required", http.StatusBadRequest)
			return
		}
		uid, err := uuid.Parse(userQ)
		if err != nil {
			http.Error(w, "invalid user_id", http.StatusBadRequest)
			return
		}
		convs, err := store.GetConversationsForUser(r.Context(), pool, uid, 50)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(convs)
	})

	// GET /api/messages?conversation_id=<uuid>&limit=50
	// POST /api/messages { conversation_id, author_id, body }
	mux.HandleFunc("/api/messages", func(w http.ResponseWriter, r *http.Request) {
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
			// decoding client payload
			var req struct {
				ConversationID string `json:"conversation_id"`
				AuthorID       string `json:"author_id"`
				Body           string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			convID, err := uuid.Parse(req.ConversationID)
			if err != nil {
				http.Error(w, "invalid conversation_id", http.StatusBadRequest)
				return
			}
			authorID, err := uuid.Parse(req.AuthorID)
			if err != nil {
				http.Error(w, "invalid author_id", http.StatusBadRequest)
				return
			}
			saved, err := store.SaveMessage(r.Context(), pool, convID, authorID, req.Body)
			if err != nil {
				log.Printf("save message error: %v", err)
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(saved)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

	})

	handler := backend.LoggingMiddleware(mux)

	const port = ":8080"
	log.Printf("Server starting on http://localhost%s", port)

	err = http.ListenAndServe(port, handler)
	if err != nil {
		log.Fatal(err)
	}
}
