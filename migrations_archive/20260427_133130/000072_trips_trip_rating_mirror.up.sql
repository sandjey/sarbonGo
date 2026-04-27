-- Denormalize per-trip ratings onto trips for API (driver→manager vs manager→driver), kept in sync from trip_ratings.

ALTER TABLE trips
  ADD COLUMN IF NOT EXISTS rating_from_driver NUMERIC(3, 1) NULL,
  ADD COLUMN IF NOT EXISTS rating_from_dispatcher NUMERIC(3, 1) NULL;

UPDATE trips t
SET rating_from_driver = tr.stars
FROM trip_ratings tr
WHERE tr.trip_id = t.id AND tr.rater_kind = 'driver';

UPDATE trips t
SET rating_from_dispatcher = tr.stars
FROM trip_ratings tr
WHERE tr.trip_id = t.id AND tr.rater_kind = 'dispatcher';
