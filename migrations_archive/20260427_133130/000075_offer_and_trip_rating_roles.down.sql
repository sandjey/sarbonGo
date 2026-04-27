ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_proposed_by_check;
ALTER TABLE offers
  ADD CONSTRAINT offers_proposed_by_check
  CHECK (proposed_by IN ('DRIVER', 'DISPATCHER'));

ALTER TABLE trip_ratings DROP CONSTRAINT IF EXISTS trip_ratings_trip_rater_unique;
ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_trip_rater_unique
  UNIQUE (trip_id, rater_kind);

ALTER TABLE trip_ratings DROP CONSTRAINT IF EXISTS trip_ratings_rater_kind_check;
ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_rater_kind_check
  CHECK (rater_kind IN ('driver', 'dispatcher'));
