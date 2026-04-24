package calls

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

type CreateParams struct {
	ConversationID  *uuid.UUID
	CallerID        uuid.UUID
	CalleeID        uuid.UUID
	ClientRequestID *string
}

// CreateRinging creates a new call in RINGING state.
// If ClientRequestID is provided, the operation is idempotent per (caller_id, client_request_id).
func (r *Repo) CreateRinging(ctx context.Context, p CreateParams) (*Call, error) {
	if p.CallerID == uuid.Nil || p.CalleeID == uuid.Nil || p.CallerID == p.CalleeID {
		return nil, ErrForbidden
	}
	// Busy check: do not allow parallel ongoing calls for either party.
	if ok, _ := r.HasOngoing(ctx, p.CallerID); ok {
		return nil, ErrInvalidState
	}
	if ok, _ := r.HasOngoing(ctx, p.CalleeID); ok {
		return nil, ErrInvalidState
	}
	if p.ClientRequestID != nil && *p.ClientRequestID != "" {
		// Try insert; on unique violation fetch existing.
		var c Call
		err := r.pg.QueryRow(ctx, `
INSERT INTO calls (conversation_id, caller_id, callee_id, status, client_request_id)
VALUES ($1,$2,$3,'RINGING',$4)
ON CONFLICT (caller_id, client_request_id) DO UPDATE SET caller_id = calls.caller_id
RETURNING id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
`, p.ConversationID, p.CallerID, p.CalleeID, *p.ClientRequestID).Scan(
			&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
		)
		if err != nil {
			return nil, err
		}
		_ = r.LogEvent(ctx, c.ID, &p.CallerID, "call.created", map[string]any{"conversation_id": p.ConversationID})
		return &c, nil
	}
	var c Call
	err := r.pg.QueryRow(ctx, `
INSERT INTO calls (conversation_id, caller_id, callee_id, status)
VALUES ($1,$2,$3,'RINGING')
RETURNING id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
`, p.ConversationID, p.CallerID, p.CalleeID).Scan(
		&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
	)
	if err != nil {
		return nil, err
	}
	_ = r.LogEvent(ctx, c.ID, &p.CallerID, "call.created", map[string]any{"conversation_id": p.ConversationID})
	return &c, nil
}

// HasOngoing returns true if user is in any RINGING or ACTIVE call.
func (r *Repo) HasOngoing(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pg.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM calls
  WHERE (caller_id=$1 OR callee_id=$1)
    AND status IN ('RINGING','ACTIVE')
)
`, userID).Scan(&exists)
	return exists, err
}

// MarkMissedExpired transitions old RINGING calls to MISSED.
// Returns number of calls updated.
func (r *Repo) MarkMissedExpired(ctx context.Context, ringingTimeout time.Duration, limit int) (int64, error) {
	if ringingTimeout <= 0 {
		ringingTimeout = 30 * time.Second
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	// Update in batches to avoid long locks.
	cmd, err := r.pg.Exec(ctx, `
WITH to_update AS (
  SELECT id
  FROM calls
  WHERE status='RINGING' AND created_at < now() - ($1::int || ' seconds')::interval
  ORDER BY created_at ASC
  LIMIT $2
)
UPDATE calls
SET status='MISSED', ended_at=now(), ended_reason='timeout'
WHERE id IN (SELECT id FROM to_update)
`, int(ringingTimeout.Seconds()), limit)
	if err != nil {
		return 0, err
	}
	return cmd.RowsAffected(), nil
}

func (r *Repo) GetForUser(ctx context.Context, callID, userID uuid.UUID) (*Call, error) {
	var c Call
	err := r.pg.QueryRow(ctx, `
