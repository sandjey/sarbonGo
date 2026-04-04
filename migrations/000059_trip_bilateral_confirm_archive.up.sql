-- Bilateral confirmation for trip status transitions (pending_confirm_to + driver/dispatcher timestamps).
-- Archived cargo/trips on successful completion; trip-only archive on cancel.

ALTER TABLE trips
  ADD COLUMN IF NOT EXISTS pending_confirm_to VARCHAR(50) NULL,
  ADD COLUMN IF NOT EXISTS driver_confirmed_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS dispatcher_confirmed_at TIMESTAMPTZ NULL;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'trips_pending_confirm_check') THEN
    ALTER TABLE trips ADD CONSTRAINT trips_pending_confirm_check CHECK (
      pending_confirm_to IS NULL OR pending_confirm_to IN (
        'ASSIGNED', 'LOADING', 'EN_ROUTE', 'UNLOADING', 'COMPLETED'
      )
    );
  END IF;
END$$;

CREATE TABLE IF NOT EXISTS archived_cargo (
  id UUID PRIMARY KEY,
  snapshot JSONB NOT NULL,
  archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_archived_cargo_archived_at ON archived_cargo (archived_at DESC);

CREATE TABLE IF NOT EXISTS archived_trips (
  id UUID PRIMARY KEY,
  cargo_id UUID NOT NULL,
  offer_id UUID NOT NULL,
  driver_id UUID NULL,
  status VARCHAR(50) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  archived_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  cancel_reason TEXT NULL,
  cancelled_by_role VARCHAR(20) NULL
);
CREATE INDEX IF NOT EXISTS idx_archived_trips_cargo_id ON archived_trips (cargo_id);
CREATE INDEX IF NOT EXISTS idx_archived_trips_archived_at ON archived_trips (archived_at DESC);
