ALTER TABLE trips
  DROP COLUMN IF EXISTS rating_from_driver,
  DROP COLUMN IF EXISTS rating_from_dispatcher;
