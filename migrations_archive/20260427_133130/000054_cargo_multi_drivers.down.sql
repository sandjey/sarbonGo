DROP TABLE IF EXISTS cargo_drivers;
DROP INDEX IF EXISTS ux_cargo_drivers_driver_active;

ALTER TABLE cargo DROP COLUMN IF EXISTS vehicles_left;
DROP INDEX IF EXISTS idx_cargo_vehicles_left;
