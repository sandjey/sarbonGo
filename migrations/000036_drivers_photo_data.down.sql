ALTER TABLE drivers DROP COLUMN IF EXISTS photo_data, DROP COLUMN IF EXISTS photo_content_type;
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = 'deleted_drivers'
  ) THEN
    ALTER TABLE deleted_drivers DROP COLUMN IF EXISTS photo_data, DROP COLUMN IF EXISTS photo_content_type;
  END IF;
END$$;
