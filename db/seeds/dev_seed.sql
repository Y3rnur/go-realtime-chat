BEGIN;

-- Adding sample users
INSERT INTO users (id, email, phone, password_hash, first_name, last_name, display_name, avatar_url, is_verified, created_at)
VALUES 
    (gen_random_uuid(), 'alice@example.test', '+10000000001', 'bcrypt:$2y$10$placeholderalice', 'Alice', 'Anderson', 'Alice', 'https://i.pravatar.cc/150?u=alice', TRUE, now())
ON CONFLICT (email) DO NOTHING;

INSERT INTO users (id, email, phone, password_hash, first_name, last_name, display_name, avatar_url, is_verified, created_at)
VALUES
    (gen_random_uuid(), 'bob@example.test', '+10000000002', 'bcrypt:$2y$10$placeholderbob', 'Bob', 'Brown', 'Bob', 'https://i.pravatar.cc/150?u=bob', TRUE, now())
ON CONFLICT (email) DO NOTHING;

INSERT INTO users (id, email, phone, password_hash, first_name, last_name, display_name, avatar_url, is_verified, created_at)
VALUES
    (gen_random_uuid(), 'charlie@example.test', '+10000000003', 'bcrypt:$2y$10$placeholdercharlie', 'Charlie', 'Clark', 'Charlie', 'https://i.pravatar.cc/150?u=charlie', TRUE, now())
ON CONFLICT (email) DO NOTHING;

-- Sample conversation and messages
WITH conv AS (
    INSERT INTO conversations (id, is_group, title, created_by, created_at)
    VALUES (gen_random_uuid(), FALSE, NULL, (SELECT id FROM users WHERE email = 'alice@example.test'), now())
    ON CONFLICT DO NOTHING
    RETURNING id
),
conv_sel AS (
    SELECT id FROM conv
    UNION
    SELECT id FROM conversations WHERE is_group = FALSE AND created_by = (SELECT id FROM users WHERE email = 'alice@example.test') LIMIT 1
)

-- Adding users as participants into a conversation
INSERT INTO conversation_participants (conversation_id, user_id, joined_at)
SELECT cs.id, u.id, now() - (INTERVAL '2 hours')
FROM conv_sel cs
CROSS JOIN users u
WHERE u.email IN ('alice@example.test', 'bob@example.test')
ON CONFLICT (conversation_id, user_id) DO NOTHING;

-- Sending some messages
WITH target AS (
    SELECT c.id AS cid
    FROM conversations c
    JOIN users u ON u.email = 'alice@example.test'
    WHERE c.is_group = FALSE AND c.created_by = u.id
    LIMIT 1
)
INSERT INTO messages (id, conversation_id, author_id, body, message_type, created_at)
SELECT gen_random_uuid(), t.cid, u.id,
    CASE u.email
        WHEN 'alice@example.test' THEN 'Hey Bob - this is Alice. Testing the chat seed!'
        WHEN 'bob@example.test' THEN 'Hi Alice - looks good. I see your message.'
    END,
    'text',
    now() - (INTERVAL '90 minutes')
FROM target t
JOIN users u ON u.email IN ('alice@example.test', 'bob@example.test')
ON CONFLICT DO NOTHING;

-- Creating group conversation 'Team'
WITH g AS (
    INSERT INTO conversations (id, is_group, title, created_by, created_at)
    VALUES (gen_random_uuid(), TRUE, 'Team', (SELECT id FROM users WHERE email = 'alice@example.test'), now())
    ON CONFLICT DO NOTHING
    RETURNING id
),
g_sel AS (
    SELECT id FROM g
    UNION
    SELECT id FROM conversations WHERE is_group = TRUE AND title = 'Team' LIMIT 1
)
INSERT INTO conversation_participants (conversation_id, user_id, joined_at)
SELECT gs.id, u.id, now() - (INTERVAL '1 day')
FROM g_sel gs
CROSS JOIN users u
WHERE u.email IN ('alice@example.test', 'bob@example.test', 'charlie@example.test')
ON CONFLICT (conversation_id, user_id) DO NOTHING;

-- Sending sample messages to 'Team' conversation
WITH target_group AS (
    SELECT id FROM conversations WHERE is_group = TRUE AND title = 'Team' LIMIT 1
)
INSERT INTO messages (id, conversation_id, author_id, body, message_type, created_at)
SELECT gen_random_uuid(), tg.id, u.id,
    CASE u.email
        WHEN 'alice@example.test' THEN 'Morning team - meeting in 10.'
        WHEN 'bob@example.test' THEN 'I can make it, will join from home.'
        WHEN 'charlie@example.test' THEN 'On my way, finishing a quick task.'
    END,
    'text',
    now() - (INTERVAL '23 hours')
FROM target_group tg
JOIN users u ON u.email IN ('alice@example.test', 'bob@example.test', 'charlie@example.test')
ON CONFLICT DO NOTHING;

COMMIT;