-- Telegram-style pre-upload cache.
--
-- The idea: the client hashes the ORIGINAL bytes of a media file it wants to
-- send (SHA-256). If the server already processed that exact source before
-- (possibly by a different user), we can skip re-upload AND re-encoding and
-- reuse the existing dedupped media_file. This saves:
--   * network: client doesn't upload the body
--   * CPU: ffmpeg doesn't run
--   * disk: media_files already stores one copy
--
-- chat_source_hashes stores the mapping:
--   source SHA-256 (input bytes)  →  media_file_id (processed bytes)
-- plus all the metadata we'd otherwise recompute from ffprobe.

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
