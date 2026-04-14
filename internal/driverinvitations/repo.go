package driverinvitations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Stored invitation status (DB). EffectiveStatus may report "expired" for pending rows past expires_at.
const (
	StatusPending   = "pending"
	StatusAccepted  = "accepted"
	StatusDeclined  = "declined"
	StatusCancelled = "cancelled"
	// StatusExpired is only returned by EffectiveStatus, not stored.
	StatusExpired = "expired"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

// sqlNormPhone matches drivers table lookup: trim, strip spaces, dashes, and plus (see drivers.FindByPhoneNormalized).
const (
	sqlNormPhoneCol  = `replace(replace(replace(trim(phone), ' ', ''), '-', ''), '+', '')`
	sqlNormPhoneArg1 = `replace(replace(replace(trim($1::text), ' ', ''), '-', ''), '+', '')`
	sqlNormPhoneArg2 = `replace(replace(replace(trim($2::text), ' ', ''), '-', ''), '+', '')`
)

// Create creates driver invitation by company (company_id set). invitedBy = dispatcher or company user id.
func (r *Repo) Create(ctx context.Context, companyID uuid.UUID, phone string, invitedBy uuid.UUID, expiresIn time.Duration) (token string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token = hex.EncodeToString(b)
	expiresAt := time.Now().Add(expiresIn)
	_, err = r.pg.Exec(ctx,
		`INSERT INTO driver_invitations (token, company_id, phone, invited_by, expires_at, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		token, companyID, phone, invitedBy, expiresAt, StatusPending)
	return token, err
}

// CreateForFreelance creates driver invitation by freelance dispatcher (no company). Driver will get freelancer_id on accept.
func (r *Repo) CreateForFreelance(ctx context.Context, dispatcherID uuid.UUID, phone string, expiresIn time.Duration) (token string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token = hex.EncodeToString(b)
	expiresAt := time.Now().Add(expiresIn)
	_, err = r.pg.Exec(ctx,
		`INSERT INTO driver_invitations (token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, status) VALUES ($1, NULL, $2, $3, $3, $4, $5)`,
		token, phone, dispatcherID, expiresAt, StatusPending)
	return token, err
}

// HasPendingFreelanceInvitation returns true if a non-expired pending freelance invitation exists for this dispatcher + phone.
func (r *Repo) HasPendingFreelanceInvitation(ctx context.Context, dispatcherID uuid.UUID, phone string) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx,
		`SELECT 1 FROM driver_invitations
		 WHERE status = $3 AND expires_at > now() AND company_id IS NULL
		   AND invited_by_dispatcher_id = $1
		   AND `+sqlNormPhoneCol+` = `+sqlNormPhoneArg2+`
		 LIMIT 1`,
		dispatcherID, phone, StatusPending).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HasPendingCompanyInvitation returns true if a non-expired pending company invitation exists for this company + phone.
func (r *Repo) HasPendingCompanyInvitation(ctx context.Context, companyID uuid.UUID, phone string) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx,
		`SELECT 1 FROM driver_invitations
		 WHERE status = $3 AND expires_at > now() AND company_id = $1
		   AND `+sqlNormPhoneCol+` = `+sqlNormPhoneArg2+`
		 LIMIT 1`,
		companyID, phone, StatusPending).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func scanInvitation(row pgx.Row) (*Invitation, error) {
	var i Invitation
	var companyID *uuid.UUID
	var invDispatcherID *uuid.UUID
	var respondedAt *time.Time
	err := row.Scan(&i.ID, &i.Token, &companyID, &i.Phone, &i.InvitedBy, &invDispatcherID, &i.ExpiresAt, &i.CreatedAt, &i.Status, &respondedAt)
	if err != nil {
		return nil, err
	}
	i.CompanyID = companyID
	i.InvitedByDispatcherID = invDispatcherID
	i.RespondedAt = respondedAt
	return &i, nil
}

// GetPendingByToken returns a pending, non-expired invitation for accept/decline/cancel flows.
func (r *Repo) GetPendingByToken(ctx context.Context, token string) (*Invitation, error) {
	row := r.pg.QueryRow(ctx,
		`SELECT id, token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, created_at, status, responded_at
		 FROM driver_invitations WHERE token = $1 AND status = $2 AND expires_at > now()`,
		token, StatusPending)
	inv, err := scanInvitation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return inv, nil
}

// Invitation is a driver_invitations row (dispatcher → driver).
type Invitation struct {
	ID                    uuid.UUID
	Token                 string
	CompanyID             *uuid.UUID // nil when invited by freelance dispatcher
	Phone                 string
	InvitedBy             uuid.UUID  // dispatcher user who sent the invite
	InvitedByDispatcherID *uuid.UUID // set when freelance dispatcher invites (no company)
	ExpiresAt             time.Time
	CreatedAt             time.Time
	Status                string
	RespondedAt           *time.Time
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

// SetStatusIfPending marks a pending non-expired invitation with a terminal status (accepted, declined, cancelled).
func (r *Repo) SetStatusIfPending(ctx context.Context, token string, newStatus string) (bool, error) {
	tag, err := r.pg.Exec(ctx,
		`UPDATE driver_invitations SET status = $2, responded_at = now()
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
		`DELETE FROM driver_invitations
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
		`UPDATE driver_invitations SET status = $2, responded_at = NULL WHERE token = $1 AND status = $3`,
		token, StatusPending, StatusAccepted)
	return err
}

// ListByPhone returns all invitations for the given phone (history + pending).
func (r *Repo) ListByPhone(ctx context.Context, phone string) ([]Invitation, error) {
	rows, err := r.pg.Query(ctx,
		`SELECT id, token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, created_at, status, responded_at
		 FROM driver_invitations WHERE `+sqlNormPhoneCol+` = `+sqlNormPhoneArg1+`
		 ORDER BY created_at DESC`,
		phone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Invitation
	for rows.Next() {
		var i Invitation
		var companyID, invDispID *uuid.UUID
		var respondedAt *time.Time
		if err := rows.Scan(&i.ID, &i.Token, &companyID, &i.Phone, &i.InvitedBy, &invDispID, &i.ExpiresAt, &i.CreatedAt, &i.Status, &respondedAt); err != nil {
			return nil, err
		}
		i.CompanyID = companyID
		i.InvitedByDispatcherID = invDispID
		i.RespondedAt = respondedAt
		list = append(list, i)
	}
	return list, rows.Err()
}

// ListByInvitedBy returns all invitations sent by the given dispatcher (company and freelance), including completed.
func (r *Repo) ListByInvitedBy(ctx context.Context, dispatcherID uuid.UUID) ([]Invitation, error) {
	rows, err := r.pg.Query(ctx,
		`SELECT id, token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, created_at, status, responded_at
		 FROM driver_invitations WHERE invited_by = $1
		 ORDER BY created_at DESC`,
		dispatcherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Invitation
	for rows.Next() {
		var i Invitation
		var companyID, invDispID *uuid.UUID
		var respondedAt *time.Time
		if err := rows.Scan(&i.ID, &i.Token, &companyID, &i.Phone, &i.InvitedBy, &invDispID, &i.ExpiresAt, &i.CreatedAt, &i.Status, &respondedAt); err != nil {
			return nil, err
		}
		i.CompanyID = companyID
		i.InvitedByDispatcherID = invDispID
		i.RespondedAt = respondedAt
		list = append(list, i)
	}
	return list, rows.Err()
}
