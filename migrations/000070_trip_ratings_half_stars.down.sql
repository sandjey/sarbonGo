ALTER TABLE trip_ratings DROP CONSTRAINT IF EXISTS trip_ratings_stars_range;

ALTER TABLE trip_ratings
  ALTER COLUMN stars TYPE SMALLINT
  USING (round(stars)::smallint);

ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_stars_check CHECK (stars >= 1 AND stars <= 10);
