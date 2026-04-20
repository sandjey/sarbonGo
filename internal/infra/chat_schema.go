package infra

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureChatTables(ctx context.Context, pg *pgxpool.Pool) error {
	_, err := pg.Exec(ctx, `
CREATE TABLE IF NOT EXISTS chat_conversations (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_a_id UUID NOT NULL,
  user_b_id UUID NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  CONSTRAINT chat_conv_ordered CHECK (user_a_id < user_b_id),
  CONSTRAINT chat_conv_unique UNIQUE (user_a_id, user_b_id)
);
CREATE INDEX IF NOT EXISTS idx_chat_conversations_user_a ON chat_conversations (user_a_id);
CREATE INDEX IF NOT EXISTS idx_chat_conversations_user_b ON chat_conversations (user_b_id);

CREATE TABLE IF NOT EXISTS chat_messages (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  conversation_id UUID NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
  sender_id UUID NOT NULL,
  body TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  deleted_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_conversation_created ON chat_messages (conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_messages_sender ON chat_messages (sender_id);
CREATE INDEX IF NOT EXISTS idx_chat_messages_deleted ON chat_messages (deleted_at) WHERE deleted_at IS NULL;
`)
	if err != nil {
		return err
	}

	// Delivery receipts (see migration 000082): delivered_at is set once the peer's
	// WS client acknowledges reception. NULL while queued.
	_, err = pg.Exec(ctx, `
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_chat_messages_undelivered
  ON chat_messages (conversation_id)
  WHERE delivered_at IS NULL AND deleted_at IS NULL;
`)
	if err != nil {
		return err
	}

	// Pre-upload SHA-256 source cache (see migration 000083). Lets the client
	// probe by source hash before uploading and lets the server skip ffmpeg
	// entirely when the same source was processed before.
	_, err = pg.Exec(ctx, `
CREATE TABLE IF NOT EXISTS chat_source_hashes (
  source_hash         TEXT PRIMARY KEY,
  media_file_id       UUID NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
  thumb_media_file_id UUID NULL     REFERENCES media_files(id) ON DELETE SET NULL,
  kind                TEXT NOT NULL,
  mime                TEXT NOT NULL,
  size_bytes          BIGINT NOT NULL,
  duration_ms         INTEGER NULL,
  width               INTEGER NULL,
  height              INTEGER NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_chat_source_hashes_media ON chat_source_hashes (media_file_id);
CREATE INDEX IF NOT EXISTS idx_chat_source_hashes_thumb ON chat_source_hashes (thumb_media_file_id);
`)
	return err
}
