-- Revert pre-upload source hash cache.

DROP INDEX IF EXISTS idx_chat_source_hashes_thumb;
DROP INDEX IF EXISTS idx_chat_source_hashes_media;
DROP TABLE IF EXISTS chat_source_hashes;
