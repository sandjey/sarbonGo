-- Extend offer/ratings role matrix for driver-manager flow.

ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_proposed_by_check;
ALTER TABLE offers
  ADD CONSTRAINT offers_proposed_by_check
  CHECK (proposed_by IN ('DRIVER', 'DISPATCHER', 'DRIVER_MANAGER'));

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'trip_ratings_trip_rater_unique') THEN
    ALTER TABLE trip_ratings DROP CONSTRAINT trip_ratings_trip_rater_unique;
  END IF;
END$$;

ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_trip_rater_unique
  UNIQUE (trip_id, rater_kind, ratee_kind);

ALTER TABLE trip_ratings DROP CONSTRAINT IF EXISTS trip_ratings_rater_kind_check;
ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_rater_kind_check
  CHECK (rater_kind IN ('driver', 'dispatcher', 'driver_manager'));
