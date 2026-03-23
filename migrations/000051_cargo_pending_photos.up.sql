-- Фото груза до создания записи cargo: загрузка без cargo_id, привязка при POST /api/cargo.

CREATE TABLE IF NOT EXISTS cargo_pending_photos (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  mime VARCHAR(128) NOT NULL,
  size_bytes BIGINT NOT NULL,
  path VARCHAR(1024) NOT NULL,
  uploader_id UUID NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cargo_pending_photos_created ON cargo_pending_photos (created_at DESC);
