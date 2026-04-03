package displaynames

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Checker enforces global display-name uniqueness between drivers and freelance dispatchers (case-insensitive).
type Checker struct {
	pg *pgxpool.Pool
}

func NewChecker(pg *pgxpool.Pool) *Checker {
	return &Checker{pg: pg}
}

// IsTaken reports whether name is already used by another driver or dispatcher (excluding deleted dispatchers).
// excludeDriverID / excludeDispatcherID skip the current row when updating a profile.
func (c *Checker) IsTaken(ctx context.Context, name string, excludeDriverID, excludeDispatcherID *uuid.UUID) (bool, error) {
	var exD, exP any
	if excludeDriverID != nil {
		exD = *excludeDriverID
	}
	if excludeDispatcherID != nil {
		exP = *excludeDispatcherID
	}
	var taken bool
	err := c.pg.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM drivers
			WHERE name IS NOT NULL AND btrim(name) <> ''
			  AND lower(name) = lower($1)
			  AND ($2::uuid IS NULL OR id <> $2)
			UNION ALL
			SELECT 1 FROM freelance_dispatchers
			WHERE deleted_at IS NULL
			  AND name IS NOT NULL AND btrim(name) <> ''
			  AND lower(name) = lower($1)
			  AND ($3::uuid IS NULL OR id <> $3)
		)`, name, exD, exP).Scan(&taken)
	return taken, err
}
