package drivertodispatcherinvitations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Stored invitation status. EffectiveStatus may report "expired" for pending rows past expires_at.
const (
	StatusPending   = "pending"
	StatusAccepted  = "accepted"
	StatusDeclined  = "declined"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired" // only from EffectiveStatus, not stored
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

func normalizePhone(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", ""))
	return strings.TrimPrefix(s, "+")
}

// Create creates an invitation from driver to dispatcher (by dispatcher phone). Returns token.
func (r *Repo) Create(ctx context.Context, driverID uuid.UUID, dispatcherPhone string, expiresIn time.Duration) (token string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token = hex.EncodeToString(b)
	expiresAt := time.Now().Add(expiresIn)
	_, err = r.pg.Exec(ctx,
		`INSERT INTO driver_to_dispatcher_invitations (token, driver_id, dispatcher_phone, expires_at, status) VALUES ($1, $2, $3, $4, $5)`,
		token, driverID, strings.TrimSpace(dispatcherPhone), expiresAt, StatusPending)
	return token, err
}

// Invitation is a driver_to_dispatcher_invitations row.
type Invitation struct {
	ID              uuid.UUID
	Token           string
	DriverID        uuid.UUID
	DispatcherPhone string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	Status          string
	RespondedAt     *time.Time
}

// EffectiveStatus returns display status: pending rows past expires_at count as expired.
func EffectiveStatus(inv Invitation) string {
	if inv.Status != StatusPending {
		return inv.Status
	}
	if time.Now().After(inv.ExpiresAt) {
		return StatusExpired
	}
	return StatusPending
}

// GetPendingByToken returns a pending, non-expired invitation for accept/decline/cancel flows.
func (r *Repo) GetPendingByToken(ctx context.Context, token string) (*Invitation, error) {
	var i Invitation
	var respondedAt *time.Time
	err := r.pg.QueryRow(ctx,
		`SELECT id, token, driver_id, dispatcher_phone, expires_at, created_at, status, responded_at
		 FROM driver_to_dispatcher_invitations
		 WHERE token = $1 AND status = $2 AND expires_at > now()`,
		token, StatusPending).Scan(&i.ID, &i.Token, &i.DriverID, &i.DispatcherPhone, &i.ExpiresAt, &i.CreatedAt, &i.Status, &respondedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	i.RespondedAt = respondedAt
	return &i, nil
}

// SetStatusIfPending marks a pending non-expired invitation with a terminal status.
func (r *Repo) SetStatusIfPending(ctx context.Context, token string, newStatus string) (bool, error) {
	tag, err := r.pg.Exec(ctx,
		`UPDATE driver_to_dispatcher_invitations SET status = $2, responded_at = now()
		 WHERE token = $1 AND status = $3 AND expires_at > now()`,
		token, newStatus, StatusPending)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// DeletePendingByToken removes a pending non-expired invitation row.
func (r *Repo) DeletePendingByToken(ctx context.Context, token string) (bool, error) {
	tag, err := r.pg.Exec(ctx,
		`DELETE FROM driver_to_dispatcher_invitations
		 WHERE token = $1 AND status = $2 AND expires_at > now()`,
		token, StatusPending)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RevertToPending undoes an accepted mark if the follow-up driver update failed.
func (r *Repo) RevertToPending(ctx context.Context, token string) error {
	_, err := r.pg.Exec(ctx,
		`UPDATE driver_to_dispatcher_invitations SET status = $2, responded_at = NULL WHERE token = $1 AND status = $3`,
		token, StatusPending, StatusAccepted)
	return err
}

// ListByDriverID returns all invitations sent by this driver (history + pending).
func (r *Repo) ListByDriverID(ctx context.Context, driverID uuid.UUID) ([]Invitation, error) {
	rows, err := r.pg.Query(ctx,
		`SELECT id, token, driver_id, dispatcher_phone, expires_at, created_at, status, responded_at
		 FROM driver_to_dispatcher_invitations WHERE driver_id = $1 ORDER BY created_at DESC`,
		driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Invitation
	for rows.Next() {
		var i Invitation
		var respondedAt *time.Time
		if err := rows.Scan(&i.ID, &i.Token, &i.DriverID, &i.DispatcherPhone, &i.ExpiresAt, &i.CreatedAt, &i.Status, &respondedAt); err != nil {
			return nil, err
		}
		i.RespondedAt = respondedAt
		list = append(list, i)
	}
	return list, rows.Err()
}

// ListByDispatcherPhone returns all invitations sent TO this dispatcher phone (normalized match).
func (r *Repo) ListByDispatcherPhone(ctx context.Context, dispatcherPhone string) ([]Invitation, error) {
	norm := normalizePhone(dispatcherPhone)
	if norm == "" {
		return []Invitation{}, nil
	}
	rows, err := r.pg.Query(ctx,
		`SELECT id, token, driver_id, dispatcher_phone, expires_at, created_at, status, responded_at
		 FROM driver_to_dispatcher_invitations
		 WHERE replace(replace(replace(trim(dispatcher_phone), ' ', ''), '-', ''), '+', '') = $1
		 ORDER BY created_at DESC`,
		norm)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Invitation
	for rows.Next() {
		var i Invitation
		var respondedAt *time.Time
		if err := rows.Scan(&i.ID, &i.Token, &i.DriverID, &i.DispatcherPhone, &i.ExpiresAt, &i.CreatedAt, &i.Status, &respondedAt); err != nil {
			return nil, err
		}
		i.RespondedAt = respondedAt
		list = append(list, i)
	}
	return list, rows.Err()
}

// PhoneMatches returns true if invitation's dispatcher_phone matches the given phone (normalized).
func (i *Invitation) PhoneMatches(phone string) bool {
	return normalizePhone(i.DispatcherPhone) == normalizePhone(phone)
}
