DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'trip_ratings_trip_rater_unique') THEN
    ALTER TABLE trip_ratings DROP CONSTRAINT trip_ratings_trip_rater_unique;
  END IF;
END$$;

WITH ranked AS (
  SELECT
    id,
    ROW_NUMBER() OVER (
      PARTITION BY trip_id, rater_kind, ratee_kind
      ORDER BY updated_at DESC, created_at DESC, id DESC
    ) AS rn
  FROM trip_ratings
)
DELETE FROM trip_ratings tr
USING ranked r
WHERE tr.id = r.id
  AND r.rn > 1;

ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_trip_rater_unique
  UNIQUE (trip_id, rater_kind, ratee_kind);
