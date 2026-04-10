package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// validation errors
var (
	ErrDirectConversationsExists = fmt.Errorf("direct conversation already exists")
	ErrInvalidDirectParticipants = fmt.Errorf("invalid direct participants")
)

type Conversation struct {
	ID          uuid.UUID `json:"id"`
	Title       *string   `json:"title,omitempty"`
	IsGroup     bool      `json:"is_group"`
	CreatedAt   time.Time `json:"created_at"`
	DisplayName *string   `json:"display_name,omitempty"`
	AvatarURL   *string   `json:"avatar_url,omitempty"`
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

	// edit / delete metadata
	EditedAt  *time.Time `json:"edited_at,omitempty"`
	Deleted   bool       `json:"deleted,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type User struct {
	ID          uuid.UUID `json:"id"`
	DisplayName *string   `json:"display_name,omitempty"`
	AvatarURL   *string   `json:"avatar_url,omitempty"`
	Email       *string   `json:"email,omitempty"`
}

type ConversationInfo struct {
	ID               uuid.UUID `json:"id"`
	Title            *string   `json:"title,omitempty"`
	Description      *string   `json:"description,omitempty"`
	IsGroup          bool      `json:"is_group"`
	ParticipantCount int       `json:"participant_count"`
}

type Participant struct {
	UserID      uuid.UUID `json:"user_id"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
	DisplayName *string   `json:"display_name,omitempty"`
	AvatarURL   *string   `json:"avatar_url,omitempty"`
}

// GetUserByID returns basic user profile.
func GetUserByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `SELECT id, display_name, avatar_url, email FROM users WHERE id = $1`, id).Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.Email)
	return u, err
}

// GetConversationInfo returns metadata and participant count for a conversation.
func GetConversationInfo(ctx context.Context, pool *pgxpool.Pool, convID uuid.UUID) (ConversationInfo, error) {
	var ci ConversationInfo
	err := pool.QueryRow(ctx, `
		SELECT c.id, c.title, c.description, c.is_group,
			(SELECT COUNT(1) FROM conversation_participants cp WHERE cp.conversation_id = c.id) AS participant_count
		FROM conversations c
		WHERE c.id = $1
	`, convID).Scan(&ci.ID, &ci.Title, &ci.Description, &ci.IsGroup, &ci.ParticipantCount)
	return ci, err
}

