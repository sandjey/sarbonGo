-- CM -> Driver Manager offer requests (CM selects cargo + DM; DM selects driver on accept)

CREATE TABLE IF NOT EXISTS cargo_manager_dm_offers (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  cargo_id UUID NOT NULL REFERENCES cargo(id) ON DELETE CASCADE,
  cargo_manager_id UUID NOT NULL REFERENCES freelance_dispatchers(id) ON DELETE CASCADE,
  driver_manager_id UUID NOT NULL REFERENCES freelance_dispatchers(id) ON DELETE CASCADE,
  driver_id UUID NULL REFERENCES drivers(id) ON DELETE SET NULL,
  offer_id UUID NULL REFERENCES offers(id) ON DELETE SET NULL,
  price DOUBLE PRECISION NOT NULL,
  currency VARCHAR NOT NULL,
  comment VARCHAR NULL,
  status VARCHAR NOT NULL DEFAULT 'PENDING',
  rejection_reason TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cm_dm_offers_dm_id ON cargo_manager_dm_offers (driver_manager_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cm_dm_offers_cm_id ON cargo_manager_dm_offers (cargo_manager_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cm_dm_offers_cargo_id ON cargo_manager_dm_offers (cargo_id, status, created_at DESC);

ALTER TABLE cargo_manager_dm_offers DROP CONSTRAINT IF EXISTS cargo_manager_dm_offers_status_check;
ALTER TABLE cargo_manager_dm_offers
  ADD CONSTRAINT cargo_manager_dm_offers_status_check
  CHECK (status IN ('PENDING', 'ACCEPTED', 'REJECTED', 'CANCELED'));

