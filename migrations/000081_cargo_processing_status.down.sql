-- Revert: drop PROCESSING status. Any existing PROCESSING rows roll back to
-- their previous searching variant (or SEARCHING_ALL as a safe default).

UPDATE cargo
SET status = COALESCE(prev_status, 'SEARCHING_ALL'),
    prev_status = NULL,
    updated_at = now()
WHERE status = 'PROCESSING';

ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_prev_status_check;
ALTER TABLE cargo DROP COLUMN IF EXISTS prev_status;

ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_status_check;
ALTER TABLE cargo ADD CONSTRAINT cargo_status_check CHECK (status IN (
  'PENDING_MODERATION',
  'SEARCHING_ALL',
  'SEARCHING_COMPANY',
  'COMPLETED',
  'CANCELLED'
));
