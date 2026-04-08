package trips

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("trip not found")
var ErrInvalidTransition = errors.New("invalid status transition")
var ErrForbiddenRole = errors.New("trip: not allowed for this role")

var allowedTransitions = map[string][]string{
	StatusInProgress: {StatusInTransit, StatusCancelled},
	StatusInTransit:  {StatusDelivered, StatusCancelled},
	StatusDelivered:  {StatusCompleted, StatusCancelled},
	StatusCompleted:  nil,
	StatusCancelled:  nil,
}

// NextStatus is the next operational status (single-step).
func NextStatus(current string) string {
	switch strings.ToUpper(strings.TrimSpace(current)) {
	case StatusInProgress:
		return StatusInTransit
	case StatusInTransit:
		return StatusDelivered
	case StatusDelivered:
		return StatusCompleted
	default:
		return ""
	}
}

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

func (r *Repo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pg.Begin(ctx)
}

// Create creates trip with status IN_PROGRESS (after offer accepted). agreedPrice/currency — договор с этим водителем.
func (r *Repo) Create(ctx context.Context, cargoID, offerID uuid.UUID, agreedPrice float64, agreedCurrency string) (uuid.UUID, error) {
	cur := strings.ToUpper(strings.TrimSpace(agreedCurrency))
	if cur == "" {
		cur = "UZS"
	}
	var id uuid.UUID
	err := r.pg.QueryRow(ctx,
		`INSERT INTO trips (cargo_id, offer_id, status, agreed_price, agreed_currency) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		cargoID, offerID, StatusInProgress, agreedPrice, cur).Scan(&id)
	return id, err
}

func scanTrip(row pgx.Row) (*Trip, error) {
	var t Trip
	err := row.Scan(&t.ID, &t.CargoID, &t.OfferID, &t.DriverID, &t.Status,
		&t.AgreedPrice, &t.AgreedCurrency,
		&t.CreatedAt, &t.UpdatedAt,
		&t.PendingConfirmTo, &t.DriverConfirmedAt, &t.DispatcherConfirmedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

const tripSelect = `SELECT id, cargo_id, offer_id, driver_id, status, agreed_price, agreed_currency, created_at, updated_at,
  pending_confirm_to, driver_confirmed_at, dispatcher_confirmed_at FROM trips `

// GetByID returns trip by id.
func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*Trip, error) {
	t, err := scanTrip(r.pg.QueryRow(ctx, tripSelect+`WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// GetByIDTx returns trip by id using an existing transaction.
func (r *Repo) GetByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Trip, error) {
	return scanTrip(tx.QueryRow(ctx, tripSelect+`WHERE id = $1`, id))
}

func (r *Repo) getByIDForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Trip, error) {
	return scanTrip(tx.QueryRow(ctx, tripSelect+`WHERE id = $1 FOR UPDATE`, id))
}

