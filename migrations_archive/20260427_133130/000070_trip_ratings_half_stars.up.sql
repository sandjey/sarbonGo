-- Trip ratings: half-star scale 1.0–5.0 on profile; trip_id remains the audit row for which completed trip was rated.

ALTER TABLE trip_ratings DROP CONSTRAINT IF EXISTS trip_ratings_stars_check;

ALTER TABLE trip_ratings
  ALTER COLUMN stars TYPE DOUBLE PRECISION
  USING (
    LEAST(
      5::double precision,
      GREATEST(
        1::double precision,
        round(stars::numeric * 0.5, 1)::double precision
      )
    )
  );

ALTER TABLE trip_ratings
  ADD CONSTRAINT trip_ratings_stars_range CHECK (stars >= 1 AND stars <= 5);
