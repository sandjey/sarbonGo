-- Договорная цена на уровне рейса (на грузе — объявленная цена в payments без изменения при accept).

ALTER TABLE trips
  ADD COLUMN IF NOT EXISTS agreed_price NUMERIC(18, 2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS agreed_currency VARCHAR(3) NOT NULL DEFAULT 'UZS';

UPDATE trips t
SET agreed_price = o.price::numeric(18,2),
    agreed_currency = upper(trim(o.currency))
FROM offers o
WHERE t.offer_id = o.id;

ALTER TABLE archived_trips
  ADD COLUMN IF NOT EXISTS agreed_price NUMERIC(18, 2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS agreed_currency VARCHAR(3) NOT NULL DEFAULT 'UZS';
