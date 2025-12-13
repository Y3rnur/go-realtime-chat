package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Conversation struct {
	ID        uuid.UUID `json:"id"`
	Title     *string   `json:"title,omitempty"`
	IsGroup   bool      `json:"is_group"`
	CreatedAt time.Time `json:"created_at"`
}

type Message struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	AuthorID       uuid.UUID `json:"author_id"`
	Body           *string   `json:"body,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// GetConversationsForUser returns conversations the user participates in.
func GetConversationsForUser(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := pool.Query(ctx, `
	SELECT c.id, c.title, c.is_group, c.created_at
	FROM conversation_participants cp
	JOIN conversations c ON c.id = cp.conversation_id
	WHERE cp.user_id = $1
	ORDER BY c.created_at DESC
	LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.IsGroup, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetMessagesForConversation returns recent messages for a conversation (newest first).
func GetMessagesForConversation(ctx context.Context, pool *pgxpool.Pool, convID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := pool.Query(ctx, `
	SELECT id, conversation_id, author_id, body, created_at
	FROM messages
	WHERE conversation_id = $1
	ORDER BY created_at DESC
	LIMIT $2`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
