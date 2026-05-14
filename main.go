package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Y3rnur/go-realtime-chat/backend"
	"github.com/Y3rnur/go-realtime-chat/backend/store"
	"github.com/Y3rnur/go-realtime-chat/backend/ws"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	// verifying Redis connectivity (in case it fails, local-only hub proceeds)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("redis: ping failed (%v) - continuing without redis (local-only)", err)
		redisClient = nil
	} else {
		log.Printf("redis: connected to %s", redisAddr)
	}

	hub := ws.NewHub(redisClient, pool)
	defer hub.Close()

	mux := http.NewServeMux()

	registerConversationInfoHandlers(mux, pool, hub)

	// websocket endpoint
	mux.HandleFunc("/ws", hub.ServeWS)

	// auth: login (returns token + sets httpOnly cookie)
	mux.Handle("/api/login", backend.LoginHandler(pool))
	// auth: logout (clears cookie)
	mux.Handle("/api/logout", backend.LogoutHandler(pool))
	// auth: refresh (rotates refresh token & issues new access token)
	mux.Handle("/api/refresh", backend.RefreshHandler(pool))
	// auth: register (registers user and returns token + cookie)
	mux.Handle("/api/register", backend.RegisterHandler(pool))

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

	// GET /api/users?q=<query>     - returns basic user list for participant picker
	mux.Handle("/api/users", backend.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		log.Printf("users search q=%q from %s", q, r.RemoteAddr)
		if strings.TrimSpace(q) == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]any{})
			return
		}
		users, err := store.SearchUsersByDisplayName(r.Context(), pool, q, 20)
		if err != nil {
			log.Printf("search users error: %v", err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	})))

	// GET /api/conversations
	// POST /api/conversations
	// (auth required - user is derived from the access token)
	mux.Handle("/api/conversations", backend.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
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
			return

		case http.MethodPost:
			// creating a new conversation
			var req struct {
				Title        *string  `json:"title"`
				IsGroup      bool     `json:"is_group"`
				Participants []string `json:"participants"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			uidStr := backend.GetUserIDFromCtx(r.Context())
			uid, err := uuid.Parse(uidStr)
			if err != nil {
				http.Error(w, "invalid user", http.StatusUnauthorized)
				return
			}

			var pIDs []uuid.UUID
			for _, s := range req.Participants {
				if s == "" {
					continue
				}

				id, err := uuid.Parse(s)
				if err != nil {
					http.Error(w, "invalid participant id", http.StatusBadRequest)
					return
				}

				pIDs = append(pIDs, id)
			}

			if !req.IsGroup && len(pIDs) > 1 {
				http.Error(w, "cannot create direct conversation with multiple participants; check 'group' to create a group chat", http.StatusBadRequest)
				return
			}

			// determine isGroup if more than 2 participants or explicit flag
			isGroup := req.IsGroup || len(pIDs) > 1

			conv, err := store.CreateConversation(r.Context(), pool, req.Title, isGroup, uid, pIDs)
			if err != nil {
				if errors.Is(err, store.ErrInvalidDirectParticipants) {
					http.Error(w, "invalid direct conversation participants (choose exactly one user for a direct chat)", http.StatusBadRequest)
					return
				}
				if errors.Is(err, store.ErrDirectConversationsExists) {
					http.Error(w, "direct conversation between these users already exists", http.StatusConflict)
					return
				}
				log.Printf("create conversation error: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			go func() {
				// publishing a conversation-level event for any local subscribers
				payloadConv := map[string]interface{}{
					"type":         "conversation_created",
					"conversation": conv,
				}
				// publish to conversation channel (existing)
				if err := hub.PublishEvent(conv.ID.String(), payloadConv); err != nil {
					log.Printf("publish conversation_created (conversation channel) error: %v", err)
				}

				// build recipients (creator + explicit participants, no duplicates)
				recipients := make([]uuid.UUID, 0, len(pIDs)+1)
				seenRecip := map[uuid.UUID]struct{}{}
				seenRecip[uid] = struct{}{}
				recipients = append(recipients, uid) // include creator
				// per user notifications
				for _, p := range pIDs {
					// avoid duplicates
					if p == uid {
						continue
					}
					if _, ok := seenRecip[p]; ok {
						continue
					}
					seenRecip[p] = struct{}{}
					recipients = append(recipients, p)
				}

				// gather user IDs to query for display names (single DB call)
				userIDs := make([]uuid.UUID, 0, len(recipients)+len(pIDs))
				userSet := map[uuid.UUID]struct{}{}
				for _, u := range recipients {
					if _, ok := userSet[u]; !ok {
						userSet[u] = struct{}{}
						userIDs = append(userIDs, u)
					}
				}

				// fetch display names in one query
				nameMap := map[uuid.UUID]string{}
				if len(userIDs) > 0 {
					rows, err := pool.Query(context.Background(), `SELECT id, display_name FROM users WHERE id = ANY($1)`, userIDs)
					if err != nil {
						log.Printf("fetch display names error: %v", err)
					} else {
						defer rows.Close()
						for rows.Next() {
							var id uuid.UUID
							var dn *string
							if err := rows.Scan(&id, &dn); err != nil {
								continue
							}
							if dn != nil {
								nameMap[id] = *dn
							}
						}
					}
				}

				// notify each recipient with a user-specific view (non-blocking publish)
				for _, recip := range recipients {
					convForUser := conv // shallow copy (struct)
					if !conv.IsGroup {
						// resolve the peer if for this recipient
						var peer uuid.UUID
						if recip == uid {
							if len(pIDs) > 0 {
								peer = pIDs[0]
							}
						} else {
							peer = uid
						}
						if dn, ok := nameMap[peer]; ok {
							convForUser.DisplayName = &dn
						}
					}

					payloadUser := map[string]interface{}{
						"type":         "conversation_created",
						"conversation": convForUser,
					}

					// publish asynchronously to avoid blocking the handler
					go func(userID string, payload map[string]interface{}) {
						if err := hub.PublishUserEvent(userID, payload); err != nil {
							log.Printf("publish conversation_created (user %s) error: %v", userID, err)
						}
					}(recip.String(), payloadUser)
				}
			}()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(conv)
			return

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
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

		case http.MethodPatch:
			var req struct {
				ID   string `json:"id"`
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			mid, err := uuid.Parse(req.ID)
			if err != nil {
				http.Error(w, "invalid message id", http.StatusBadRequest)
				return
			}
			uidStr := backend.GetUserIDFromCtx(r.Context())
			authorID, err := uuid.Parse(uidStr)
			if err != nil {
				http.Error(w, "invalid user", http.StatusUnauthorized)
				return
			}

			updated, err := store.UpdateMessage(r.Context(), pool, mid, authorID, req.Body)
			if err != nil {
				if err == pgx.ErrNoRows {
					http.Error(w, "forbidden or not found", http.StatusForbidden)
					return
				}
				log.Printf("update message error: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// publish update to conversation channel
			payload := map[string]interface{}{"type": "message_updated", "message": updated}
			if err := hub.PublishMessage(updated.ConversationID.String(), payload); err != nil {
				log.Printf("publish message_updated error: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(updated)
			return

		case http.MethodDelete:
			var req struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			mid, err := uuid.Parse(req.ID)
			if err != nil {
				http.Error(w, "invalid message id", http.StatusBadRequest)
				return
			}
			uidStr := backend.GetUserIDFromCtx(r.Context())
			actorID, err := uuid.Parse(uidStr)
			if err != nil {
				http.Error(w, "invalid user", http.StatusUnauthorized)
				return
			}

			convID, err := store.DeleteMessage(r.Context(), pool, mid, actorID)
			if err != nil {
				if err == pgx.ErrNoRows {
					http.Error(w, "forbidden or not found", http.StatusForbidden)
					return
				}
				log.Printf("delete message error: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// notify conversation subscribers
			payload := map[string]interface{}{"type": "message_deleted", "id": mid.String()}
			if err := hub.PublishMessage(convID.String(), payload); err != nil {
				log.Printf("publish message_deleted error: %v", err)
			}

			w.WriteHeader(http.StatusNoContent)
			return

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

// registerConversationInfoHandlers handles the conversation info requests
func registerConversationInfoHandlers(mux *http.ServeMux, pool *pgxpool.Pool, hub *ws.Hub) {
	mux.Handle("/api/conversations/", backend.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/conversations/")
		if path == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var idStr, rest string
		if i := strings.Index(path, "/"); i >= 0 {
			idStr = path[:i]
			rest = strings.TrimPrefix(path[i+1:], "/")
		} else {
			idStr = path
			rest = ""
		}
		convID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		switch {
		// GET  /api/conversations/<id>/info
		case rest == "info" && r.Method == http.MethodGet:
			ci, err := store.GetConversationInfo(r.Context(), pool, convID)
			if err != nil {
				if err == pgx.ErrNoRows {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				log.Printf("get conv info: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ci)
			return

		// GET  /api/conversations/<id>/participants?limit=&offset=
		case strings.HasPrefix(rest, "participants") && r.Method == http.MethodGet:
			q := r.URL.Query()
			lim, _ := strconv.Atoi(q.Get("limit"))
			if lim <= 0 {
				lim = 50
			}
			off, _ := strconv.Atoi(q.Get("offset"))
			parts, err := store.GetParticipants(r.Context(), pool, convID, lim, off)
			if err != nil {
				log.Printf("get participants: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(parts)
			return

		// POST /api/conversations/<id>/participants    (body: { user_id, role })  (admin)
		case strings.HasPrefix(rest, "participants") && r.Method == http.MethodPost:
			var req struct {
				UserID string `json:"user_id"`
				Role   string `json:"role"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			newUID, err := uuid.Parse(req.UserID)
			if err != nil {
				http.Error(w, "invalid user id", http.StatusBadRequest)
				return
			}
			actorStr := backend.GetUserIDFromCtx(r.Context())
			actorID, _ := uuid.Parse(actorStr)
			isAdmin, err := store.IsUserAdmin(r.Context(), pool, convID, actorID)
			if err != nil || !isAdmin {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if req.Role == "" {
				req.Role = "member"
			}
			if err := store.AddParticipant(r.Context(), pool, convID, newUID, req.Role); err != nil {
				log.Printf("add participant: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			// publish participant_added event
			payload := map[string]interface{}{"type": "participant_added", "conversation_id": convID.String(), "user_id": newUID.String(), "role": req.Role}
			if hub != nil {
				_ = hub.PublishEvent(convID.String(), payload)
			}
			w.WriteHeader(http.StatusNoContent)
			return

		// DELETE /api/conversations/<id>/participants/<user_id>  (admin or self)
		case strings.HasPrefix(rest, "participants/") && r.Method == http.MethodDelete:
			// expected path here "participants/<user_id>"
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) < 2 {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			targetID, err := uuid.Parse(parts[1])
			if err != nil {
				http.Error(w, "invalid user id", http.StatusBadRequest)
				return
			}
			actorStr := backend.GetUserIDFromCtx(r.Context())
			actorID, _ := uuid.Parse(actorStr)
			if actorID != targetID {
				isAdmin, err := store.IsUserAdmin(r.Context(), pool, convID, actorID)
				if err != nil || !isAdmin {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
			if err := store.RemoveParticipant(r.Context(), pool, convID, targetID); err != nil {
				log.Printf("remove participant: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			payload := map[string]interface{}{"type": "participant_removed", "conversation_id": convID.String(), "user_id": targetID.String()}
			if hub != nil {
				_ = hub.PublishEvent(convID.String(), payload)
			}
			w.WriteHeader(http.StatusNoContent)
			return

		// PATCH  /api/conversations/<id>  (body: { title?, description? }) (admin)
		case rest == "" && r.Method == http.MethodPatch:
			var req struct {
				Title       *string `json:"title"`
				Description *string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			actorStr := backend.GetUserIDFromCtx(r.Context())
			actorID, _ := uuid.Parse(actorStr)
			isAdmin, err := store.IsUserAdmin(r.Context(), pool, convID, actorID)
			if err != nil || !isAdmin {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			updated, err := store.UpdateConversationMeta(r.Context(), pool, convID, req.Title, req.Description)
			if err != nil {
				log.Printf("update conv meta: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			payload := map[string]interface{}{"type": "conversation_updated", "conversation": updated}
			if hub != nil {
				_ = hub.PublishEvent(convID.String(), payload)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(updated)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	})))
}
