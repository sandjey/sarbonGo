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

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

// sqlNormPhone matches drivers table lookup: trim, strip spaces, dashes, and plus (see drivers.FindByPhoneNormalized).
const (
	sqlNormPhoneCol   = `replace(replace(replace(trim(phone), ' ', ''), '-', ''), '+', '')`
	sqlNormPhoneArg1  = `replace(replace(replace(trim($1::text), ' ', ''), '-', ''), '+', '')`
	sqlNormPhoneArg2  = `replace(replace(replace(trim($2::text), ' ', ''), '-', ''), '+', '')`
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
		`INSERT INTO driver_invitations (token, company_id, phone, invited_by, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		token, companyID, phone, invitedBy, expiresAt)
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
		`INSERT INTO driver_invitations (token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at) VALUES ($1, NULL, $2, $3, $3, $4)`,
		token, phone, dispatcherID, expiresAt)
	return token, err
}

// HasPendingFreelanceInvitation returns true if a non-expired freelance invitation exists for this dispatcher + phone.
func (r *Repo) HasPendingFreelanceInvitation(ctx context.Context, dispatcherID uuid.UUID, phone string) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx,
		`SELECT 1 FROM driver_invitations
		 WHERE expires_at > now() AND company_id IS NULL
		   AND invited_by_dispatcher_id = $1
		   AND `+sqlNormPhoneCol+` = `+sqlNormPhoneArg2+`
		 LIMIT 1`,
		dispatcherID, phone).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HasPendingCompanyInvitation returns true if a non-expired company invitation exists for this company + phone.
func (r *Repo) HasPendingCompanyInvitation(ctx context.Context, companyID uuid.UUID, phone string) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx,
		`SELECT 1 FROM driver_invitations
		 WHERE expires_at > now() AND company_id = $1
		   AND `+sqlNormPhoneCol+` = `+sqlNormPhoneArg2+`
		 LIMIT 1`,
		companyID, phone).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetByToken returns invitation if not expired.
func (r *Repo) GetByToken(ctx context.Context, token string) (*Invitation, error) {
	var i Invitation
	var companyID *uuid.UUID
	var invDispatcherID *uuid.UUID
	err := r.pg.QueryRow(ctx,
		`SELECT id, token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, created_at FROM driver_invitations WHERE token = $1`,
		token).Scan(&i.ID, &i.Token, &companyID, &i.Phone, &i.InvitedBy, &invDispatcherID, &i.ExpiresAt, &i.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if time.Now().After(i.ExpiresAt) {
		return nil, nil
	}
	i.CompanyID = companyID
	i.InvitedByDispatcherID = invDispatcherID
	return &i, nil
}

type Invitation struct {
	ID                     uuid.UUID
	Token                  string
	CompanyID              *uuid.UUID // nil when invited by freelance dispatcher
	Phone                  string
	InvitedBy              uuid.UUID
	InvitedByDispatcherID  *uuid.UUID // set when freelance dispatcher invites (no company)
	ExpiresAt              time.Time
	CreatedAt              time.Time
}

// Delete removes invitation after accept or decline.
func (r *Repo) Delete(ctx context.Context, token string) error {
	_, err := r.pg.Exec(ctx, `DELETE FROM driver_invitations WHERE token = $1`, token)
	return err
}

// ListByPhone returns non-expired invitations for the given phone (для водителя: список приглашений в чате).
func (r *Repo) ListByPhone(ctx context.Context, phone string) ([]Invitation, error) {
	rows, err := r.pg.Query(ctx,
		`SELECT id, token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, created_at
		 FROM driver_invitations WHERE expires_at > now() AND `+sqlNormPhoneCol+` = `+sqlNormPhoneArg1+`
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
		if err := rows.Scan(&i.ID, &i.Token, &companyID, &i.Phone, &i.InvitedBy, &invDispID, &i.ExpiresAt, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.CompanyID = companyID
		i.InvitedByDispatcherID = invDispID
		list = append(list, i)
	}
	return list, rows.Err()
}

// ListByInvitedBy returns non-expired invitations sent by the given dispatcher (company or freelance).
func (r *Repo) ListByInvitedBy(ctx context.Context, dispatcherID uuid.UUID) ([]Invitation, error) {
	rows, err := r.pg.Query(ctx,
		`SELECT id, token, company_id, phone, invited_by, invited_by_dispatcher_id, expires_at, created_at
		 FROM driver_invitations WHERE expires_at > now() AND invited_by = $1
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
		if err := rows.Scan(&i.ID, &i.Token, &companyID, &i.Phone, &i.InvitedBy, &invDispID, &i.ExpiresAt, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.CompanyID = companyID
		i.InvitedByDispatcherID = invDispID
		list = append(list, i)
	}
	return list, rows.Err()
}
