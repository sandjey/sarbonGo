-- Route point: place from maps + planned datetime (UTC); cargo: extra fields from API create body

ALTER TABLE cargo ADD COLUMN IF NOT EXISTS capacity_required DOUBLE PRECISION NULL;
ALTER TABLE cargo ADD COLUMN IF NOT EXISTS packaging VARCHAR(500) NULL;
ALTER TABLE cargo ADD COLUMN IF NOT EXISTS dimensions VARCHAR(500) NULL;
ALTER TABLE cargo ADD COLUMN IF NOT EXISTS photo_urls TEXT[] NULL;

ALTER TABLE route_points ADD COLUMN IF NOT EXISTS place_id VARCHAR(255) NULL;
ALTER TABLE route_points ADD COLUMN IF NOT EXISTS point_at TIMESTAMPTZ NULL;
