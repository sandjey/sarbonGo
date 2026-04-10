package cargodrivers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusActive    = "ACTIVE"
	StatusCompleted = "COMPLETED"
	StatusCancelled = "CANCELLED"
	StatusRemoved   = "REMOVED"
)

var ErrDriverBusy = errors.New("cargodrivers: driver already has active cargo")
var ErrNotFound = errors.New("cargodrivers: not found")

type CargoDriver struct {
	ID        uuid.UUID
	CargoID   uuid.UUID
	DriverID  uuid.UUID
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo { return &Repo{pg: pg} }

func (r *Repo) ListByCargo(ctx context.Context, cargoID uuid.UUID, limit int) ([]CargoDriver, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.pg.Query(ctx,
		`SELECT id, cargo_id, driver_id, status, created_at, updated_at
		 FROM cargo_drivers
		 WHERE cargo_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		cargoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CargoDriver
	for rows.Next() {
		var cd CargoDriver
		if err := rows.Scan(&cd.ID, &cd.CargoID, &cd.DriverID, &cd.Status, &cd.CreatedAt, &cd.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, cd)
	}
	return out, rows.Err()
}

func (r *Repo) GetActiveCargoIDByDriver(ctx context.Context, driverID uuid.UUID) (*uuid.UUID, error) {
	var cargoID uuid.UUID
	err := r.pg.QueryRow(ctx,
		`SELECT cargo_id FROM cargo_drivers WHERE driver_id = $1 AND status = $2 LIMIT 1`,
		driverID, StatusActive).Scan(&cargoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &cargoID, nil
}

// MarkFinished sets status to COMPLETED/CANCELLED/REMOVED for the (cargo, driver) link, but only if currently ACTIVE.
func (r *Repo) MarkFinished(ctx context.Context, cargoID, driverID uuid.UUID, newStatus string) error {
	switch newStatus {
	case StatusCompleted, StatusCancelled, StatusRemoved:
	default:
		return errors.New("cargodrivers: invalid status")
	}
	res, err := r.pg.Exec(ctx,
		`UPDATE cargo_drivers
		 SET status = $3, updated_at = now()
		 WHERE cargo_id = $1 AND driver_id = $2 AND status = $4`,
		cargoID, driverID, newStatus, StatusActive)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AcceptTx inserts ACTIVE link inside existing tx.
// Uses SAVEPOINT so a failed INSERT (unique violation) does not abort the whole transaction:
// PostgreSQL would otherwise reject further SQL with SQLSTATE 25P02 until ROLLBACK.
func AcceptTx(ctx context.Context, tx pgx.Tx, cargoID, driverID uuid.UUID) error {
	const sp = "sp_cargo_drivers_accept"
	insertSQL := `INSERT INTO cargo_drivers (cargo_id, driver_id, status)
		 VALUES ($1, $2, $3)`

	if _, err := tx.Exec(ctx, "SAVEPOINT "+sp); err != nil {
		return err
	}
	release := func() error {
		_, e := tx.Exec(ctx, "RELEASE SAVEPOINT "+sp)
		return e
	}
	rollback := func() error {
		_, e := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+sp)
		return e
	}

	_, err := tx.Exec(ctx, insertSQL, cargoID, driverID, StatusActive)
	if err == nil {
		return release()
	}

	pgErr, ok := err.(*pgconn.PgError)
	if !ok {
		_ = rollback()
		_ = release()
		return err
	}

	// Duplicate (cargo_id, driver_id): link already exists — treat as success.
	if pgErr.ConstraintName == "cargo_drivers_cargo_id_driver_id_key" {
		if e := rollback(); e != nil {
			return e
		}
		return release()
	}

	// Unique partial index: one ACTIVE cargo per driver — may be stale row; recover after rollback.
	isActiveDriverConflict := pgErr.ConstraintName == "ux_cargo_drivers_driver_active" ||
		(pgErr.Code == "23505" && pgErr.TableName == "cargo_drivers")
	if isActiveDriverConflict {
		if e := rollback(); e != nil {
			return e
		}
		released, relErr := releaseStaleActiveTx(ctx, tx, driverID)
		if relErr != nil {
			_ = release()
			return relErr
		}
		if !released {
			if e := release(); e != nil {
				return e
			}
			return ErrDriverBusy
		}
		// released stale ACTIVE — retry insert (still under SAVEPOINT; another failure aborts sub-xact)
		_, err2 := tx.Exec(ctx, insertSQL, cargoID, driverID, StatusActive)
		if err2 == nil {
			return release()
		}
		if rb2 := rollback(); rb2 != nil {
			return rb2
		}
		if pgErr2, ok2 := err2.(*pgconn.PgError); ok2 {
			if pgErr2.ConstraintName == "cargo_drivers_cargo_id_driver_id_key" {
				return release()
			}
			if pgErr2.ConstraintName == "ux_cargo_drivers_driver_active" || (pgErr2.Code == "23505" && pgErr2.TableName == "cargo_drivers") {
				if e := release(); e != nil {
					return e
				}
				return ErrDriverBusy
			}
		}
		if e := release(); e != nil {
			return e
		}
		return err2
	}

	if e := rollback(); e != nil {
		return e
	}
	_ = release()
	return err
}

func releaseStaleActiveTx(ctx context.Context, tx pgx.Tx, driverID uuid.UUID) (bool, error) {
	var cargoID uuid.UUID
	err := tx.QueryRow(ctx, `
SELECT cargo_id
FROM cargo_drivers
WHERE driver_id = $1 AND status = 'ACTIVE'
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE`, driverID).Scan(&cargoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	var activeTripCount int
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*)
FROM trips
WHERE cargo_id = $1
  AND driver_id = $2
  AND status NOT IN ('COMPLETED', 'CANCELLED')`, cargoID, driverID).Scan(&activeTripCount); err != nil {
		return false, err
	}
	if activeTripCount > 0 {
		return false, nil
	}

	res, err := tx.Exec(ctx, `
UPDATE cargo_drivers
SET status = 'CANCELLED', updated_at = now()
WHERE cargo_id = $1 AND driver_id = $2 AND status = 'ACTIVE'`, cargoID, driverID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

