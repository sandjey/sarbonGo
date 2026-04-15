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

// Upsert inserts or updates the single rating for (trip_id, rater_kind).
func (r *Repo) Upsert(ctx context.Context, tripID uuid.UUID, raterKind string, raterID uuid.UUID, rateeKind string, rateeID uuid.UUID, stars float64) error {
	if err := ValidateStars(stars); err != nil {
		return err
	}
	_, err := r.pg.Exec(ctx, `
INSERT INTO trip_ratings (trip_id, rater_kind, rater_id, ratee_kind, ratee_id, stars, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now(), now())
ON CONFLICT (trip_id, rater_kind, ratee_kind) DO UPDATE SET
  stars = EXCLUDED.stars,
  ratee_kind = EXCLUDED.ratee_kind,
  ratee_id = EXCLUDED.ratee_id,
  updated_at = now()
`, tripID, raterKind, raterID, rateeKind, rateeID, stars)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(raterKind)) {
	case "driver":
		_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_from_driver = $1, updated_at = now() WHERE id = $2`, stars, tripID)
	case "dispatcher":
		_, _ = r.pg.Exec(ctx, `UPDATE trips SET rating_from_dispatcher = $1, updated_at = now() WHERE id = $2`, stars, tripID)
	}
	return r.syncRateeProfileRating(ctx, rateeKind, rateeID)
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
