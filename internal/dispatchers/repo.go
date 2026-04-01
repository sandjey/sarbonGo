package dispatchers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"sarbonNew/internal/util"
)

var (
	ErrNotFound              = errors.New("dispatcher not found")
	ErrPhoneAlreadyRegistered = errors.New("phone already registered")
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

type CatalogFilter struct {
	Q          string  // name/phone search (ILIKE)
	PhoneHint  string  // phone prefix hint (LIKE prefix%)
	Status     *string // account_status
	WorkStatus *string
	HasPhoto   *bool
	RatingMin  *float64
	RatingMax  *float64
	Limit      int
	Offset     int
}

// ListCatalog returns all active (not deleted) freelance dispatchers with filters + pagination.
func (r *Repo) ListCatalog(ctx context.Context, f CatalogFilter) (items []Dispatcher, total int64, err error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	where := `WHERE deleted_at IS NULL`
	args := make([]any, 0, 12)
	argN := 1

	add := func(cond string, v any) {
		where += " AND " + cond
		args = append(args, v)
		argN++
	}

	if f.Q != "" {
		// match by phone or name (case-insensitive)
		add(`(phone ILIKE $`+itoa(argN)+` OR COALESCE(name,'') ILIKE $`+itoa(argN)+`)`, "%"+f.Q+"%")
	}
	if f.PhoneHint != "" {
		add(`phone LIKE $`+itoa(argN)+``, f.PhoneHint+"%")
	}
	if f.Status != nil && *f.Status != "" {
		add(`account_status = $`+itoa(argN)+``, *f.Status)
	}
	if f.WorkStatus != nil && *f.WorkStatus != "" {
		add(`work_status = $`+itoa(argN)+``, *f.WorkStatus)
	}
	if f.HasPhoto != nil {
		if *f.HasPhoto {
			where += " AND photo_data IS NOT NULL"
		} else {
			where += " AND photo_data IS NULL"
		}
	}
	if f.RatingMin != nil {
		add(`COALESCE(rating,0) >= $`+itoa(argN)+``, *f.RatingMin)
	}
	if f.RatingMax != nil {
		add(`COALESCE(rating,0) <= $`+itoa(argN)+``, *f.RatingMax)
	}

	// Add pagination args
	args = append(args, limit, offset)

	const sel = `
SELECT
  id, name, phone, '' as password,
  passport_series, passport_number, pinfl,
  cargo_id, driver_id,
  rating, work_status, account_status AS status,
  photo_path AS photo,
  (photo_data IS NOT NULL) AS has_photo,
  created_at, updated_at, last_online_at, deleted_at,
  COUNT(*) OVER() AS total
FROM freelance_dispatchers
%s
ORDER BY last_online_at DESC NULLS LAST, created_at DESC
LIMIT $%d OFFSET $%d`

	// last 2 args are limit/offset
	limitPos := len(args) - 1
	offsetPos := len(args)
	q := sprintf(sel, where, limitPos, offsetPos)

	rows, qerr := r.pg.Query(ctx, q, args...)
	if qerr != nil {
		return nil, 0, qerr
	}
	defer rows.Close()

	for rows.Next() {
		var d Dispatcher
		var dummyPassword string
		var tot int64
		if err := rows.Scan(
			&d.ID, &d.Name, &d.Phone, &dummyPassword,
			&d.PassportSeries, &d.PassportNumber, &d.PINFL,
			&d.CargoID, &d.DriverID,
			&d.Rating, &d.WorkStatus, &d.Status,
			&d.Photo,
			&d.HasPhoto,
			&d.CreatedAt, &d.UpdatedAt, &d.LastOnlineAt, &d.DeletedAt,
			&tot,
		); err != nil {
			return nil, 0, err
		}
		// normalize tz
		d.CreatedAt = util.InTashkent(d.CreatedAt)
		d.UpdatedAt = util.InTashkent(d.UpdatedAt)
		if d.LastOnlineAt != nil {
			v := util.InTashkent(*d.LastOnlineAt)
			d.LastOnlineAt = &v
		}
		if d.DeletedAt != nil {
			v := util.InTashkent(*d.DeletedAt)
			d.DeletedAt = &v
		}
		total = tot
		items = append(items, d)
	}
	return items, total, rows.Err()
}

// HintByPhonePrefix returns up to limit dispatchers whose phone starts with prefix.
func (r *Repo) HintByPhonePrefix(ctx context.Context, prefix string, limit int) ([]Dispatcher, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	const q = `
SELECT
  id, name, phone, password,
  passport_series, passport_number, pinfl,
  cargo_id, driver_id,
  rating, work_status, account_status AS status,
  photo_path AS photo,
  (photo_data IS NOT NULL) AS has_photo,
  created_at, updated_at, last_online_at, deleted_at
FROM freelance_dispatchers
WHERE deleted_at IS NULL AND phone LIKE $1
ORDER BY last_online_at DESC NULLS LAST, created_at DESC
LIMIT $2`
	rows, err := r.pg.Query(ctx, q, prefix+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Dispatcher, 0, limit)
	for rows.Next() {
		var d Dispatcher
		if err := rows.Scan(
			&d.ID, &d.Name, &d.Phone, &d.Password,
			&d.PassportSeries, &d.PassportNumber, &d.PINFL,
			&d.CargoID, &d.DriverID,
			&d.Rating, &d.WorkStatus, &d.Status,
			&d.Photo, &d.HasPhoto,
			&d.CreatedAt, &d.UpdatedAt, &d.LastOnlineAt, &d.DeletedAt,
		); err != nil {
			return nil, err
		}
		// never expose password
		d.Password = ""
		d.CreatedAt = util.InTashkent(d.CreatedAt)
		d.UpdatedAt = util.InTashkent(d.UpdatedAt)
		if d.LastOnlineAt != nil {
			v := util.InTashkent(*d.LastOnlineAt)
			d.LastOnlineAt = &v
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) FindByPhone(ctx context.Context, phone string) (*Dispatcher, error) {
	const q = `
SELECT
  id, name, phone, password,
  passport_series, passport_number, pinfl,
  cargo_id, driver_id,
  rating, work_status, account_status AS status,
  photo_path AS photo,
  (photo_data IS NOT NULL) AS has_photo,
  created_at, updated_at, last_online_at, deleted_at
FROM freelance_dispatchers
WHERE phone = $1 AND deleted_at IS NULL
LIMIT 1`
	d, err := scanDispatcher(r.pg.QueryRow(ctx, q, phone))
	if err != nil {
		return nil, err
	}
	return d, nil
}

// small helpers
func itoa(n int) string { return strconv.Itoa(n) }
func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

func (r *Repo) FindByID(ctx context.Context, id uuid.UUID) (*Dispatcher, error) {
	const q = `
SELECT
  id, name, phone, password,
  passport_series, passport_number, pinfl,
  cargo_id, driver_id,
  rating, work_status, account_status AS status,
  photo_path AS photo,
  (photo_data IS NOT NULL) AS has_photo,
  created_at, updated_at, last_online_at, deleted_at
FROM freelance_dispatchers
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1`
	d, err := scanDispatcher(r.pg.QueryRow(ctx, q, id))
	if err != nil {
		return nil, err
	}
	return d, nil
}

func scanDispatcher(row pgx.Row) (*Dispatcher, error) {
	var d Dispatcher
	err := row.Scan(
		&d.ID, &d.Name, &d.Phone, &d.Password,
		&d.PassportSeries, &d.PassportNumber, &d.PINFL,
		&d.CargoID, &d.DriverID,
		&d.Rating, &d.WorkStatus, &d.Status,
		&d.Photo,
		&d.HasPhoto,
		&d.CreatedAt, &d.UpdatedAt, &d.LastOnlineAt, &d.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	d.CreatedAt = util.InTashkent(d.CreatedAt)
	d.UpdatedAt = util.InTashkent(d.UpdatedAt)
	if d.LastOnlineAt != nil {
		v := util.InTashkent(*d.LastOnlineAt)
		d.LastOnlineAt = &v
	}
	if d.DeletedAt != nil {
		v := util.InTashkent(*d.DeletedAt)
		d.DeletedAt = &v
	}

	return &d, nil
}

type CreateParams struct {
	Phone          string
	Name           string
	PasswordHash   string
	PassportSeries string
	PassportNumber string
	PINFL          string
}

func (r *Repo) Create(ctx context.Context, p CreateParams) (uuid.UUID, error) {
	const q = `
INSERT INTO freelance_dispatchers (
  phone, name, password,
  passport_series, passport_number, pinfl,
  rating, work_status, account_status,
  created_at, updated_at, deleted_at
) VALUES (
  $1, $2, $3,
  $4, $5, $6,
  0, 'available', 'active',
  now(), now(), NULL
) RETURNING id`

	var id uuid.UUID
	err := r.pg.QueryRow(ctx, q,
		p.Phone, p.Name, p.PasswordHash,
		p.PassportSeries, p.PassportNumber, p.PINFL,
	).Scan(&id)
	if err != nil {
		if e, ok := err.(*pgconn.PgError); ok && e.SQLState() == "23505" {
			return uuid.Nil, ErrPhoneAlreadyRegistered
		}
		return uuid.Nil, err
	}
	return id, nil
}

type UpdateProfileParams struct {
	Name           *string
	PassportSeries *string
	PassportNumber *string
	PINFL          *string
	Photo          *string
}

func (r *Repo) UpdateProfile(ctx context.Context, id uuid.UUID, p UpdateProfileParams) error {
	const q = `
UPDATE freelance_dispatchers
SET name = COALESCE($2, name),
    passport_series = COALESCE($3, passport_series),
    passport_number = COALESCE($4, passport_number),
    pinfl = COALESCE($5, pinfl),
    photo_path = COALESCE($6, photo_path),
    updated_at = now()
WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id, p.Name, p.PassportSeries, p.PassportNumber, p.PINFL, p.Photo)
	return err
}

func (r *Repo) UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	const q = `UPDATE freelance_dispatchers SET password = $2, updated_at = now() WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id, passwordHash)
	return err
}

func (r *Repo) UpdatePhone(ctx context.Context, id uuid.UUID, newPhone string) error {
	const q = `UPDATE freelance_dispatchers SET phone = $2, updated_at = now() WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id, newPhone)
	if err != nil {
		if e, ok := err.(*pgconn.PgError); ok && e.SQLState() == "23505" {
			return ErrPhoneAlreadyRegistered
		}
		return err
	}
	return nil
}

var ErrDeleteNotFound = errors.New("dispatcher to delete not found")

func (r *Repo) DeleteAndArchive(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE freelance_dispatchers SET deleted_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `INSERT INTO deleted_freelance_dispatchers SELECT * FROM freelance_dispatchers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDeleteNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM freelance_dispatchers WHERE id = $1`, id); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Repo) Touch(ctx context.Context, id uuid.UUID, t time.Time) error {
	const q = `UPDATE freelance_dispatchers SET updated_at = $2 WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id, t)
	return err
}

// UpdateLastOnlineAt обновляет last_online_at диспетчера (при каждом запросе).
func (r *Repo) UpdateLastOnlineAt(ctx context.Context, id uuid.UUID, t time.Time) error {
	const q = `UPDATE freelance_dispatchers SET last_online_at = $2, updated_at = now() WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id, t)
	return err
}

// UpdatePhoto сохраняет фото диспетчера в БД (бинарные данные + content-type).
func (r *Repo) UpdatePhoto(ctx context.Context, id uuid.UUID, data []byte, contentType string) error {
	const q = `UPDATE freelance_dispatchers SET photo_data = $2, photo_content_type = $3, updated_at = now() WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id, data, contentType)
	return err
}

// GetPhoto возвращает фото диспетчера (данные и content-type). Если фото нет — ErrNotFound.
func (r *Repo) GetPhoto(ctx context.Context, id uuid.UUID) (data []byte, contentType string, err error) {
	const q = `SELECT photo_data, COALESCE(photo_content_type, 'image/jpeg') FROM freelance_dispatchers WHERE id = $1 AND deleted_at IS NULL AND photo_data IS NOT NULL`
	err = r.pg.QueryRow(ctx, q, id).Scan(&data, &contentType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	return data, contentType, nil
}

func (r *Repo) DeletePhoto(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE freelance_dispatchers SET photo_data = NULL, photo_content_type = NULL, updated_at = now() WHERE id = $1`
	_, err := r.pg.Exec(ctx, q, id)
	return err
}
