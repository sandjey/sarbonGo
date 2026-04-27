-- Simplified cargo/trip lifecycle: cargo stays SEARCHING_* until all trips COMPLETED; trips use IN_PROGRESS → IN_TRANSIT → DELIVERED → COMPLETED.

-- 1) Cargo: collapse mid-flight cargo statuses back to searching (execution tracked on trips only).
ALTER TABLE cargo DROP CONSTRAINT IF EXISTS cargo_status_check;
UPDATE cargo SET status = 'SEARCHING_ALL'
  WHERE status IN ('ASSIGNED', 'IN_PROGRESS', 'IN_TRANSIT', 'DELIVERED');
ALTER TABLE cargo ADD CONSTRAINT cargo_status_check CHECK (status IN (
  'CREATED', 'PENDING_MODERATION', 'REJECTED', 'SEARCHING_ALL', 'SEARCHING_COMPANY', 'COMPLETED', 'CANCELLED'
));

-- 2) Trips: map legacy statuses to the new linear model.
ALTER TABLE trips DROP CONSTRAINT IF EXISTS trips_status_check;
UPDATE trips SET status = CASE status
  WHEN 'PENDING_DRIVER' THEN 'IN_PROGRESS'
  WHEN 'ASSIGNED' THEN 'IN_PROGRESS'
  WHEN 'LOADING' THEN 'IN_PROGRESS'
  WHEN 'EN_ROUTE' THEN 'IN_TRANSIT'
  WHEN 'UNLOADING' THEN 'DELIVERED'
  ELSE status
END
WHERE status IN ('PENDING_DRIVER', 'ASSIGNED', 'LOADING', 'EN_ROUTE', 'UNLOADING');
ALTER TABLE trips ADD CONSTRAINT trips_status_check CHECK (status IN (
  'IN_PROGRESS', 'IN_TRANSIT', 'DELIVERED', 'COMPLETED', 'CANCELLED'
));

-- 3) Bilateral confirm column: allow new pending_confirm_to values (legacy field, rarely set).
ALTER TABLE trips DROP CONSTRAINT IF EXISTS trips_pending_confirm_check;
ALTER TABLE trips ADD CONSTRAINT trips_pending_confirm_check CHECK (
  pending_confirm_to IS NULL OR pending_confirm_to IN ('IN_TRANSIT', 'DELIVERED', 'COMPLETED')
);
