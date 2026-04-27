-- Cargo: require transport plate types on create (API-level) and store them in DB.
-- Also store vehicles_amount = how many vehicles are required for this cargo.

ALTER TABLE cargo ADD COLUMN IF NOT EXISTS power_plate_type VARCHAR NULL;
ALTER TABLE cargo ADD COLUMN IF NOT EXISTS trailer_plate_type VARCHAR NULL;
ALTER TABLE cargo ADD COLUMN IF NOT EXISTS vehicles_amount INTEGER NULL;

CREATE INDEX IF NOT EXISTS idx_cargo_power_plate_type ON cargo (power_plate_type);
CREATE INDEX IF NOT EXISTS idx_cargo_trailer_plate_type ON cargo (trailer_plate_type);
CREATE INDEX IF NOT EXISTS idx_cargo_vehicles_amount ON cargo (vehicles_amount);

