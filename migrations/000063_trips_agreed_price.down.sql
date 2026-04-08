ALTER TABLE archived_trips DROP COLUMN IF EXISTS agreed_currency;
ALTER TABLE archived_trips DROP COLUMN IF EXISTS agreed_price;

ALTER TABLE trips DROP COLUMN IF EXISTS agreed_currency;
ALTER TABLE trips DROP COLUMN IF EXISTS agreed_price;
