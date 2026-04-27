-- Who proposed the price in the offer: DRIVER (driver asks dispatcher to accept) or DISPATCHER (dispatcher asks driver to accept).

ALTER TABLE offers ADD COLUMN IF NOT EXISTS proposed_by VARCHAR(20) NOT NULL DEFAULT 'DRIVER';

UPDATE offers SET proposed_by = 'DRIVER' WHERE proposed_by IS NULL OR proposed_by = '';

ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_proposed_by_check;
ALTER TABLE offers ADD CONSTRAINT offers_proposed_by_check CHECK (proposed_by IN ('DRIVER', 'DISPATCHER'));
