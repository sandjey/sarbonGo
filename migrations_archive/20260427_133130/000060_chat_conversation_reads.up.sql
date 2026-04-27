-- Per-user read timestamps for chat (Telegram-like unread counts and read receipts).

CREATE TABLE IF NOT EXISTS chat_conversation_reads (
  conversation_id UUID NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL,
  last_read_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_chat_conversation_reads_user ON chat_conversation_reads (user_id);
