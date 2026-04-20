-- Chat delivery receipts: per-message delivered_at (set once when recipient's
-- device confirms delivery — in practice when their WS reconnects or when we
-- send a new message while the recipient is already online).
--
-- NULL → message is queued (sent but not confirmed delivered).
-- NOT NULL → recipient's client has received it (single-check → double-check UX).

ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ NULL;

-- Fast lookup of undelivered messages for a recipient when they come online.
-- recipient = the OTHER participant of the conversation, not the sender.
CREATE INDEX IF NOT EXISTS idx_chat_messages_undelivered
  ON chat_messages (conversation_id)
  WHERE delivered_at IS NULL AND deleted_at IS NULL;