SELECT id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
FROM calls
WHERE id=$1 AND (caller_id=$2 OR callee_id=$2)
LIMIT 1
`, callID, userID).Scan(
		&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

type ListParams struct {
	UserID uuid.UUID
	Limit  int
}

func (r *Repo) ListForUser(ctx context.Context, p ListParams) ([]Call, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pg.Query(ctx, `
SELECT id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
FROM calls
WHERE caller_id=$1 OR callee_id=$1
ORDER BY created_at DESC
LIMIT $2
`, p.UserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		var c Call
		if err := rows.Scan(&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListForUserWithPeerName returns calls for user with counterparty display name.
func (r *Repo) ListForUserWithPeerName(ctx context.Context, p ListParams) ([]CallListItem, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pg.Query(ctx, `
SELECT
  c.id, c.conversation_id, c.caller_id, c.callee_id, c.status, c.created_at, c.started_at, c.ended_at, c.ended_by, c.ended_reason, c.client_request_id,
  COALESCE(
    NULLIF(TRIM(d.name), ''),
    NULLIF(TRIM(fd.name), ''),
    NULLIF(TRIM(a.name), ''),
    d.phone,
    fd.phone,
    ''
  ) AS peer_name
FROM calls c
LEFT JOIN drivers d ON d.id = (CASE WHEN c.caller_id = $1 THEN c.callee_id ELSE c.caller_id END)
LEFT JOIN freelance_dispatchers fd ON fd.id = (CASE WHEN c.caller_id = $1 THEN c.callee_id ELSE c.caller_id END) AND fd.deleted_at IS NULL
LEFT JOIN admins a ON a.id = (CASE WHEN c.caller_id = $1 THEN c.callee_id ELSE c.caller_id END)
WHERE c.caller_id = $1 OR c.callee_id = $1
ORDER BY c.created_at DESC
LIMIT $2
`, p.UserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CallListItem, 0, limit)
	for rows.Next() {
		var item CallListItem
		if err := rows.Scan(
			&item.ID, &item.ConversationID, &item.CallerID, &item.CalleeID, &item.Status, &item.CreatedAt, &item.StartedAt, &item.EndedAt, &item.EndedBy, &item.EndedReason, &item.ClientRequestID,
			&item.Name,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListOngoingForUser returns current RINGING/ACTIVE calls for participant.
func (r *Repo) ListOngoingForUser(ctx context.Context, userID uuid.UUID) ([]Call, error) {
	rows, err := r.pg.Query(ctx, `
SELECT id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
FROM calls
WHERE (caller_id=$1 OR callee_id=$1) AND status IN ('RINGING','ACTIVE')
ORDER BY created_at DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Call, 0, 8)
	for rows.Next() {
		var c Call
		if err := rows.Scan(&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EndIfActiveSystem ends ACTIVE call without participant action (server-side recovery).
func (r *Repo) EndIfActiveSystem(ctx context.Context, callID uuid.UUID, reason string) (bool, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "system_recovered"
	}
	cmd, err := r.pg.Exec(ctx, `
UPDATE calls
SET status='ENDED', ended_at=now(), ended_reason=$2
WHERE id=$1 AND status='ACTIVE'
`, callID, reason)
	if err != nil {
		return false, err
	}
	_ = r.LogEvent(ctx, callID, nil, "call.ended.system", map[string]any{"reason": reason})
	return cmd.RowsAffected() > 0, nil
}

// MissIfRingingSystem marks RINGING call as MISSED (server-side recovery).
func (r *Repo) MissIfRingingSystem(ctx context.Context, callID uuid.UUID, reason string) (bool, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "timeout"
	}
	cmd, err := r.pg.Exec(ctx, `
UPDATE calls
SET status='MISSED', ended_at=now(), ended_reason=$2
WHERE id=$1 AND status='RINGING'
`, callID, reason)
	if err != nil {
		return false, err
	}
	_ = r.LogEvent(ctx, callID, nil, "call.missed.system", map[string]any{"reason": reason})
	return cmd.RowsAffected() > 0, nil
}

