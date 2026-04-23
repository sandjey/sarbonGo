package triprating

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("trip rating not found")
var ErrInvalidStars = errors.New("stars must be 1..5 in steps of 0.5")

const (
	dispatcherRateeRoleUnknown       = ""
	dispatcherRateeRoleCargoManager  = "cargo_manager"
	dispatcherRateeRoleDriverManager = "driver_manager"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

// ValidateStars checks allowed values 1.0, 1.5, …, 5.0 (half-star steps).
func ValidateStars(s float64) error {
	if s < 1-1e-6 || s > 5+1e-6 {
		return ErrInvalidStars
	}
	x := s * 2
	if math.Abs(x-math.Round(x)) > 1e-3 {
		return ErrInvalidStars
	}
	return nil
}

func tripMirrorTargets(raterKind, rateeKind, dispatcherRateeRole string) []string {
	rk := strings.ToLower(strings.TrimSpace(raterKind))
	ek := strings.ToLower(strings.TrimSpace(rateeKind))
	switch {
	case rk == "driver" && ek == "dispatcher":
		switch dispatcherRateeRole {
		case dispatcherRateeRoleCargoManager:
			return []string{"rating_from_driver"}
		case dispatcherRateeRoleDriverManager:
			return []string{"rating_driver_to_dm"}
		default:
			return nil
		}
	case rk == "driver_manager" && ek == "driver":
		return []string{"rating_dm_to_driver"}
	case rk == "driver_manager" && ek == "dispatcher":
		return []string{"rating_dm_to_cm"}
	case rk == "dispatcher" && ek == "driver_manager":
		return []string{"rating_cm_to_dm"}
	case rk == "dispatcher" && ek == "driver":
		return []string{"rating_from_dispatcher"}
	default:
		return nil
	}
}

func (r *Repo) resolveDispatcherRateeRole(ctx context.Context, tripID, rateeID uuid.UUID) (string, error) {
	var (
		createdByType           *string
		cargoDispatcherID       *uuid.UUID
		offerProposedBy         *string
		offerProposedByID       *uuid.UUID
		negotiationDispatcherID *uuid.UUID
	)
	err := r.pg.QueryRow(ctx, `
SELECT
  c.created_by_type,
  c.created_by_id,
  o.proposed_by,
  o.proposed_by_id,
  o.negotiation_dispatcher_id
FROM trips t
INNER JOIN cargo c ON c.id = t.cargo_id
INNER JOIN offers o ON o.id = t.offer_id
WHERE t.id = $1
`, tripID).Scan(&createdByType, &cargoDispatcherID, &offerProposedBy, &offerProposedByID, &negotiationDispatcherID)
	if err != nil {
		return dispatcherRateeRoleUnknown, err
	}
	if negotiationDispatcherID != nil && *negotiationDispatcherID == rateeID {
		return dispatcherRateeRoleDriverManager, nil
	}
	if offerProposedByID != nil && *offerProposedByID == rateeID && strings.EqualFold(strings.TrimSpace(derefString(offerProposedBy)), "DRIVER_MANAGER") {
		return dispatcherRateeRoleDriverManager, nil
	}
	if cargoDispatcherID != nil && *cargoDispatcherID == rateeID && strings.EqualFold(strings.TrimSpace(derefString(createdByType)), "DISPATCHER") {
		return dispatcherRateeRoleCargoManager, nil
	}
	if offerProposedByID != nil && *offerProposedByID == rateeID && strings.EqualFold(strings.TrimSpace(derefString(offerProposedBy)), "DISPATCHER") {
		return dispatcherRateeRoleCargoManager, nil
	}
	return dispatcherRateeRoleUnknown, nil
}

// Upsert inserts or updates the single rating for (trip_id, rater_kind, ratee_kind, ratee_id).
func (r *Repo) Upsert(ctx context.Context, tripID uuid.UUID, raterKind string, raterID uuid.UUID, rateeKind string, rateeID uuid.UUID, stars float64) error {
	if err := ValidateStars(stars); err != nil {
		return err
	}
	_, err := r.pg.Exec(ctx, `
INSERT INTO trip_ratings (trip_id, rater_kind, rater_id, ratee_kind, ratee_id, stars, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now(), now())
ON CONFLICT (trip_id, rater_kind, ratee_kind, ratee_id) DO UPDATE SET
  stars = EXCLUDED.stars,
  ratee_kind = EXCLUDED.ratee_kind,
  ratee_id = EXCLUDED.ratee_id,
  updated_at = now()
`, tripID, raterKind, raterID, rateeKind, rateeID, stars)
	if err != nil {
		return err
	}

	// Mirror to trips table for history/reporting
	rk := strings.ToLower(strings.TrimSpace(raterKind))
	ek := strings.ToLower(strings.TrimSpace(rateeKind))
	starsInt := int(math.Round(stars)) // Extended ratings use INTEGER in trips table
	dispatcherRole := dispatcherRateeRoleUnknown
	if rk == "driver" && ek == "dispatcher" {
		dispatcherRole, err = r.resolveDispatcherRateeRole(ctx, tripID, rateeID)
		if err != nil {
			return err
		}
	}
	for _, column := range tripMirrorTargets(rk, ek, dispatcherRole) {
		switch column {
		case "rating_from_driver":
			_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_from_driver = $1, updated_at = now() WHERE id = $2`, stars, tripID)
		case "rating_driver_to_dm":
			_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_driver_to_dm = $1, updated_at = now() WHERE id = $2`, starsInt, tripID)
		case "rating_dm_to_driver":
			_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_dm_to_driver = $1, updated_at = now() WHERE id = $2`, starsInt, tripID)
		case "rating_dm_to_cm":
			_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_dm_to_cm = $1, updated_at = now() WHERE id = $2`, starsInt, tripID)
		case "rating_cm_to_dm":
			_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_cm_to_dm = $1, updated_at = now() WHERE id = $2`, starsInt, tripID)
		case "rating_from_dispatcher":
			_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_from_dispatcher = $1, updated_at = now() WHERE id = $2`, stars, tripID)
		}
	}

	return r.syncRateeProfileRating(ctx, rateeKind, rateeID)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (r *Repo) syncRateeProfileRating(ctx context.Context, rateeKind string, rateeID uuid.UUID) error {
	rows, err := r.pg.Query(ctx, `
SELECT stars FROM trip_ratings
WHERE ratee_kind = $1 AND ratee_id = $2
ORDER BY created_at ASC, id ASC
`, rateeKind, rateeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []float64
	for rows.Next() {
		var s float64
		if err := rows.Scan(&s); err != nil {
			return err
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}
	rating := FoldTripRatingsToProfile(list)
	switch rateeKind {
	case "driver":
		_, err = r.pg.Exec(ctx, `UPDATE drivers SET rating = $1, updated_at = now() WHERE id = $2`, rating, rateeID)
		return err
	case "dispatcher":
		_, err = r.pg.Exec(ctx, `UPDATE freelance_dispatchers SET rating = $1, updated_at = now() WHERE id = $2`, rating, rateeID)
		return err
	default:
		return fmt.Errorf("triprating: unknown ratee_kind %q", rateeKind)
	}
}

func (r *Repo) GetByTripAndRater(ctx context.Context, tripID uuid.UUID, raterKind string) (stars float64, ok bool, err error) {
	err = r.pg.QueryRow(ctx, `
SELECT stars FROM trip_ratings WHERE trip_id = $1 AND rater_kind = $2
`, tripID, raterKind).Scan(&stars)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return stars, true, nil
}

// TripCompleted returns true if trip row exists and status is COMPLETED.
func (r *Repo) TripCompleted(ctx context.Context, tripID uuid.UUID) (bool, error) {
	var st string
	err := r.pg.QueryRow(ctx, `SELECT status FROM trips WHERE id = $1`, tripID).Scan(&st)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st == "COMPLETED", nil
}

// TripDriverFinished returns true when driver side is finished:
// either full COMPLETED, or DELIVERED with driver completion request pending manager confirmation.
func (r *Repo) TripDriverFinished(ctx context.Context, tripID uuid.UUID) (bool, error) {
	var st string
	var pendingTo *string
	var driverConfirmedAt *string
	err := r.pg.QueryRow(ctx, `SELECT status, pending_confirm_to, driver_confirmed_at::text FROM trips WHERE id = $1`, tripID).Scan(&st, &pendingTo, &driverConfirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if st == "COMPLETED" {
		return true, nil
	}
	if st == "DELIVERED" &&
		pendingTo != nil &&
		driverConfirmedAt != nil &&
		strings.EqualFold(strings.TrimSpace(*pendingTo), "COMPLETED") {
		return true, nil
	}
	return false, nil
}
