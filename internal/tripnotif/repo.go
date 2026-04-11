package tripnotif

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

func (r *Repo) Insert(ctx context.Context, tripID uuid.UUID, recipientKind string, recipientID uuid.UUID, eventKind string, fromStatus, toStatus *string) error {
	_, err := r.pg.Exec(ctx, `
INSERT INTO trip_user_notifications (trip_id, recipient_kind, recipient_id, event_kind, from_status, to_status)
VALUES ($1, $2, $3, $4, $5, $6)
`, tripID, recipientKind, recipientID, eventKind, fromStatus, toStatus)
	return err
}

func (r *Repo) List(ctx context.Context, recipientKind string, recipientID uuid.UUID, unreadOnly bool, limit int) ([]Row, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	q := `
SELECT id, trip_id, recipient_kind, recipient_id, event_kind, from_status, to_status, read_at, created_at
FROM trip_user_notifications
WHERE recipient_kind = $1 AND recipient_id = $2`
	args := []any{recipientKind, recipientID}
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT $3`
	args = append(args, limit)

	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var x Row
		if err := rows.Scan(&x.ID, &x.TripID, &x.RecipientKind, &x.RecipientID, &x.EventKind, &x.FromStatus, &x.ToStatus, &x.ReadAt, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *Repo) MarkRead(ctx context.Context, recipientKind string, recipientID, notificationID uuid.UUID) (bool, error) {
	tag, err := r.pg.Exec(ctx, `
UPDATE trip_user_notifications
SET read_at = now()
WHERE id = $1 AND recipient_kind = $2 AND recipient_id = $3 AND read_at IS NULL
`, notificationID, recipientKind, recipientID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) MarkAllRead(ctx context.Context, recipientKind string, recipientID uuid.UUID) (int64, error) {
	tag, err := r.pg.Exec(ctx, `
UPDATE trip_user_notifications
SET read_at = now()
WHERE recipient_kind = $1 AND recipient_id = $2 AND read_at IS NULL
`, recipientKind, recipientID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountUnread returns unread count for badge.
func (r *Repo) CountUnread(ctx context.Context, recipientKind string, recipientID uuid.UUID) (int64, error) {
	var n int64
	err := r.pg.QueryRow(ctx, `
SELECT COUNT(*) FROM trip_user_notifications
WHERE recipient_kind = $1 AND recipient_id = $2 AND read_at IS NULL
`, recipientKind, recipientID).Scan(&n)
	return n, err
}
