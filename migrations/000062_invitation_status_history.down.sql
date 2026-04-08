DROP INDEX IF EXISTS idx_d2d_invitations_driver_status_created;
ALTER TABLE driver_to_dispatcher_invitations DROP CONSTRAINT IF EXISTS chk_d2d_invitations_status;
ALTER TABLE driver_to_dispatcher_invitations DROP COLUMN IF EXISTS responded_at;
ALTER TABLE driver_to_dispatcher_invitations DROP COLUMN IF EXISTS status;

DROP INDEX IF EXISTS idx_driver_invitations_invited_by_status_created;
DROP INDEX IF EXISTS idx_driver_invitations_phone_status_created;
ALTER TABLE driver_invitations DROP CONSTRAINT IF EXISTS chk_driver_invitations_status;
ALTER TABLE driver_invitations DROP COLUMN IF EXISTS responded_at;
ALTER TABLE driver_invitations DROP COLUMN IF EXISTS status;
