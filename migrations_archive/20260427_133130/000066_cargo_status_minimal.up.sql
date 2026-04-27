-- Tighten cargo status set to minimal lifecycle:
-- PENDING_MODERATION → SEARCHING_ALL|SEARCHING_COMPANY → COMPLETED, or CANCELLED.
-- Legacy statuses are mapped to the closest equivalent.

ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_status_check;

-- Map legacy/removed statuses.
UPDATE cargo SET status = 'PENDING_MODERATION' WHERE status = 'CREATED';
UPDATE cargo SET status = 'CANCELLED' WHERE status = 'REJECTED';
UPDATE cargo SET status = 'SEARCHING_ALL' WHERE status IN ('ASSIGNED', 'IN_PROGRESS', 'IN_TRANSIT', 'DELIVERED');

ALTER TABLE cargo ADD CONSTRAINT cargo_status_check CHECK (status IN (
  'PENDING_MODERATION',
  'SEARCHING_ALL',
  'SEARCHING_COMPANY',
  'COMPLETED',
  'CANCELLED'
));

