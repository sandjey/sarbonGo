ALTER TABLE route_points ADD COLUMN IF NOT EXISTS country_code VARCHAR(3) NULL;
CREATE INDEX IF NOT EXISTS idx_route_points_country_code ON route_points (country_code);

-- Best-effort backfill from cities by city_code
UPDATE route_points rp
SET country_code = c.country_code
FROM cities c
WHERE rp.country_code IS NULL
  AND rp.city_code IS NOT NULL
  AND c.code = rp.city_code;

