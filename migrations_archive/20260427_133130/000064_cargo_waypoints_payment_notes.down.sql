ALTER TABLE payments
  DROP COLUMN IF EXISTS payment_terms_note,
  DROP COLUMN IF EXISTS payment_note;

ALTER TABLE cargo
  DROP COLUMN IF EXISTS unloading_types,
  DROP COLUMN IF EXISTS is_two_drivers_required,
  DROP COLUMN IF EXISTS packaging_amount,
  DROP COLUMN IF EXISTS way_points;
