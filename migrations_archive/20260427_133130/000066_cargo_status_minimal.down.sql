-- Best-effort rollback: re-allow legacy statuses (data may not round-trip perfectly).

ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_status_check;

ALTER TABLE cargo ADD CONSTRAINT cargo_status_check CHECK (status IN (
  'CREATED', 'PENDING_MODERATION', 'REJECTED',
  'SEARCHING_ALL', 'SEARCHING_COMPANY',
  'ASSIGNED', 'IN_TRANSIT', 'DELIVERED', 'IN_PROGRESS',
  'COMPLETED', 'CANCELLED'
));

