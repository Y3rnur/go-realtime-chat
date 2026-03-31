DROP TRIGGER IF EXISTS trg_ensure_admin_after_del_up ON conversation_participants;
DROP TRIGGER IF EXISTS trg_creator_admin_before_ins_up ON conversation_participants;

DROP FUNCTION IF EXISTS ensure_admin_after_participant_removal();
DROP FUNCTION IF EXISTS ensure_creator_admin_on_participant_insert();