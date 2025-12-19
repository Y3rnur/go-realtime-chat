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

	// author info for convenience
	AuthorName   *string `json:"author_name,omitempty"`
	AuthorAvatar *string `json:"author_avatar,omitempty"`
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

// GetMessagesForConversation returns recent messages for a conversation (oldest first).
func GetMessagesForConversation(ctx context.Context, pool *pgxpool.Pool, convID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := pool.Query(ctx, `
	SELECT m.id, m.conversation_id, m.author_id, m.body, m.created_at, u.display_name, u.avatar_url
	FROM (
		SELECT * FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	) m
	LEFT JOIN users u ON u.id = m.author_id
	ORDER BY m.created_at ASC
	`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.Body, &m.CreatedAt, &m.AuthorName, &m.AuthorAvatar); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsUserInConversation returns true when the given user is a participant of the conversation.
func IsUserInConversation(ctx context.Context, pool *pgxpool.Pool, convID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM conversation_participants
			WHERE conversation_id = $1 AND user_id = $2
		)`, convID, userID).Scan(&exists)
	return exists, err
}

// SaveMessage inserts a new message and returns the saved row
func SaveMessage(ctx context.Context, pool *pgxpool.Pool, convID, authorID uuid.UUID, body string) (Message, error) {
	var m Message

	err := pool.QueryRow(ctx, `
	INSERT INTO messages (id, conversation_id, author_id, body, message_type, created_at)
	VALUES (gen_random_uuid(), $1, $2, $3, 'text', now())
	RETURNING id, conversation_id, author_id, body, created_at
	`, convID, authorID, body).Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.Body, &m.CreatedAt)
	if err != nil {
		return m, err
	}

	// fetching author display_name and avatar_url
	_ = pool.QueryRow(ctx, `SELECT display_name, avatar_url FROM users WHERE id = $1`, m.AuthorID).Scan(&m.AuthorName, &m.AuthorAvatar)

	return m, nil
}
