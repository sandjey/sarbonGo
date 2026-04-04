DROP TABLE IF EXISTS archived_trips;
DROP TABLE IF EXISTS archived_cargo;

ALTER TABLE trips DROP CONSTRAINT IF EXISTS trips_pending_confirm_check;
ALTER TABLE trips DROP COLUMN IF EXISTS pending_confirm_to;
ALTER TABLE trips DROP COLUMN IF EXISTS driver_confirmed_at;
ALTER TABLE trips DROP COLUMN IF EXISTS dispatcher_confirmed_at;
