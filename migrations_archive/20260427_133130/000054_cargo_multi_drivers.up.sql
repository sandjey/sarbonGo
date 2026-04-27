-- Multi-vehicle cargo support:
-- - vehicles_left tracks remaining available vehicle slots
-- - cargo_drivers tracks which drivers are attached to cargo

ALTER TABLE cargo ADD COLUMN IF NOT EXISTS vehicles_left INTEGER NULL;

UPDATE cargo
SET vehicles_left = GREATEST(COALESCE(vehicles_amount, 1), 1)
WHERE vehicles_left IS NULL;

ALTER TABLE cargo ALTER COLUMN vehicles_left SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cargo_vehicles_left ON cargo (vehicles_left);

CREATE TABLE IF NOT EXISTS cargo_drivers (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  cargo_id UUID NOT NULL REFERENCES cargo(id) ON DELETE CASCADE,
  driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
  status VARCHAR NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  UNIQUE (cargo_id, driver_id),
  CONSTRAINT cargo_drivers_status_chk CHECK (status IN ('ACTIVE', 'COMPLETED', 'CANCELLED', 'REMOVED'))
);

-- A driver can be active on only one cargo at a time.
CREATE UNIQUE INDEX IF NOT EXISTS ux_cargo_drivers_driver_active
ON cargo_drivers (driver_id)
WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_cargo_drivers_cargo ON cargo_drivers (cargo_id);
CREATE INDEX IF NOT EXISTS idx_cargo_drivers_driver ON cargo_drivers (driver_id);
