package favorites

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

type CargoFavorite struct {
	CargoID   uuid.UUID
	CreatedAt time.Time
}

type DriverFavorite struct {
	DriverID  uuid.UUID
	CreatedAt time.Time
}

// --- Driver <-> Cargo favorites ---

func (r *Repo) AddDriverCargoFavorite(ctx context.Context, driverID, cargoID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
INSERT INTO driver_cargo_favorites(driver_id, cargo_id)
VALUES ($1, $2)
ON CONFLICT (driver_id, cargo_id) DO NOTHING
`, driverID, cargoID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) DeleteDriverCargoFavorite(ctx context.Context, driverID, cargoID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
DELETE FROM driver_cargo_favorites
WHERE driver_id = $1 AND cargo_id = $2
`, driverID, cargoID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

type DispatcherFavorite struct {
	DispatcherID uuid.UUID
	CreatedAt    time.Time
}

// --- Driver <-> Dispatcher favorites (bookmark freelance dispatchers) ---

func (r *Repo) AddDriverDispatcherFavorite(ctx context.Context, driverID, dispatcherID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
INSERT INTO driver_dispatcher_favorites(driver_id, dispatcher_id)
VALUES ($1, $2)
ON CONFLICT (driver_id, dispatcher_id) DO NOTHING
`, driverID, dispatcherID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) DeleteDriverDispatcherFavorite(ctx context.Context, driverID, dispatcherID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
DELETE FROM driver_dispatcher_favorites
WHERE driver_id = $1 AND dispatcher_id = $2
`, driverID, dispatcherID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) ListDriverDispatcherFavorites(ctx context.Context, driverID uuid.UUID, limit int) ([]DispatcherFavorite, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.pg.Query(ctx, `
SELECT dispatcher_id, created_at
FROM driver_dispatcher_favorites
WHERE driver_id = $1
ORDER BY created_at DESC
LIMIT $2
`, driverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DispatcherFavorite
	for rows.Next() {
		var f DispatcherFavorite
		if err := rows.Scan(&f.DispatcherID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *Repo) ListDriverCargoFavorites(ctx context.Context, driverID uuid.UUID, limit int) ([]CargoFavorite, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := r.pg.Query(ctx, `
SELECT cargo_id, created_at
FROM driver_cargo_favorites
WHERE driver_id = $1
ORDER BY created_at DESC
LIMIT $2
`, driverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CargoFavorite
	for rows.Next() {
		var f CargoFavorite
		if err := rows.Scan(&f.CargoID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// --- Freelance dispatcher <-> Cargo favorites ---

func (r *Repo) AddDispatcherCargoFavorite(ctx context.Context, dispatcherID, cargoID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
INSERT INTO freelance_dispatcher_cargo_favorites(dispatcher_id, cargo_id)
VALUES ($1, $2)
ON CONFLICT (dispatcher_id, cargo_id) DO NOTHING
`, dispatcherID, cargoID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) DeleteDispatcherCargoFavorite(ctx context.Context, dispatcherID, cargoID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
DELETE FROM freelance_dispatcher_cargo_favorites
WHERE dispatcher_id = $1 AND cargo_id = $2
`, dispatcherID, cargoID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) ListDispatcherCargoFavorites(ctx context.Context, dispatcherID uuid.UUID, limit int) ([]CargoFavorite, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := r.pg.Query(ctx, `
SELECT cargo_id, created_at
FROM freelance_dispatcher_cargo_favorites
WHERE dispatcher_id = $1
ORDER BY created_at DESC
LIMIT $2
`, dispatcherID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CargoFavorite
	for rows.Next() {
		var f CargoFavorite
		if err := rows.Scan(&f.CargoID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// --- Freelance dispatcher <-> Driver favorites ---

func (r *Repo) AddDispatcherDriverFavorite(ctx context.Context, dispatcherID, driverID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
INSERT INTO freelance_dispatcher_driver_favorites(dispatcher_id, driver_id)
VALUES ($1, $2)
ON CONFLICT (dispatcher_id, driver_id) DO NOTHING
`, dispatcherID, driverID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) DeleteDispatcherDriverFavorite(ctx context.Context, dispatcherID, driverID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
DELETE FROM freelance_dispatcher_driver_favorites
WHERE dispatcher_id = $1 AND driver_id = $2
`, dispatcherID, driverID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) ListDispatcherDriverFavorites(ctx context.Context, dispatcherID uuid.UUID, limit int) ([]DriverFavorite, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := r.pg.Query(ctx, `
SELECT driver_id, created_at
FROM freelance_dispatcher_driver_favorites
WHERE dispatcher_id = $1
ORDER BY created_at DESC
LIMIT $2
`, dispatcherID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DriverFavorite
	for rows.Next() {
		var f DriverFavorite
		if err := rows.Scan(&f.DriverID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
