DROP INDEX IF EXISTS idx_route_points_country_code;
ALTER TABLE route_points DROP COLUMN IF EXISTS country_code;

