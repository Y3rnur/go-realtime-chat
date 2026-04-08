-- Ensure the conversation creator is always 'admin' and promote a new admin if none remain (early joined first).

-- Auto-assign 'admin' to the conversation creator
CREATE OR REPLACE FUNCTION ensure_creator_admin_on_participant_insert() RETURNS trigger AS $$
DECLARE
    creator uuid;
BEGIN
    IF (TG_OP = 'INSERT') OR (TG_OP = 'UPDATE' AND NEW.user_id <> OLD.user_id) THEN
        IF EXISTS (
            SELECT 1 FROM conversations
            WHERE id = NEW.conversation_id
                AND created_by = NEW.user_id
                AND is_group = TRUE
        ) THEN
            NEW.role := 'admin';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Ensure at least one admin exists in group chats
CREATE OR REPLACE FUNCTION ensure_admin_after_participant_removal() RETURNS trigger AS $$
BEGIN
    IF (TG_OP = 'DELETE' AND OLD.role = 'admin') OR
        (TG_OP = 'UPDATE' AND OLD.role = 'admin' AND NEW.role <> 'admin') THEN

        DECLARE
            target_id uuid := COALESCE(NEW.conversation_id, OLD.conversation_id);
        BEGIN
            UPDATE conversation_participants
            SET role = 'admin'
            WHERE (conversation_id, user_id) = (
                SELECT cp.conversation_id, cp.user_id
                FROM conversation_participants cp
                JOIN conversations c ON cp.conversation_id = c.id
                WHERE cp.conversation_id = target_id
                AND c.is_group = TRUE
                AND NOT EXISTS (
                    SELECT 1 FROM conversation_participants
                    WHERE conversation_id = target_id AND role = 'admin'
                )
                ORDER BY cp.joined_at ASC
                LIMIT 1
            )
            AND role <> 'admin';
        END;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Trigger 1: Before Insert/Update (for first function above)
DROP TRIGGER IF EXISTS trg_creator_admin_before_ins_up ON conversation_participants;
CREATE TRIGGER trg_creator_admin_before_ins_up
BEFORE INSERT OR UPDATE ON conversation_participants
FOR EACH ROW EXECUTE FUNCTION ensure_creator_admin_on_participant_insert();

-- Trigger 2: After Delete/Update of role (for second function above)
DROP TRIGGER IF EXISTS trg_ensure_admin_after_del_up ON conversation_participants;
CREATE TRIGGER trg_ensure_admin_after_del_up
AFTER DELETE OR UPDATE OF role ON conversation_participants
FOR EACH ROW EXECUTE FUNCTION ensure_admin_after_participant_removal();