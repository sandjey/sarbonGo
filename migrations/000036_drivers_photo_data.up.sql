-- Фото водителя в БД (необязательно при регистрации; можно добавить/обновить/удалить когда угодно).

ALTER TABLE drivers
  ADD COLUMN IF NOT EXISTS photo_data BYTEA NULL,
  ADD COLUMN IF NOT EXISTS photo_content_type VARCHAR(50) NULL;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = 'deleted_drivers'
  ) THEN
    ALTER TABLE deleted_drivers
      ADD COLUMN IF NOT EXISTS photo_data BYTEA NULL,
      ADD COLUMN IF NOT EXISTS photo_content_type VARCHAR(50) NULL;
  END IF;
END$$;
