-- Track invitation outcomes: who invited whom, accepted/declined/cancelled/expired (pending+past expiry).

ALTER TABLE driver_invitations
  ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS responded_at TIMESTAMPTZ NULL;

ALTER TABLE driver_invitations
  DROP CONSTRAINT IF EXISTS chk_driver_invitations_status;

ALTER TABLE driver_invitations
  ADD CONSTRAINT chk_driver_invitations_status
  CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled'));

CREATE INDEX IF NOT EXISTS idx_driver_invitations_phone_status_created
  ON driver_invitations ((replace(replace(replace(trim(phone), ' ', ''), '-', ''), '+', '')), status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_driver_invitations_invited_by_status_created
  ON driver_invitations (invited_by, status, created_at DESC);

ALTER TABLE driver_to_dispatcher_invitations
  ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS responded_at TIMESTAMPTZ NULL;

ALTER TABLE driver_to_dispatcher_invitations
  DROP CONSTRAINT IF EXISTS chk_d2d_invitations_status;

ALTER TABLE driver_to_dispatcher_invitations
  ADD CONSTRAINT chk_d2d_invitations_status
  CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled'));

CREATE INDEX IF NOT EXISTS idx_d2d_invitations_driver_status_created
  ON driver_to_dispatcher_invitations (driver_id, status, created_at DESC);
