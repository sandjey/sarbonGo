-- Favorites:
-- 1) driver <-> cargo favorites (driver favorites cargos)
-- 2) freelance_dispatcher <-> cargo favorites (freelancer favorites cargos)
-- 3) freelance_dispatcher <-> driver favorites (freelancer favorites drivers)

CREATE TABLE IF NOT EXISTS driver_cargo_favorites (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
  cargo_id UUID NOT NULL REFERENCES cargo(id) ON DELETE CASCADE,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  UNIQUE(driver_id, cargo_id)
);
CREATE INDEX IF NOT EXISTS idx_driver_cargo_favorites_driver ON driver_cargo_favorites (driver_id);
CREATE INDEX IF NOT EXISTS idx_driver_cargo_favorites_cargo ON driver_cargo_favorites (cargo_id);

CREATE TABLE IF NOT EXISTS freelance_dispatcher_cargo_favorites (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  dispatcher_id UUID NOT NULL REFERENCES freelance_dispatchers(id) ON DELETE CASCADE,
  cargo_id UUID NOT NULL REFERENCES cargo(id) ON DELETE CASCADE,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  UNIQUE(dispatcher_id, cargo_id)
);
CREATE INDEX IF NOT EXISTS idx_freelance_dispatcher_cargo_favorites_dispatcher ON freelance_dispatcher_cargo_favorites (dispatcher_id);
CREATE INDEX IF NOT EXISTS idx_freelance_dispatcher_cargo_favorites_cargo ON freelance_dispatcher_cargo_favorites (cargo_id);

CREATE TABLE IF NOT EXISTS freelance_dispatcher_driver_favorites (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  dispatcher_id UUID NOT NULL REFERENCES freelance_dispatchers(id) ON DELETE CASCADE,
  driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  UNIQUE(dispatcher_id, driver_id)
);
CREATE INDEX IF NOT EXISTS idx_freelance_dispatcher_driver_favorites_dispatcher ON freelance_dispatcher_driver_favorites (dispatcher_id);
CREATE INDEX IF NOT EXISTS idx_freelance_dispatcher_driver_favorites_driver ON freelance_dispatcher_driver_favorites (driver_id);

