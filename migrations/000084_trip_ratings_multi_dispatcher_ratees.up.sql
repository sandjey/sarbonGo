DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'trip_ratings_trip_rater_unique') THEN
    ALTER TABLE trip_ratings DROP CONSTRAINT trip_ratings_trip_rater_unique;
  END IF;
END$$;

ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_trip_rater_unique
  UNIQUE (trip_id, rater_kind, ratee_kind, ratee_id);
