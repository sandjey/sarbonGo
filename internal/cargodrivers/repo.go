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
func AcceptTx(ctx context.Context, tx pgx.Tx, cargoID, driverID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO cargo_drivers (cargo_id, driver_id, status)
		 VALUES ($1, $2, $3)`,
		cargoID, driverID, StatusActive)
	if err != nil {
		// unique active per driver
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.ConstraintName == "ux_cargo_drivers_driver_active" {
			return ErrDriverBusy
		}
		// already exists (cargo_id, driver_id)
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.ConstraintName == "cargo_drivers_cargo_id_driver_id_key" {
			return nil
		}
		return err
	}
	return nil
}

