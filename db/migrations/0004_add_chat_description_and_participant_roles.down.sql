ALTER TABLE conversation_participants DROP COLUMN IF EXISTS joined_at;
ALTER TABLE conversation_participants DROP COLUMN IF EXISTS role;
ALTER TABLE conversation DROP COLUMN IF EXISTS description;