// GetByIDForUpdateTx loads a trip row FOR UPDATE (caller begins transaction).
func (r *Repo) GetByIDForUpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Trip, error) {
	t, err := scanTrip(tx.QueryRow(ctx, tripSelect+`WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// GetByOfferID returns trip by offer_id (unique).
func (r *Repo) GetByOfferID(ctx context.Context, offerID uuid.UUID) (*Trip, error) {
	t, err := scanTrip(r.pg.QueryRow(ctx, tripSelect+`WHERE offer_id = $1`, offerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// ListByCargoID returns all trips for a cargo (multiple vehicles / drivers).
func (r *Repo) ListByCargoID(ctx context.Context, cargoID uuid.UUID) ([]Trip, error) {
	rows, err := r.pg.Query(ctx, tripSelect+`WHERE cargo_id = $1 ORDER BY created_at ASC`, cargoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTripRows(rows)
}

// GetByCargoID returns the latest trip for cargo (legacy; prefer ListByCargoID).
func (r *Repo) GetByCargoID(ctx context.Context, cargoID uuid.UUID) (*Trip, error) {
	t, err := scanTrip(r.pg.QueryRow(ctx, tripSelect+`WHERE cargo_id = $1 ORDER BY created_at DESC LIMIT 1`, cargoID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// HasActiveTripForCargo returns true if a non-terminal trip row exists for this cargo.
func (r *Repo) HasActiveTripForCargo(ctx context.Context, cargoID uuid.UUID) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx,
		`SELECT COUNT(*) FROM trips WHERE cargo_id = $1 AND status NOT IN ($2, $3)`,
		cargoID, StatusCompleted, StatusCancelled).Scan(&n)
	return n > 0, err
}

// AssignDriver sets driver_id (dispatcher assigns driver). Trip must be in_progress without driver.
func (r *Repo) AssignDriver(ctx context.Context, tripID, driverID uuid.UUID) error {
	res, err := r.pg.Exec(ctx,
		`UPDATE trips SET driver_id = $2,
			pending_confirm_to = NULL, driver_confirmed_at = NULL, dispatcher_confirmed_at = NULL,
			updated_at = now() WHERE id = $1 AND status = $3 AND driver_id IS NULL`,
		tripID, driverID, StatusInProgress)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DriverReject clears driver assignment so dispatcher can assign another driver.
func (r *Repo) DriverReject(ctx context.Context, tripID, driverID uuid.UUID) error {
	res, err := r.pg.Exec(ctx,
		`UPDATE trips SET driver_id = NULL,
			pending_confirm_to = NULL, driver_confirmed_at = NULL, dispatcher_confirmed_at = NULL,
			updated_at = now()
		 WHERE id = $1 AND driver_id = $2 AND status = $3`,
		tripID, driverID, StatusInProgress)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ConfirmTransitionTx advances trip one step (unilateral: driver or dispatcher may call).
func (r *Repo) ConfirmTransitionTx(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, driverID uuid.UUID, asDispatcher bool) (*Trip, error) {
	return r.confirmTransitionTx(ctx, tx, tripID, driverID, asDispatcher)
}

func (r *Repo) confirmTransitionTx(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, driverID uuid.UUID, asDispatcher bool) (*Trip, error) {
	t, err := r.getByIDForUpdate(ctx, tx, tripID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	if t.Status == StatusCompleted || t.Status == StatusCancelled {
		return nil, ErrInvalidTransition
	}
	if !asDispatcher {
		if t.DriverID == nil || *t.DriverID != driverID {
			return nil, ErrForbiddenRole
		}
	}
	next := NextStatus(t.Status)
	if next == "" {
		return nil, ErrInvalidTransition
	}
	if t.Status == StatusInProgress && t.DriverID == nil {
		return nil, ErrInvalidTransition
	}
	allowed := allowedTransitions[t.Status]
	ok := false
	for _, s := range allowed {
		if s == next {
			ok = true
			break
		}
	}
	if !ok {
		return nil, ErrInvalidTransition
	}

	_, err = tx.Exec(ctx, `
		UPDATE trips SET
		  status = $2,
		  pending_confirm_to = NULL,
		  driver_confirmed_at = NULL,
		  dispatcher_confirmed_at = NULL,
		  updated_at = now()
		WHERE id = $1`,
		tripID, next)
	if err != nil {
		return nil, err
	}
	return r.GetByIDTx(ctx, tx, tripID)
}

// SetStatus updates trip status without checks (internal / tests).
func (r *Repo) SetStatus(ctx context.Context, tripID uuid.UUID, newStatus string) error {
	t, err := r.GetByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if t == nil {
		return ErrNotFound
	}
	allowed := allowedTransitions[t.Status]
	for _, s := range allowed {
		if s == newStatus {
			_, err = r.pg.Exec(ctx, `UPDATE trips SET status = $1, updated_at = now() WHERE id = $2`, newStatus, tripID)
			return err
		}
	}
	return ErrInvalidTransition
}

// ArchiveTripAndDeleteTx inserts into archived_trips and deletes the trip row (cancellation).
func (r *Repo) ArchiveTripAndDeleteTx(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, cancelledByRole string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO archived_trips (id, cargo_id, offer_id, driver_id, status, created_at, updated_at, archived_at, cancel_reason, cancelled_by_role, agreed_price, agreed_currency)
		SELECT id, cargo_id, offer_id, driver_id, status, created_at, updated_at, now(), 'CANCELLED', $2, agreed_price, agreed_currency
		FROM trips WHERE id = $1`,
		tripID, cancelledByRole)
	if err != nil {
		return err
	}
	res, err := tx.Exec(ctx, `DELETE FROM trips WHERE id = $1`, tripID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByDriver returns trips for driver (where driver_id = driverID).
func (r *Repo) ListByDriver(ctx context.Context, driverID uuid.UUID, limit int) ([]Trip, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pg.Query(ctx,
		tripSelect+`WHERE driver_id = $1 ORDER BY created_at DESC LIMIT $2`,
		driverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTripRows(rows)
}

// ListByCargoIDs returns trips for given cargo IDs (for dispatcher listing by cargo).
func (r *Repo) ListByCargoIDs(ctx context.Context, cargoIDs []uuid.UUID) ([]Trip, error) {
	if len(cargoIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pg.Query(ctx,
		tripSelect+`WHERE cargo_id = ANY($1) ORDER BY created_at DESC`,
		cargoIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTripRows(rows)
}

func scanTripRows(rows pgx.Rows) ([]Trip, error) {
	var list []Trip
	for rows.Next() {
		var t Trip
		err := rows.Scan(&t.ID, &t.CargoID, &t.OfferID, &t.DriverID, &t.Status,
			&t.AgreedPrice, &t.AgreedCurrency,
			&t.CreatedAt, &t.UpdatedAt,
			&t.PendingConfirmTo, &t.DriverConfirmedAt, &t.DispatcherConfirmedAt)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}
