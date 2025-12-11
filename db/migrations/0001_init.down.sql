DROP INDEX IF EXISTS idx_messages_conversation_created_at;
DROP INDEX IF EXISTS idx_participants_user_id;

DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversation_participants;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS users;