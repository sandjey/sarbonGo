-- Revert delivery receipts.

DROP INDEX IF EXISTS idx_chat_messages_undelivered;
ALTER TABLE chat_messages DROP COLUMN IF EXISTS delivered_at;
