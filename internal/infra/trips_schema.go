package infra

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureTripAgreedPriceColumns adds trips.agreed_price / agreed_currency and archived_trips columns if missing
// (idempotent). Used by integration tests when full migrations are not applied.
func EnsureTripAgreedPriceColumns(ctx context.Context, pg *pgxpool.Pool) error {
	_, err := pg.Exec(ctx, `
ALTER TABLE trips
  ADD COLUMN IF NOT EXISTS agreed_price NUMERIC(18, 2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS agreed_currency VARCHAR(3) NOT NULL DEFAULT 'UZS';
`)
	if err != nil {
		return err
	}
	_, err = pg.Exec(ctx, `
DO $$
BEGIN
  IF to_regclass('public.archived_trips') IS NOT NULL THEN
    ALTER TABLE archived_trips
      ADD COLUMN IF NOT EXISTS agreed_price NUMERIC(18, 2) NOT NULL DEFAULT 0,
      ADD COLUMN IF NOT EXISTS agreed_currency VARCHAR(3) NOT NULL DEFAULT 'UZS';
  END IF;
END$$;
`)
	return err
}
