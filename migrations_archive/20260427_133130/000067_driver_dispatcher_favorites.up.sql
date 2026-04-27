-- Driver bookmarks freelance dispatchers (shortlist), separate from freelancer_id link.
CREATE TABLE IF NOT EXISTS driver_dispatcher_favorites (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
  dispatcher_id UUID NOT NULL REFERENCES freelance_dispatchers(id) ON DELETE CASCADE,
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  UNIQUE(driver_id, dispatcher_id)
);
CREATE INDEX IF NOT EXISTS idx_driver_dispatcher_favorites_driver ON driver_dispatcher_favorites (driver_id);
CREATE INDEX IF NOT EXISTS idx_driver_dispatcher_favorites_dispatcher ON driver_dispatcher_favorites (dispatcher_id);
