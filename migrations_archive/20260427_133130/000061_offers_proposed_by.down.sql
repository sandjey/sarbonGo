ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_proposed_by_check;
ALTER TABLE offers DROP COLUMN IF EXISTS proposed_by;