// GetParticipants returns paged participants with light user info.
func GetParticipants(ctx context.Context, pool *pgxpool.Pool, convID uuid.UUID, limit, offset int) ([]Participant, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := pool.Query(ctx, `
		SELECT cp.user_id, cp.role, cp.joined_at, u.display_name, u.avatar_url
		FROM conversation_participants cp
		JOIN users u ON u.id = cp.user_id
		WHERE cp.conversation_id = $1
		ORDER BY cp.joined_at ASC
		LIMIT $2 OFFSET $3
	`, convID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Participant
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.UserID, &p.Role, &p.JoinedAt, &p.DisplayName, &p.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// IsUserAdmin checks whether a participant has admin role.
func IsUserAdmin(ctx context.Context, pool *pgxpool.Pool, convID, userID uuid.UUID) (bool, error) {
	var role string
	err := pool.QueryRow(ctx, `
		SELECT role FROM conversation_participants
		WHERE conversation_id = $1 AND user_id = $2
	`, convID, userID).Scan(&role)
	if err != nil {
		return false, err
	}
	return role == "admin" || role == "creator", nil
}

// AddParticipant inserts a user into conversation_participants.
func AddParticipant(ctx context.Context, pool *pgxpool.Pool, convID, userID uuid.UUID, role string) error {
	if role == "" {
		role = "member"
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO conversation_participants (conversation_id, user_id, role, joined_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (conversation_id, user_id) DO NOTHING
	`, convID, userID, role)
	return err
}

// RemoveParticipant removes a user from a conversation.
func RemoveParticipant(ctx context.Context, pool *pgxpool.Pool, convID, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM conversation_participants WHERE conversation_id = $1 AND user_id = $2
	`, convID, userID)
	return err
}

// UpdateConversationMeta updates title/description and returns updated info.
func UpdateConversationMeta(ctx context.Context, pool *pgxpool.Pool, convID uuid.UUID, title *string, description *string) (ConversationInfo, error) {
	var ci ConversationInfo
	_, err := pool.Exec(ctx, `
		UPDATE conversations
		SET title = COALESCE($2, title), description = COALESCE($3, description)
		WHERE id = $1
	`, convID, title, description)
	if err != nil {
		return ci, err
	}
	return GetConversationInfo(ctx, pool, convID)
}

// BlockUser inserts into user_blocks.
func BlockUser(ctx context.Context, pool *pgxpool.Pool, blocker, blocked uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO user_blocks (blocker_id, blocked_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, blocker, blocked)
	return err
}

// SearchUsersByDisplayName does a simple ILIKE lookup for display names.
func SearchUsersByDisplayName(ctx context.Context, pool *pgxpool.Pool, q string, limit int) ([]User, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		SELECT id, display_name, email
		FROM users
		WHERE display_name ILIKE '%' || $1 || '%'
		ORDER BY display_name
		LIMIT $2
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetConversationsForUser returns conversations the user participates in.
func GetConversationsForUser(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := pool.Query(ctx, `
	SELECT c.id, c.title, c.is_group, c.created_at,
		CASE
			WHEN c.is_group THEN NULL
			ELSE (
				SELECT u.display_name
				FROM conversation_participants cp2
				JOIN users u ON u.id = cp2.user_id
				WHERE cp2.conversation_id = c.id AND cp2.user_id <> $1
				LIMIT 1
			)
		END AS display_name,
		CASE
			WHEN c.is_group THEN c.avatar_url
			ELSE (
				SELECT u.avatar_url
				FROM conversation_participants cp2
				JOIN users u ON u.id = cp2.user_id
				WHERE cp2.conversation_id = c.id AND cp2.user_id <> $1
				LIMIT 1
			)
		END AS avatar_url
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
		if err := rows.Scan(&c.ID, &c.Title, &c.IsGroup, &c.CreatedAt, &c.DisplayName, &c.AvatarURL); err != nil {
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
	SELECT m.id, m.conversation_id, m.author_id, m.body, m.created_at, m.edited_at, m.is_deleted, u.display_name, u.avatar_url
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
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.Body, &m.CreatedAt, &m.EditedAt, &m.Deleted, &m.AuthorName, &m.AuthorAvatar); err != nil {
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

func UpdateMessage(ctx context.Context, pool *pgxpool.Pool, msgID, authorID uuid.UUID, newBody string) (Message, error) {
	var m Message

	err := pool.QueryRow(ctx, `
		UPDATE messages
		SET body = $1, edited_at = now()
		WHERE id = $2 AND author_id = $3 AND is_deleted = FALSE
		RETURNING id, conversation_id, author_id, body, created_at, edited_at, is_deleted, deleted_at
	`, newBody, msgID, authorID).Scan(
		&m.ID, &m.ConversationID, &m.AuthorID, &m.Body, &m.CreatedAt, &m.EditedAt, &m.Deleted, &m.DeletedAt,
	)

	if err != nil {
		return m, err
	}
	_ = pool.QueryRow(ctx, `SELECT display_name, avatar_url FROM users WHERE id = $1`, m.AuthorID).Scan(&m.AuthorName, &m.AuthorAvatar)
	return m, nil
}

func DeleteMessage(ctx context.Context, pool *pgxpool.Pool, msgID, actorID uuid.UUID) (uuid.UUID, error) {
	var convID uuid.UUID
	err := pool.QueryRow(ctx, `
		UPDATE messages
		SET is_deleted = TRUE, deleted_at = now(), body = NULL
		WHERE id = $1 AND author_id = $2 AND is_deleted = FALSE
		RETURNING conversation_id
	`, msgID, actorID).Scan(&convID)
	if err != nil {
		return convID, err
	}
	return convID, nil
}

// CreateConversation creates a conversation and inserts participants atomically.
func CreateConversation(ctx context.Context, pool *pgxpool.Pool, title *string, isGroup bool, creatorID uuid.UUID, participantIDs []uuid.UUID) (Conversation, error) {
	var c Conversation

	// deduping participant IDs and ensuring that creator is included
	seen := map[uuid.UUID]bool{}
	var unique []uuid.UUID
	for _, p := range participantIDs {
		if p == uuid.Nil {
			continue
		}
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	if !seen[creatorID] {
		unique = append(unique, creatorID)
		seen[creatorID] = true
	}

	// if not group, require exactly two unique participants (creator + one other)
	if !isGroup {
		if len(unique) != 2 {
			return c, ErrInvalidDirectParticipants
		}

		// finding the other user id
		var other uuid.UUID
		for _, u := range unique {
			if u != creatorID {
				other = u
				break
			}
		}
		// checking for existing non-group conversation between creator and other with exactly 2 participants
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM conversations c
				WHERE c.is_group = FALSE
					AND EXISTS(SELECT 1 FROM conversation_participants cp1 WHERE cp1.conversation_id = c.id AND cp1.user_id = $1)
					AND EXISTS(SELECT 1 FROM conversation_participants cp2 WHERE cp2.conversation_id = c.id AND cp2.user_id = $2)
					AND (SELECT COUNT(*) FROM conversation_participants cp3 WHERE cp3.conversation_id = c.id) = 2
			)
		`, creatorID, other).Scan(&exists)
		if err != nil {
			return c, err
		}
		if exists {
			return c, ErrDirectConversationsExists
		}
	}

	if len(unique) == 0 {
		// ensuring that at least creator is included
		unique = append(unique, creatorID)
	}

	var existingCount int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(1) FROM users WHERE id = ANY($1)
	`, unique).Scan(&existingCount)
	if err != nil {
		return c, err
	}
	if existingCount != len(unique) {
		return c, fmt.Errorf("one or more participant IDs are invalid")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var titleArg *string = title
	if !isGroup {
		titleArg = nil
	}

	// creating conversation row
	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (id, title, is_group, created_by, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, now())
		RETURNING id, title, is_group, created_at
	`, titleArg, isGroup, creatorID).Scan(&c.ID, &c.Title, &c.IsGroup, &c.CreatedAt)
	if err != nil {
		return c, err
	}

	// inserting participants
	for _, p := range unique {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_participants (conversation_id, user_id, joined_at, role)
			VALUES ($1, $2, now(), 'member')
			ON CONFLICT DO NOTHING
		`, c.ID, p); err != nil {
			return c, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return c, err
	}

	return c, nil
}
