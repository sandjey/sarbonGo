-- Add PROCESSING cargo status: cargo leaves SEARCHING_* once all offer slots are
-- filled (accepted_count == vehicles_amount), then stays PROCESSING until every
-- trip is COMPLETED (→ COMPLETED). If a slot opens back up (offer reverts to
-- PENDING through trip cancellation), cargo rolls back to prev_status.

ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_status_check;

ALTER TABLE cargo ADD CONSTRAINT cargo_status_check CHECK (status IN (
  'PENDING_MODERATION',
  'SEARCHING_ALL',
  'SEARCHING_COMPANY',
  'PROCESSING',
  'COMPLETED',
  'CANCELLED'
));

-- Remembered searching variant so we can revert PROCESSING → SEARCHING_ALL /
-- SEARCHING_COMPANY on cancellation. NULL when cargo is not (and was not) in PROCESSING.
ALTER TABLE cargo ADD COLUMN IF NOT EXISTS prev_status TEXT;

ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_prev_status_check;
ALTER TABLE cargo ADD CONSTRAINT cargo_prev_status_check CHECK (
  prev_status IS NULL OR prev_status IN ('SEARCHING_ALL', 'SEARCHING_COMPANY')
);