func (r *Repo) Accept(ctx context.Context, callID, actorID uuid.UUID) (*Call, error) {
	now := time.Now()
	var c Call
	err := r.pg.QueryRow(ctx, `
UPDATE calls
SET status='ACTIVE', started_at=COALESCE(started_at, $3)
WHERE id=$1 AND callee_id=$2 AND status='RINGING'
RETURNING id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
`, callID, actorID, now).Scan(
		&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// either not participant or already transitioned
			_, gerr := r.GetForUser(ctx, callID, actorID)
			if gerr != nil {
				return nil, gerr
			}
			return nil, ErrInvalidState
		}
		return nil, err
	}
	_ = r.LogEvent(ctx, callID, &actorID, "call.accepted", nil)
	return &c, nil
}

func (r *Repo) Decline(ctx context.Context, callID, actorID uuid.UUID, reason string) (*Call, error) {
	if reason == "" {
		reason = "declined"
	}
	now := time.Now()
	var c Call
	err := r.pg.QueryRow(ctx, `
UPDATE calls
SET status='DECLINED', ended_at=$3, ended_by=$2, ended_reason=$4
WHERE id=$1 AND callee_id=$2 AND status='RINGING'
RETURNING id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
`, callID, actorID, now, reason).Scan(
		&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, gerr := r.GetForUser(ctx, callID, actorID)
			if gerr != nil {
				return nil, gerr
			}
			return nil, ErrInvalidState
		}
		return nil, err
	}
	_ = r.LogEvent(ctx, callID, &actorID, "call.declined", map[string]any{"reason": reason})
	return &c, nil
}

// Cancel lets caller cancel before callee accepted (RINGING -> CANCELLED).
func (r *Repo) Cancel(ctx context.Context, callID, actorID uuid.UUID) (*Call, error) {
	now := time.Now()
	var c Call
	err := r.pg.QueryRow(ctx, `
UPDATE calls
SET status='CANCELLED', ended_at=$3, ended_by=$2, ended_reason='cancelled'
WHERE id=$1 AND caller_id=$2 AND status='RINGING'
RETURNING id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
`, callID, actorID, now).Scan(
		&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, gerr := r.GetForUser(ctx, callID, actorID)
			if gerr != nil {
				return nil, gerr
			}
			return nil, ErrInvalidState
		}
		return nil, err
	}
	_ = r.LogEvent(ctx, callID, &actorID, "call.cancelled", nil)
	return &c, nil
}

// End ends an ACTIVE call for either participant.
func (r *Repo) End(ctx context.Context, callID, actorID uuid.UUID, reason string) (*Call, error) {
	if reason == "" {
		reason = "ended"
	}
	now := time.Now()
	var c Call
	err := r.pg.QueryRow(ctx, `
UPDATE calls
SET status='ENDED', ended_at=$3, ended_by=$2, ended_reason=$4
WHERE id=$1 AND (caller_id=$2 OR callee_id=$2) AND status='ACTIVE'
RETURNING id, conversation_id, caller_id, callee_id, status, created_at, started_at, ended_at, ended_by, ended_reason, client_request_id
`, callID, actorID, now, reason).Scan(
		&c.ID, &c.ConversationID, &c.CallerID, &c.CalleeID, &c.Status, &c.CreatedAt, &c.StartedAt, &c.EndedAt, &c.EndedBy, &c.EndedReason, &c.ClientRequestID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, gerr := r.GetForUser(ctx, callID, actorID)
			if gerr != nil {
				return nil, gerr
			}
			return nil, ErrInvalidState
		}
		return nil, err
	}
	_ = r.LogEvent(ctx, callID, &actorID, "call.ended", map[string]any{"reason": reason})
	return &c, nil
}

func (r *Repo) LogEvent(ctx context.Context, callID uuid.UUID, actorID *uuid.UUID, eventType string, payload any) error {
	var payloadJSON []byte
	if payload != nil {
		payloadJSON, _ = json.Marshal(payload)
	}
	_, err := r.pg.Exec(ctx, `INSERT INTO call_events (call_id, actor_id, event_type, payload) VALUES ($1,$2,$3,$4)`, callID, actorID, eventType, payloadJSON)
	return err
}

