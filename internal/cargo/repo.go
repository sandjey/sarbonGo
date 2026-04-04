package cargo

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sarbonNew/internal/cargodrivers"
)

type Repo struct {
	pg *pgxpool.Pool
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

// tripStatusConsumesVehiclesLeft is true when vehicles_left was already decremented (trip reached LOADING or later).
func tripStatusConsumesVehiclesLeft(status string) bool {
	switch status {
	case "LOADING", "EN_ROUTE", "UNLOADING":
		return true
	default:
		return false
	}
}

func (r *Repo) CompanyExists(ctx context.Context, companyID uuid.UUID) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx, `SELECT 1 FROM companies WHERE id = $1 AND deleted_at IS NULL`, companyID).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *Repo) CargoTypeExists(ctx context.Context, cargoTypeID uuid.UUID) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx, `SELECT 1 FROM cargo_types WHERE id = $1`, cargoTypeID).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// MaxCargoExportRows — максимум строк в одном Excel-экспорте диспетчера (без пагинации).
const MaxCargoExportRows = 20000

// ErrCargoExportTooManyRows возвращается, если отфильтрованных грузов больше MaxCargoExportRows.
var ErrCargoExportTooManyRows = errors.New("cargo export: too many rows")

// ListFilter for GET /api/cargo.
type ListFilter struct {
	Status             []string   // status=SEARCHING_ALL,SEARCHING_COMPANY
	ForDriverCompanyID *uuid.UUID // when set, "searching" filter shows SEARCHING_ALL + SEARCHING_COMPANY for this company only
	CreatedByDispatcherID *uuid.UUID // only cargo created by this dispatcher (export / «мои грузы»)
	CompanyID          *uuid.UUID // optional: only cargo for this company_id (marketplace filter)
	NameContains       string     // optional: ILIKE substring on c.name (from q=)
	WeightMin          *float64
	WeightMax          *float64
	TruckType          string
	CreatedFrom        string // YYYY-MM-DD
	CreatedTo          string
	WithOffers         *bool   // only cargo that have at least one offer
	Page               int
	Limit              int
	Sort               string // "created_at:desc" or "created_at:asc"
}

// ListResult for paginated list.
type ListResult struct {
	Items []Cargo
	Total int
}

// CreateParams for creating cargo with route points and payment.
type CreateParams struct {
	// Шаг 1 — Груз
	Name       *string
	Weight     float64  `validate:"required,gt=0"`
	Volume     float64
	VehiclesAmount int `validate:"required,gt=0"` // Количество машин
	Packaging  *string  // Упаковка
	Dimensions *string  // Габариты
	Photos     []string // Фото (max 5, каждая ≤10MB)

	// Шаг 2 — Готовность
	ReadyEnabled bool
	ReadyAt      *string
	Comment      *string

	// Шаг 3 — Транспорт
	TruckType        string        `validate:"required"`
	PowerPlateType   string        `validate:"required"` // TRUCK|TRACTOR (GET /v1/driver/transport-options)
	TrailerPlateType string        `validate:"required"` // depends on PowerPlateType
	TempMin          *float64
	TempMax          *float64
	ADREnabled       bool
	ADRClass         *string       `validate:"required_if=ADREnabled true"`
	LoadingTypes     []string
	ShipmentType     *ShipmentType
	BeltsCount       *int
	Documents        *Documents // TIR, T1, CMR, Medbook, GLONASS, Seal, Permit

	// Контакты
	ContactName  *string `validate:"required"`
	ContactPhone *string `validate:"required"`

	// Статус
	Status CargoStatus

	// Маршрут
	RoutePoints []RoutePointInput `validate:"min=2,dive"` // минимум 2 точки

	// Оплата
	Payment *PaymentInput

	// Системные (из JWT)
	CreatedByType *string
	CreatedByID   *uuid.UUID
	CompanyID     *uuid.UUID

	// Тип груза
	CargoTypeID *uuid.UUID
}

type RoutePointInput struct {
	Type         string  `validate:"required,oneof=load unload customs transit"`
	CountryCode  string
	CityCode     string
	RegionCode   string
	Address      string  `validate:"required"`
	Orientir     string
	Lat          float64 `validate:"required_with=Address"`
	Lng          float64 `validate:"required_with=Address"`
	PlaceID      *string // ID от карт для автокомплита
	Comment      *string
	PointOrder   int  `validate:"required,min=1"`
	IsMainLoad   bool // Первая точка load?
	IsMainUnload bool // Последняя точка unload?
	// PointAt — плановое время в UTC (хранится как timestamptz).
	PointAt *time.Time
}

type PaymentInput struct {
	IsNegotiable       bool
	PriceRequest       bool
	TotalAmount        *float64
	TotalCurrency      *string
	WithPrepayment     bool
	PrepaymentAmount   *float64
	PrepaymentCurrency *string
	PrepaymentType     *string
	RemainingAmount    *float64
	RemainingCurrency  *string
	RemainingType      *string
}

// Create creates cargo, route_points and payment in a transaction.
func (r *Repo) Create(ctx context.Context, p CreateParams) (uuid.UUID, error) {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	docJSON, _ := DocumentsToJSON(p.Documents)
	var id uuid.UUID
	q := `
INSERT INTO cargo (
  name, weight, volume, vehicles_amount, vehicles_left,
  packaging, dimensions, photo_urls,
  ready_enabled, ready_at, load_comment,
  truck_type, power_plate_type, trailer_plate_type,
  temp_min, temp_max, adr_enabled, adr_class,
  loading_types, shipment_type, belts_count,
  documents, contact_name, contact_phone,
  status, created_at, updated_at, deleted_at,
  created_by_type, created_by_id, company_id, cargo_type_id
)
-- NOTE: load_comment column is used as generic comment in API (field name: comment).
VALUES (
  $1, $2, $3, $4, $4,
  $5, $6, $7,
  $8, $9, $10,
  $11, $12, $13,
  $14, $15, $16, $17,
  $18, $19, $20,
  $21, $22, $23,
  COALESCE(NULLIF(TRIM($24),''), 'PENDING_MODERATION'), now(), now(), NULL,
  $25, $26, $27, $28
)
RETURNING id`
	err = tx.QueryRow(ctx, q,
		p.Name,
		p.Weight, p.Volume, p.VehiclesAmount,
		p.Packaging, p.Dimensions, p.Photos,
		p.ReadyEnabled, p.ReadyAt, p.Comment,
		p.TruckType, p.PowerPlateType, p.TrailerPlateType,
		p.TempMin, p.TempMax, p.ADREnabled, p.ADRClass,
		p.LoadingTypes, p.ShipmentType, p.BeltsCount,
		docJSON, p.ContactName, p.ContactPhone,
		p.Status,
		p.CreatedByType, p.CreatedByID, p.CompanyID, p.CargoTypeID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}

	for _, rp := range p.RoutePoints {
		_, err = tx.Exec(ctx, `
INSERT INTO route_points (cargo_id, type, country_code, city_code, region_code, address, orientir, lat, lng, place_id, comment, point_order, is_main_load, is_main_unload, point_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			id, rp.Type, emptyToNil(strings.ToUpper(strings.TrimSpace(rp.CountryCode))), emptyToNil(rp.CityCode), emptyToNil(rp.RegionCode), rp.Address, emptyToNil(rp.Orientir), rp.Lat, rp.Lng, rp.PlaceID, rp.Comment, rp.PointOrder, rp.IsMainLoad, rp.IsMainUnload, rp.PointAt)
		if err != nil {
			return uuid.Nil, err
		}
	}

	if p.Payment != nil {
		_, err = tx.Exec(ctx, `
INSERT INTO payments (
  cargo_id, is_negotiable, price_request,
  total_amount, total_currency,
  with_prepayment,
  prepayment_amount, prepayment_currency, prepayment_type,
  remaining_amount, remaining_currency, remaining_type
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			id, p.Payment.IsNegotiable, p.Payment.PriceRequest, p.Payment.TotalAmount, p.Payment.TotalCurrency,
			p.Payment.WithPrepayment,
			p.Payment.PrepaymentAmount, p.Payment.PrepaymentCurrency, p.Payment.PrepaymentType,
			p.Payment.RemainingAmount, p.Payment.RemainingCurrency, p.Payment.RemainingType)
		if err != nil {
			return uuid.Nil, err
		}
	}

	return id, tx.Commit(ctx)
}

// GetByID returns cargo by id (excluding soft-deleted if needAll=false).
func (r *Repo) GetByID(ctx context.Context, id uuid.UUID, includeDeleted bool) (*Cargo, error) {
	q := `SELECT c.id, c.name, c.weight, c.volume, COALESCE(c.vehicles_amount, 0), COALESCE(c.vehicles_left, 0), c.packaging, c.dimensions, COALESCE(c.photo_urls, ARRAY[]::text[]),
  c.ready_enabled, c.ready_at, c.load_comment, c.truck_type, COALESCE(c.power_plate_type,''), COALESCE(c.trailer_plate_type,''),
  c.temp_min, c.temp_max, c.adr_enabled, c.adr_class, c.loading_types, c.shipment_type, c.belts_count,
  c.documents, c.contact_name, c.contact_phone, c.status, c.created_at, c.updated_at, c.deleted_at,
  c.moderation_rejection_reason, c.created_by_type, c.created_by_id, c.company_id, c.cargo_type_id,
  ct.code, ct.name_ru, ct.name_uz, ct.name_en, ct.name_tr, ct.name_zh
FROM cargo c
LEFT JOIN cargo_types ct ON ct.id = c.cargo_type_id
WHERE c.id = $1`
	if !includeDeleted {
		q += ` AND c.deleted_at IS NULL`
	}
	return scanCargo(r.pg.QueryRow(ctx, q, id))
}

// GetRoutePoints returns route points for a cargo.
func (r *Repo) GetRoutePoints(ctx context.Context, cargoID uuid.UUID) ([]RoutePoint, error) {
	rows, err := r.pg.Query(ctx, `
SELECT id, cargo_id, type, COALESCE(country_code,''), COALESCE(city_code,''), COALESCE(region_code,''), address, COALESCE(orientir,''), lat, lng, place_id, comment, point_order, is_main_load, is_main_unload, point_at
FROM route_points WHERE cargo_id = $1 ORDER BY point_order`,
		cargoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []RoutePoint
	for rows.Next() {
		var rp RoutePoint
		err := rows.Scan(&rp.ID, &rp.CargoID, &rp.Type, &rp.CountryCode, &rp.CityCode, &rp.RegionCode, &rp.Address, &rp.Orientir, &rp.Lat, &rp.Lng, &rp.PlaceID, &rp.Comment, &rp.PointOrder, &rp.IsMainLoad, &rp.IsMainUnload, &rp.PointAt)
		if err != nil {
			return nil, err
		}
		list = append(list, rp)
	}
	return list, rows.Err()
}

// GetPayment returns payment for a cargo (if any).
func (r *Repo) GetPayment(ctx context.Context, cargoID uuid.UUID) (*Payment, error) {
	var pay Payment
	err := r.pg.QueryRow(ctx, `
SELECT id, cargo_id, is_negotiable, price_request, total_amount, total_currency, with_prepayment,
  prepayment_amount, prepayment_currency, prepayment_type, remaining_amount, remaining_currency, remaining_type
FROM payments WHERE cargo_id = $1`, cargoID).Scan(
		&pay.ID, &pay.CargoID, &pay.IsNegotiable, &pay.PriceRequest, &pay.TotalAmount, &pay.TotalCurrency,
		&pay.WithPrepayment, &pay.PrepaymentAmount, &pay.PrepaymentCurrency,
		&pay.PrepaymentType, &pay.RemainingAmount, &pay.RemainingCurrency, &pay.RemainingType)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &pay, nil
}

// CreateCargoPhoto stores photo metadata for cargo.
func (r *Repo) CreateCargoPhoto(ctx context.Context, cargoID uuid.UUID, uploaderID *uuid.UUID, mime string, sizeBytes int64, path string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pg.QueryRow(ctx, `
INSERT INTO cargo_photos (cargo_id, uploader_id, mime, size_bytes, path)
VALUES ($1,$2,$3,$4,$5)
RETURNING id`,
		cargoID, uploaderID, mime, sizeBytes, path,
	).Scan(&id)
	return id, err
}

func (r *Repo) ListCargoPhotos(ctx context.Context, cargoID uuid.UUID) ([]CargoPhoto, error) {
	rows, err := r.pg.Query(ctx, `
SELECT id, cargo_id, uploader_id, mime, size_bytes, path, created_at
FROM cargo_photos
WHERE cargo_id = $1
ORDER BY created_at DESC`, cargoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CargoPhoto
	for rows.Next() {
		var p CargoPhoto
		if err := rows.Scan(&p.ID, &p.CargoID, &p.UploaderID, &p.Mime, &p.SizeBytes, &p.Path, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) GetCargoPhotoForUser(ctx context.Context, photoID uuid.UUID) (*CargoPhoto, error) {
	var p CargoPhoto
	err := r.pg.QueryRow(ctx, `
SELECT id, cargo_id, uploader_id, mime, size_bytes, path, created_at
FROM cargo_photos
WHERE id = $1
LIMIT 1`, photoID).Scan(&p.ID, &p.CargoID, &p.UploaderID, &p.Mime, &p.SizeBytes, &p.Path, &p.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repo) DeleteCargoPhoto(ctx context.Context, photoID uuid.UUID) error {
	_, err := r.pg.Exec(ctx, `DELETE FROM cargo_photos WHERE id = $1`, photoID)
	return err
}

func scanCargo(row pgx.Row) (*Cargo, error) {
	var c Cargo
	var docBytes []byte
	var loadingTypes []string
	var packaging, dimensions sql.NullString
	var ctCode, ctRU, ctUZ, ctEN, ctTR, ctZH sql.NullString
	err := row.Scan(
		&c.ID, &c.Name, &c.Weight, &c.Volume, &c.VehiclesAmount, &c.VehiclesLeft, &packaging, &dimensions, &c.PhotoURLs, &c.ReadyEnabled, &c.ReadyAt, &c.Comment, &c.TruckType, &c.PowerPlateType, &c.TrailerPlateType,
		&c.TempMin, &c.TempMax, &c.ADREnabled, &c.ADRClass, &loadingTypes, &c.ShipmentType, &c.BeltsCount,
		&docBytes, &c.ContactName, &c.ContactPhone, &c.Status, &c.CreatedAt, &c.UpdatedAt, &c.DeletedAt,
		&c.ModerationRejectionReason, &c.CreatedByType, &c.CreatedByID, &c.CompanyID, &c.CargoTypeID,
		&ctCode, &ctRU, &ctUZ, &ctEN, &ctTR, &ctZH,
	)
	if err != nil {
		return nil, err
	}
	c.LoadingTypes = loadingTypes
	if packaging.Valid {
		s := packaging.String
		c.Packaging = &s
	}
	if dimensions.Valid {
		s := dimensions.String
		c.Dimensions = &s
	}
	if ctCode.Valid {
		c.CargoTypeCode = &ctCode.String
		c.CargoTypeNameRU = &ctRU.String
		c.CargoTypeNameUZ = &ctUZ.String
		c.CargoTypeNameEN = &ctEN.String
		c.CargoTypeNameTR = &ctTR.String
		c.CargoTypeNameZH = &ctZH.String
	}
	if len(docBytes) > 0 {
		c.Documents, _ = DocumentsFromJSON(docBytes)
	}
	return &c, nil
}

func cargoListSelectFrom() string {
	return `SELECT c.id, c.name, c.weight, c.volume, COALESCE(c.vehicles_amount, 0), COALESCE(c.vehicles_left, 0), c.packaging, c.dimensions, COALESCE(c.photo_urls, ARRAY[]::text[]),
  c.ready_enabled, c.ready_at, c.load_comment, c.truck_type, COALESCE(c.power_plate_type,''), COALESCE(c.trailer_plate_type,''),
  c.temp_min, c.temp_max, c.adr_enabled, c.adr_class, c.loading_types, c.shipment_type, c.belts_count,
  c.documents, c.contact_name, c.contact_phone, c.status, c.created_at, c.updated_at, c.deleted_at,
  c.moderation_rejection_reason, c.created_by_type, c.created_by_id, c.company_id, c.cargo_type_id,
  ct.code, ct.name_ru, ct.name_uz, ct.name_en, ct.name_tr, ct.name_zh
FROM cargo c
LEFT JOIN cargo_types ct ON ct.id = c.cargo_type_id
WHERE `
}

var ErrOfferNotFoundOrNotPending = errors.New("cargo: offer not found or not pending")
var ErrCargoNotSearching = errors.New("cargo: cargo not searching")
var ErrCargoSlotsFull = errors.New("cargo: cargo has no vehicles_left")
var ErrDriverBusy = errors.New("cargo: driver already has active cargo")

// buildCargoListWhereAndOrder строит WHERE (без префикса), аргументы и ORDER BY для списка грузов.
func buildCargoListWhereAndOrder(f ListFilter) (where string, args []any, order string) {
	var conds []string
	argNum := 1
	conds = append(conds, "c.deleted_at IS NULL")

	if f.CreatedByDispatcherID != nil {
		conds = append(conds, "UPPER(COALESCE(c.created_by_type,'')) = 'DISPATCHER' AND c.created_by_id = $"+strconv.Itoa(argNum))
		args = append(args, *f.CreatedByDispatcherID)
		argNum++
	}
	if f.CompanyID != nil {
		conds = append(conds, "c.company_id = $"+strconv.Itoa(argNum))
		args = append(args, *f.CompanyID)
		argNum++
	}

	// When driver lists "searching" cargo, show SEARCHING_ALL + SEARCHING_COMPANY (only his company)
	if f.ForDriverCompanyID != nil && len(f.Status) > 0 && statusListContainsSearching(f.Status) {
		conds = append(conds, "(c.status = 'SEARCHING_ALL' OR (c.status = 'SEARCHING_COMPANY' AND c.company_id = $"+strconv.Itoa(argNum)+"))")
		args = append(args, *f.ForDriverCompanyID)
		argNum++
	} else if len(f.Status) > 0 {
		conds = append(conds, "c.status = ANY($"+strconv.Itoa(argNum)+")")
		args = append(args, f.Status)
		argNum++
	}
	if pat := sanitizeCargoNameLikePattern(f.NameContains); pat != "" {
		conds = append(conds, "c.name ILIKE $"+strconv.Itoa(argNum))
		args = append(args, pat)
		argNum++
	}
	if f.WeightMin != nil {
		conds = append(conds, "c.weight >= $"+strconv.Itoa(argNum))
		args = append(args, *f.WeightMin)
		argNum++
	}
	if f.WeightMax != nil {
		conds = append(conds, "c.weight <= $"+strconv.Itoa(argNum))
		args = append(args, *f.WeightMax)
		argNum++
	}
	if f.TruckType != "" {
		conds = append(conds, "c.truck_type = $"+strconv.Itoa(argNum))
		args = append(args, f.TruckType)
		argNum++
	}
	if f.CreatedFrom != "" {
		conds = append(conds, "c.created_at::date >= $"+strconv.Itoa(argNum))
		args = append(args, f.CreatedFrom)
		argNum++
	}
	if f.CreatedTo != "" {
		conds = append(conds, "c.created_at::date <= $"+strconv.Itoa(argNum))
		args = append(args, f.CreatedTo)
		argNum++
	}
	if f.WithOffers != nil && *f.WithOffers {
		conds = append(conds, "EXISTS (SELECT 1 FROM offers o WHERE o.cargo_id = c.id)")
	}

	order = "c.created_at DESC"
	if f.Sort != "" {
		parts := strings.SplitN(f.Sort, ":", 2)
		if len(parts) == 2 {
			col := strings.TrimSpace(parts[0])
			dir := strings.ToUpper(strings.TrimSpace(parts[1]))
			if col == "created_at" || col == "weight" || col == "status" {
				if dir == "ASC" || dir == "DESC" {
					order = "c." + col + " " + dir
				}
			}
		}
	}

	return strings.Join(conds, " AND "), args, order
}

// ListDispatcherCargoForExport — все грузы диспетчера по тем же фильтрам, что GET /api/cargo, без пагинации (до MaxCargoExportRows).
func (r *Repo) ListDispatcherCargoForExport(ctx context.Context, dispatcherID uuid.UUID, f ListFilter) ([]Cargo, int, error) {
	f2 := f
	f2.CreatedByDispatcherID = &dispatcherID
	f2.ForDriverCompanyID = nil

	where, args, order := buildCargoListWhereAndOrder(f2)
	var total int
	if err := r.pg.QueryRow(ctx, "SELECT COUNT(*) FROM cargo c WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total > MaxCargoExportRows {
		return nil, total, ErrCargoExportTooManyRows
	}
	if total == 0 {
		return []Cargo{}, 0, nil
	}

	limitArg := len(args) + 1
	args2 := append(append([]any(nil), args...), total)
	q := cargoListSelectFrom() + where + ` ORDER BY ` + order + ` LIMIT $` + strconv.Itoa(limitArg)

	rows, err := r.pg.Query(ctx, q, args2...)
	if err != nil {
		return nil, total, err
	}
	defer rows.Close()
	var items []Cargo
	for rows.Next() {
		c, err := scanCargo(rows)
		if err != nil {
			return nil, total, err
		}
		items = append(items, *c)
	}
	return items, total, rows.Err()
}

// GetRoutePointsForCargoIDs загружает точки маршрута для набора грузов (для экспорта).
func (r *Repo) GetRoutePointsForCargoIDs(ctx context.Context, cargoIDs []uuid.UUID) (map[uuid.UUID][]RoutePoint, error) {
	out := make(map[uuid.UUID][]RoutePoint)
	if len(cargoIDs) == 0 {
		return out, nil
	}
	rows, err := r.pg.Query(ctx, `
SELECT id, cargo_id, type, COALESCE(country_code,''), COALESCE(city_code,''), COALESCE(region_code,''), address, COALESCE(orientir,''), lat, lng, place_id, comment, point_order, is_main_load, is_main_unload, point_at
FROM route_points WHERE cargo_id = ANY($1::uuid[]) ORDER BY cargo_id, point_order`, cargoIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rp RoutePoint
		if err := rows.Scan(&rp.ID, &rp.CargoID, &rp.Type, &rp.CountryCode, &rp.CityCode, &rp.RegionCode, &rp.Address, &rp.Orientir, &rp.Lat, &rp.Lng, &rp.PlaceID, &rp.Comment, &rp.PointOrder, &rp.IsMainLoad, &rp.IsMainUnload, &rp.PointAt); err != nil {
			return nil, err
		}
		out[rp.CargoID] = append(out[rp.CargoID], rp)
	}
	return out, rows.Err()
}

// GetPaymentsForCargoIDs загружает оплату для набора грузов.
func (r *Repo) GetPaymentsForCargoIDs(ctx context.Context, cargoIDs []uuid.UUID) (map[uuid.UUID]*Payment, error) {
	out := make(map[uuid.UUID]*Payment)
	if len(cargoIDs) == 0 {
		return out, nil
	}
	rows, err := r.pg.Query(ctx, `
SELECT id, cargo_id, is_negotiable, price_request, total_amount, total_currency, with_prepayment,
  prepayment_amount, prepayment_currency, prepayment_type, remaining_amount, remaining_currency, remaining_type
FROM payments WHERE cargo_id = ANY($1::uuid[])`, cargoIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pay Payment
		if err := rows.Scan(
			&pay.ID, &pay.CargoID, &pay.IsNegotiable, &pay.PriceRequest, &pay.TotalAmount, &pay.TotalCurrency,
			&pay.WithPrepayment, &pay.PrepaymentAmount, &pay.PrepaymentCurrency,
			&pay.PrepaymentType, &pay.RemainingAmount, &pay.RemainingCurrency, &pay.RemainingType); err != nil {
			return nil, err
		}
		p := pay
		out[pay.CargoID] = &p
	}
	return out, rows.Err()
}

// List returns paginated cargo list with filters.
func (r *Repo) List(ctx context.Context, f ListFilter) (ListResult, error) {
	where, args, order := buildCargoListWhereAndOrder(f)

	var total int
	err := r.pg.QueryRow(ctx, "SELECT COUNT(*) FROM cargo c WHERE "+where, args...).Scan(&total)
	if err != nil {
		return ListResult{}, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (f.Page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	argNum := len(args) + 1
	args = append(args, limit, offset)
	q := cargoListSelectFrom() + where + ` ORDER BY ` + order + ` LIMIT $` + strconv.Itoa(argNum) + ` OFFSET $` + strconv.Itoa(argNum+1)

	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	var items []Cargo
	for rows.Next() {
		c, err := scanCargo(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, *c)
	}
	return ListResult{Items: items, Total: total}, rows.Err()
}

func statusListContainsSearching(statuses []string) bool {
	for _, s := range statuses {
		switch s {
		case string(StatusSearchingAll), string(StatusSearchingCompany), "SEARCHING":
			return true
		}
	}
	return false
}

// sanitizeCargoNameLikePattern builds a safe ILIKE pattern: trim, max length, strip % _ \.
func sanitizeCargoNameLikePattern(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 200 {
		s = s[:200]
	}
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "\\", "")
	if s == "" {
		return ""
	}
	return "%" + s + "%"
}

// nextArgNum returns placeholder number as string (1, 2, ...).
func nextArgNum(n *int) string {
	x := *n
	*n++
	return strconv.Itoa(x)
}

// UpdateParams for PUT /api/cargo/:id (partial; only non-nil fields updated where applicable).
type UpdateParams struct {
	Name             *string
	Weight           *float64
	Volume           *float64
	Packaging        *string
	Dimensions       *string
	Photos           []string
	ReadyEnabled     *bool
	ReadyAt          *string
	Comment          *string
	TruckType        *string
	TempMin          *float64
	TempMax          *float64
	ADREnabled       *bool
	ADRClass         *string
	LoadingTypes     []string
	ShipmentType     *ShipmentType
	BeltsCount       *int
	Documents        *Documents
	ContactName      *string
	ContactPhone     *string
	RoutePoints      []RoutePointInput
	Payment          *PaymentInput
}

// Update updates cargo and optionally replaces route_points and payment. Returns error if cargo not found or deleted.
func (r *Repo) Update(ctx context.Context, id uuid.UUID, p UpdateParams) error {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	existing, err := r.GetByID(ctx, id, false)
	if err != nil || existing == nil {
		return err
	}
	if existing.Status == StatusAssigned || existing.Status == StatusInTransit || existing.Status == StatusDelivered {
		// After assigned cannot change price and route - we block full update of route and payment
		// but allow contact/comment edits if needed; for simplicity we block any update of route_points
		if len(p.RoutePoints) > 0 || p.Payment != nil {
			return ErrCannotEditAfterAssigned
		}
	}

	// Build dynamic update for cargo
	setCols := []string{"updated_at = now()"}
	args := []any{}
	argN := 1
	add := func(col string, v any) {
		setCols = append(setCols, col+" = $"+nextArgNum(&argN))
		args = append(args, v)
	}
	if p.Name != nil {
		add("name", *p.Name)
	}
	if p.Weight != nil {
		add("weight", *p.Weight)
	}
	if p.Volume != nil {
		add("volume", *p.Volume)
	}
	if p.Packaging != nil {
		add("packaging", *p.Packaging)
	}
	if p.Dimensions != nil {
		add("dimensions", *p.Dimensions)
	}
	if p.Photos != nil {
		add("photo_urls", p.Photos)
	}
	if p.ReadyEnabled != nil {
		add("ready_enabled", *p.ReadyEnabled)
	}
	if p.ReadyAt != nil {
		add("ready_at", p.ReadyAt)
	}
	if p.Comment != nil {
		add("load_comment", *p.Comment)
	}
	if p.TruckType != nil {
		add("truck_type", *p.TruckType)
	}
	if p.TempMin != nil {
		add("temp_min", *p.TempMin)
	}
	if p.TempMax != nil {
		add("temp_max", *p.TempMax)
	}
	if p.ADREnabled != nil {
		add("adr_enabled", *p.ADREnabled)
	}
	if p.ADRClass != nil {
		add("adr_class", *p.ADRClass)
	}
	if p.LoadingTypes != nil {
		add("loading_types", p.LoadingTypes)
	}
	if p.ShipmentType != nil {
		add("shipment_type", *p.ShipmentType)
	}
	if p.BeltsCount != nil {
		add("belts_count", *p.BeltsCount)
	}
	if p.Documents != nil {
		docJSON, _ := DocumentsToJSON(p.Documents)
		add("documents", docJSON)
	}
	if p.ContactName != nil {
		add("contact_name", *p.ContactName)
	}
	if p.ContactPhone != nil {
		add("contact_phone", *p.ContactPhone)
	}

	if len(setCols) > 1 {
		args = append(args, id)
		_, err = tx.Exec(ctx, "UPDATE cargo SET "+strings.Join(setCols, ", ")+" WHERE id = $"+nextArgNum(&argN)+" AND deleted_at IS NULL", args...)
		if err != nil {
			return err
		}
	}

	if len(p.RoutePoints) > 0 && existing.Status != StatusAssigned && existing.Status != StatusInTransit && existing.Status != StatusDelivered {
		_, _ = tx.Exec(ctx, "DELETE FROM route_points WHERE cargo_id = $1", id)
		for _, rp := range p.RoutePoints {
			_, err = tx.Exec(ctx, `
INSERT INTO route_points (cargo_id, type, country_code, city_code, region_code, address, orientir, lat, lng, place_id, comment, point_order, is_main_load, is_main_unload, point_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
				id, rp.Type, emptyToNil(strings.ToUpper(strings.TrimSpace(rp.CountryCode))), emptyToNil(rp.CityCode), emptyToNil(rp.RegionCode), rp.Address, emptyToNil(rp.Orientir), rp.Lat, rp.Lng, rp.PlaceID, rp.Comment, rp.PointOrder, rp.IsMainLoad, rp.IsMainUnload, rp.PointAt)
			if err != nil {
				return err
			}
		}
	}

	if p.Payment != nil && existing.Status != StatusAssigned && existing.Status != StatusInTransit && existing.Status != StatusDelivered {
		_, err = tx.Exec(ctx, `
UPDATE payments SET is_negotiable=$2, price_request=$3, total_amount=$4, total_currency=$5, with_prepayment=$6,
  prepayment_amount=$7, prepayment_currency=$8, prepayment_type=$9, remaining_amount=$10, remaining_currency=$11, remaining_type=$12
WHERE cargo_id = $1`,
			id, p.Payment.IsNegotiable, p.Payment.PriceRequest, p.Payment.TotalAmount, p.Payment.TotalCurrency,
			p.Payment.WithPrepayment,
			p.Payment.PrepaymentAmount, p.Payment.PrepaymentCurrency, p.Payment.PrepaymentType,
			p.Payment.RemainingAmount, p.Payment.RemainingCurrency, p.Payment.RemainingType)
		if err != nil {
			return err
		}
		// If no row updated, insert
		var n int
		_ = tx.QueryRow(ctx, "SELECT 1 FROM payments WHERE cargo_id = $1", id).Scan(&n)
		if n == 0 {
			_, err = tx.Exec(ctx, `
INSERT INTO payments (cargo_id, is_negotiable, price_request, total_amount, total_currency, with_prepayment,
  prepayment_amount, prepayment_currency, prepayment_type, remaining_amount, remaining_currency, remaining_type)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				id, p.Payment.IsNegotiable, p.Payment.PriceRequest, p.Payment.TotalAmount, p.Payment.TotalCurrency,
				p.Payment.WithPrepayment,
				p.Payment.PrepaymentAmount, p.Payment.PrepaymentCurrency, p.Payment.PrepaymentType,
				p.Payment.RemainingAmount, p.Payment.RemainingCurrency, p.Payment.RemainingType)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

var ErrCannotEditAfterAssigned = errors.New("cargo: cannot edit route or payment after assigned")

// CountByDispatcher возвращает число грузов, созданных диспетчером (created_by_type='dispatcher', без удалённых).
func (r *Repo) CountByDispatcher(ctx context.Context, dispatcherID uuid.UUID) (int, error) {
	var n int
	err := r.pg.QueryRow(ctx,
		"SELECT count(*) FROM cargo WHERE created_by_type = 'DISPATCHER' AND created_by_id = $1 AND deleted_at IS NULL",
		dispatcherID).Scan(&n)
	return n, err
}

// Delete soft-deletes cargo (sets deleted_at).
func (r *Repo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pg.Exec(ctx, "UPDATE cargo SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL", id)
	return err
}

// SetStatus updates cargo status with allowed transitions. Returns error if transition invalid.
func (r *Repo) SetStatus(ctx context.Context, id uuid.UUID, newStatus string) error {
	allowed := map[CargoStatus][]CargoStatus{
		StatusCreated:            {StatusSearchingAll, StatusSearchingCompany, StatusCancelled},
		StatusPendingModeration:   {StatusSearchingAll, StatusSearchingCompany, StatusRejected},
		StatusSearchingAll:       {StatusAssigned, StatusCancelled},
		StatusSearchingCompany:   {StatusAssigned, StatusCancelled},
		StatusRejected:           nil,
		StatusAssigned:          {StatusInProgress, StatusCancelled},
		StatusInProgress:        {StatusCompleted},
		StatusInTransit:         {StatusDelivered},
		StatusDelivered:         nil,
		StatusCompleted:         nil,
		StatusCancelled:         nil,
	}
	cur, err := r.GetByID(ctx, id, false)
	if err != nil || cur == nil {
		return err
	}
	next, ok := allowed[cur.Status]
	if !ok {
		return errors.New("cargo: invalid current status")
	}
	for _, s := range next {
		if s == CargoStatus(newStatus) {
			_, err = r.pg.Exec(ctx, "UPDATE cargo SET status = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL", newStatus, id)
			return err
		}
	}
	return errors.New("cargo: status transition not allowed")
}

// GetOfferByID returns one offer by id (nil if not found).
func (r *Repo) GetOfferByID(ctx context.Context, offerID uuid.UUID) (*Offer, error) {
	var o Offer
	var rejReason string
	err := r.pg.QueryRow(ctx, `
SELECT id, cargo_id, carrier_id, price, currency, comment, status, COALESCE(rejection_reason, ''), created_at
FROM offers WHERE id = $1`, offerID).Scan(&o.ID, &o.CargoID, &o.CarrierID, &o.Price, &o.Currency, &o.Comment, &o.Status, &rejReason, &o.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if rejReason != "" {
		o.RejectionReason = &rejReason
	}
	return &o, nil
}

// GetOffers returns all offers for a cargo.
func (r *Repo) GetOffers(ctx context.Context, cargoID uuid.UUID) ([]Offer, error) {
	rows, err := r.pg.Query(ctx, `
SELECT id, cargo_id, carrier_id, price, currency, comment, status, COALESCE(rejection_reason, ''), created_at
FROM offers WHERE cargo_id = $1 ORDER BY created_at DESC`, cargoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Offer
	for rows.Next() {
		var o Offer
		var rejReason string
		err := rows.Scan(&o.ID, &o.CargoID, &o.CarrierID, &o.Price, &o.Currency, &o.Comment, &o.Status, &rejReason, &o.CreatedAt)
		if err != nil {
			return nil, err
		}
		if rejReason != "" {
			o.RejectionReason = &rejReason
		}
		list = append(list, o)
	}
	return list, rows.Err()
}

// CountDriverCargoOffersByBucket returns count of offers by driver (carrier_id) for selected bucket.
// bucket: sent|accepted|completed|rejected.
func (r *Repo) CountDriverCargoOffersByBucket(ctx context.Context, driverID uuid.UUID, bucket string) (int, error) {
	where, err := driverCargoOffersBucketWhere(bucket)
	if err != nil {
		return 0, err
	}
	var total int
	err = r.pg.QueryRow(ctx, `
SELECT COUNT(*)
  FROM offers o
 INNER JOIN cargo c ON c.id = o.cargo_id AND c.deleted_at IS NULL
 WHERE o.carrier_id = $1 AND `+where, driverID).Scan(&total)
	return total, err
}

// ListDriverCargoOffersByBucket lists driver offers with minimal cargo info.
// bucket: sent|accepted|completed|rejected.
func (r *Repo) ListDriverCargoOffersByBucket(ctx context.Context, driverID uuid.UUID, bucket string, limit, offset int) ([]DriverCargoOffer, error) {
	where, err := driverCargoOffersBucketWhere(bucket)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.pg.Query(ctx, `
SELECT
  o.id, o.cargo_id, o.carrier_id,
  o.price, o.currency, o.comment, o.status,
  COALESCE(o.rejection_reason, ''), o.created_at,
  c.status, c.name, c.weight, c.volume, c.truck_type, COALESCE(c.vehicles_left, 0),
  t.id, t.status
FROM offers o
INNER JOIN cargo c ON c.id = o.cargo_id AND c.deleted_at IS NULL
LEFT JOIN trips t ON t.offer_id = o.id
WHERE o.carrier_id = $1 AND `+where+`
ORDER BY o.created_at DESC
LIMIT $2 OFFSET $3`, driverID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DriverCargoOffer
	for rows.Next() {
		var row DriverCargoOffer
		var rejReason string
		var cargoName sql.NullString
		var tripID *uuid.UUID
		var tripStatus sql.NullString
		err := rows.Scan(
			&row.ID, &row.CargoID, &row.CarrierID,
			&row.Price, &row.Currency, &row.Comment, &row.Status,
			&rejReason, &row.CreatedAt,
			&row.CargoStatus, &cargoName, &row.CargoWeight, &row.CargoVolume, &row.CargoTruckType, &row.CargoVehiclesLeft,
			&tripID, &tripStatus,
		)
		if err != nil {
			return nil, err
		}
		if rejReason != "" {
			row.RejectionReason = &rejReason
		}
		if cargoName.Valid {
			n := cargoName.String
			row.CargoName = &n
		}
		row.TripID = tripID
		if tripStatus.Valid {
			s := tripStatus.String
			row.TripStatus = &s
		} else {
			row.TripStatus = nil
		}
		list = append(list, row)
	}
	return list, rows.Err()
}

func driverCargoOffersBucketWhere(bucket string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(bucket)) {
	case "sent":
		return "o.status = 'PENDING'", nil
	case "accepted":
		// Accepted but not yet completed.
		return "o.status = 'ACCEPTED' AND c.status <> 'COMPLETED'", nil
	case "completed":
		return "o.status = 'ACCEPTED' AND c.status = 'COMPLETED'", nil
	case "rejected", "declined":
		return "o.status = 'REJECTED'", nil
	default:
		return "", errors.New("cargo: invalid bucket")
	}
}

// CreateOffer inserts an offer for a cargo.
func (r *Repo) CreateOffer(ctx context.Context, cargoID, carrierID uuid.UUID, price float64, currency, comment string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pg.QueryRow(ctx, `
INSERT INTO offers (cargo_id, carrier_id, price, currency, comment, status, created_at)
VALUES ($1, $2, $3, $4, $5, 'PENDING', now()) RETURNING id`,
		cargoID, carrierID, price, currency, nullStr(comment)).Scan(&id)
	return id, err
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// AcceptOffer sets offer status to accepted and cargo status to assigned. Returns cargoID and carrierID (driver).
func (r *Repo) AcceptOffer(ctx context.Context, offerID uuid.UUID) (cargoID, carrierID uuid.UUID, err error) {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, "SELECT cargo_id, carrier_id FROM offers WHERE id = $1 AND status = 'PENDING'", offerID).Scan(&cargoID, &carrierID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, uuid.Nil, ErrOfferNotFoundOrNotPending
		}
		return uuid.Nil, uuid.Nil, err
	}

	// Lock cargo row for concurrency correctness (trip count vs vehicles_amount).
	var status CargoStatus
	var vehiclesAmount int
	err = tx.QueryRow(ctx,
		`SELECT status, vehicles_amount
		 FROM cargo
		 WHERE id = $1 AND deleted_at IS NULL
		 FOR UPDATE`,
		cargoID).Scan(&status, &vehiclesAmount)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if !IsSearching(status) {
		return uuid.Nil, uuid.Nil, ErrCargoNotSearching
	}
	// Slots = vehicles_amount: count ACCEPTED offers (not trips). Trips are created after this tx commits,
	// so counting trips would allow too many accepts in flight; ACCEPTED offers reserve slots correctly.
	var acceptedCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM offers WHERE cargo_id = $1 AND status = 'ACCEPTED'`,
		cargoID).Scan(&acceptedCount)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if acceptedCount >= vehiclesAmount {
		return uuid.Nil, uuid.Nil, ErrCargoSlotsFull
	}

	if err := cargodrivers.AcceptTx(ctx, tx, cargoID, carrierID); err != nil {
		if errors.Is(err, cargodrivers.ErrDriverBusy) {
			return uuid.Nil, uuid.Nil, ErrDriverBusy
		}
		return uuid.Nil, uuid.Nil, err
	}

	_, err = tx.Exec(ctx, "UPDATE offers SET status = 'ACCEPTED' WHERE id = $1", offerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	// vehicles_left is decremented when a trip enters LOADING (see OnTripEnteredLoadingTx), not on offer accept.
	// Do NOT auto-reject other pending offers: cargo may require multiple vehicles.
	return cargoID, carrierID, tx.Commit(ctx)
}

// RejectOffer sets offer status to rejected with optional reason (dispatcher).
func (r *Repo) RejectOffer(ctx context.Context, offerID uuid.UUID, reason string) error {
	res, err := r.pg.Exec(ctx,
		"UPDATE offers SET status = 'REJECTED', rejection_reason = NULLIF(TRIM($2), '') WHERE id = $1 AND status = 'PENDING'",
		offerID, reason)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return errors.New("cargo: offer not found or not pending")
	}
	return nil
}

// SearchVisibility is the visibility when admin accepts moderation: "all" (SEARCHING_ALL) or "company" (SEARCHING_COMPANY).
const SearchVisibilityAll = "all"
const SearchVisibilityCompany = "company"

// ModerationAccept sets cargo status to SEARCHING_ALL or SEARCHING_COMPANY (admin approved).
// visibility: "all" or "company". For dispatcher-created cargo only "all" is valid (caller should pass "all").
func (r *Repo) ModerationAccept(ctx context.Context, cargoID uuid.UUID, visibility string) error {
	status := StatusSearchingAll
	if visibility == SearchVisibilityCompany {
		status = StatusSearchingCompany
	}
	res, err := r.pg.Exec(ctx,
		"UPDATE cargo SET status = $1, updated_at = now(), moderation_rejection_reason = NULL WHERE id = $2 AND deleted_at IS NULL AND status = $3",
		status, cargoID, StatusPendingModeration)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return errors.New("cargo: not found or not pending_moderation")
	}
	return nil
}

// ModerationReject sets cargo status to rejected with mandatory reason (admin).
func (r *Repo) ModerationReject(ctx context.Context, cargoID uuid.UUID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("cargo: moderation rejection reason is required")
	}
	res, err := r.pg.Exec(ctx,
		"UPDATE cargo SET status = $1, moderation_rejection_reason = $2, updated_at = now() WHERE id = $3 AND deleted_at IS NULL AND status = $4",
		StatusRejected, strings.TrimSpace(reason), cargoID, StatusPendingModeration)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return errors.New("cargo: not found or not pending_moderation")
	}
	return nil
}

// ListPendingModeration returns cargo list with status pending_moderation (for admin).
func (r *Repo) ListPendingModeration(ctx context.Context, limit, offset int) ([]Cargo, int, error) {
	var total int
	_ = r.pg.QueryRow(ctx, "SELECT count(*) FROM cargo WHERE deleted_at IS NULL AND status = $1", StatusPendingModeration).Scan(&total)
	q := cargoListSelectFrom() + `c.deleted_at IS NULL AND c.status = $1 ORDER BY c.created_at ASC LIMIT $2 OFFSET $3`
	rows, err := r.pg.Query(ctx, q, StatusPendingModeration, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []Cargo
	for rows.Next() {
		c, err := scanCargo(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *c)
	}
	return list, total, rows.Err()
}

// SetCargoStatusInProgress sets cargo status to in_progress (when trip execution starts).
func (r *Repo) SetCargoStatusInProgress(ctx context.Context, cargoID uuid.UUID) error {
	res, err := r.pg.Exec(ctx,
		"UPDATE cargo SET status = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL AND status = $3",
		StatusInProgress, cargoID, StatusAssigned)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return nil // already in progress or other status, idempotent
	}
	return nil
}

// SetCargoStatusCompleted sets cargo status to completed (when trip is completed).
func (r *Repo) SetCargoStatusCompleted(ctx context.Context, cargoID uuid.UUID) error {
	// For multi-vehicle cargo: consider cargo completed only when
	// - it is already in progress/in transit AND
	// - there are no ACTIVE drivers left AND
	// - vehicles_left is 0 (fully staffed).
	_, err := r.pg.Exec(ctx,
		`UPDATE cargo
		 SET status = $1, updated_at = now()
		 WHERE id = $2
		   AND deleted_at IS NULL
		   AND (status = $3 OR status = $4)
		   AND COALESCE(vehicles_left, 0) <= 0
		   AND NOT EXISTS (
		     SELECT 1 FROM cargo_drivers cd
		     WHERE cd.cargo_id = cargo.id AND cd.status = 'ACTIVE'
		   )`,
		StatusCompleted, cargoID, StatusInProgress, StatusInTransit)
	return err
}

// MarkDriverCompleted marks driver-cargo link completed (frees driver for next cargo).
func (r *Repo) MarkDriverCompleted(ctx context.Context, cargoID, driverID uuid.UUID) error {
	_, err := r.pg.Exec(ctx,
		`UPDATE cargo_drivers
		 SET status = 'COMPLETED', updated_at = now()
		 WHERE cargo_id = $1 AND driver_id = $2 AND status = 'ACTIVE'`,
		cargoID, driverID)
	return err
}

// OnTripEnteredLoadingTx decrements vehicles_left and sets cargo to IN_PROGRESS when the first vehicle starts execution.
func (r *Repo) OnTripEnteredLoadingTx(ctx context.Context, tx pgx.Tx, cargoID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE cargo
		SET vehicles_left = GREATEST(0, vehicles_left - 1),
		    status = CASE
		      WHEN status IN ('SEARCHING_ALL', 'SEARCHING_COMPANY') THEN 'IN_PROGRESS'
		      ELSE status
		    END,
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`,
		cargoID)
	return err
}

// MarkDriverCancelled marks driver-cargo link cancelled; optionally restores vehicles_left if the trip had already reached LOADING.
func (r *Repo) MarkDriverCancelled(ctx context.Context, cargoID, driverID uuid.UUID) error {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	restore := false
	var st string
	err = tx.QueryRow(ctx,
		`SELECT status FROM trips WHERE cargo_id = $1 AND driver_id = $2 ORDER BY created_at DESC LIMIT 1`,
		cargoID, driverID).Scan(&st)
	if err == nil {
		restore = tripStatusConsumesVehiclesLeft(st)
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else {
		return err
	}
	if err := r.MarkDriverCancelledTx(ctx, tx, cargoID, driverID, restore); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkDriverCancelledTx is the transactional core for MarkDriverCancelled.
func (r *Repo) MarkDriverCancelledTx(ctx context.Context, tx pgx.Tx, cargoID, carrierID uuid.UUID, restoreVehiclesLeft bool) error {
	res, err := tx.Exec(ctx,
		`UPDATE cargo_drivers
		 SET status = 'CANCELLED', updated_at = now()
		 WHERE cargo_id = $1 AND driver_id = $2 AND status = 'ACTIVE'`,
		cargoID, carrierID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return nil
	}
	if !restoreVehiclesLeft {
		return nil
	}
	_, err = tx.Exec(ctx,
		`UPDATE cargo
		 SET vehicles_left = LEAST(vehicles_amount, vehicles_left + 1),
		     updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		cargoID)
	return err
}

// OnTripCancelledTx reverts offer to PENDING and frees the driver slot via offer.carrier_id (same transaction as trip removal).
func (r *Repo) OnTripCancelledTx(ctx context.Context, tx pgx.Tx, cargoID, offerID uuid.UUID, tripStatus string) error {
	var carrierID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT carrier_id FROM offers WHERE id = $1 AND cargo_id = $2`,
		offerID, cargoID).Scan(&carrierID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE offers SET status = 'PENDING', rejection_reason = NULL WHERE id = $1 AND cargo_id = $2`,
		offerID, cargoID)
	if err != nil {
		return err
	}
	return r.MarkDriverCancelledTx(ctx, tx, cargoID, carrierID, tripStatusConsumesVehiclesLeft(tripStatus))
}

// ArchiveCompletedCargoTx archives the completed trip; if it was the last open trip for the cargo, archives cargo and deletes it.
func (r *Repo) ArchiveCompletedCargoTx(ctx context.Context, tx pgx.Tx, cargoID, tripID uuid.UUID, driverID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE cargo_drivers
		 SET status = 'COMPLETED', updated_at = now()
		 WHERE cargo_id = $1 AND driver_id = $2 AND status = 'ACTIVE'`,
		cargoID, driverID)
	if err != nil {
		return err
	}

	var tripCount int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM trips WHERE cargo_id = $1`, cargoID).Scan(&tripCount)
	if err != nil {
		return err
	}
	lastTrip := tripCount <= 1

	_, err = tx.Exec(ctx, `
		INSERT INTO archived_trips (id, cargo_id, offer_id, driver_id, status, created_at, updated_at, archived_at, cancel_reason, cancelled_by_role)
		SELECT id, cargo_id, offer_id, driver_id, status, created_at, updated_at, now(), NULL, NULL FROM trips WHERE id = $1`,
		tripID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM trips WHERE id = $1`, tripID)
	if err != nil {
		return err
	}

	if !lastTrip {
		return nil
	}

	_, err = tx.Exec(ctx, `UPDATE cargo SET status = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL AND status <> $1`,
		StatusCompleted, cargoID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO archived_cargo (id, snapshot, archived_at)
		 SELECT id, to_jsonb(cargo.*), now() FROM cargo WHERE id = $1`,
		cargoID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM cargo WHERE id = $1`, cargoID)
	return err
}

// emptyToNil returns nil for empty string (for NULL in DB), else the string.
func emptyToNil(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// NearbyItem extends Cargo with distance and origin coordinates.
type NearbyItem struct {
	Cargo
	DistanceKM float64
	OriginLat  float64
	OriginLng  float64
}

// NearbyFilter for nearby-cargo search.
type NearbyFilter struct {
	Lat                float64
	Lng                float64
	ForDriverCompanyID *uuid.UUID
	Page               int
	Limit              int
}

// NearbyResult is paginated nearby cargo.
type NearbyResult struct {
	Items []NearbyItem
	Total int
}

// ListNearby returns cargo sorted by distance from the given point (main load route_point).
func (r *Repo) ListNearby(ctx context.Context, f NearbyFilter) (NearbyResult, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (f.Page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	// COUNT query uses its own args (no lat/lng needed).
	countStatusCond := "c.status = 'SEARCHING_ALL'"
	var countArgs []any
	if f.ForDriverCompanyID != nil {
		countStatusCond = "(c.status = 'SEARCHING_ALL' OR (c.status = 'SEARCHING_COMPANY' AND c.company_id = $1))"
		countArgs = append(countArgs, *f.ForDriverCompanyID)
	}
	countWhere := "c.deleted_at IS NULL AND " + countStatusCond
	countQ := `SELECT COUNT(*) FROM cargo c JOIN route_points rp ON rp.cargo_id = c.id AND rp.is_main_load = true WHERE ` + countWhere
	var total int
	if err := r.pg.QueryRow(ctx, countQ, countArgs...).Scan(&total); err != nil {
		return NearbyResult{}, err
	}

	// Main query: lat/lng first, then optional company_id, then limit/offset.
	var args []any
	argN := 1

	args = append(args, f.Lat)
	latArg := "$" + strconv.Itoa(argN)
	argN++
	args = append(args, f.Lng)
	lngArg := "$" + strconv.Itoa(argN)
	argN++

	statusCond := "c.status = 'SEARCHING_ALL'"
	if f.ForDriverCompanyID != nil {
		statusCond = "(c.status = 'SEARCHING_ALL' OR (c.status = 'SEARCHING_COMPANY' AND c.company_id = $" + strconv.Itoa(argN) + "))"
		args = append(args, *f.ForDriverCompanyID)
		argN++
	}

	distExpr := `(6371 * acos(GREATEST(-1.0, LEAST(1.0, cos(radians(` + latArg + `)) * cos(radians(rp.lat)) * cos(radians(rp.lng) - radians(` + lngArg + `)) + sin(radians(` + latArg + `)) * sin(radians(rp.lat))))))`

	where := "c.deleted_at IS NULL AND " + statusCond

	args = append(args, limit, offset)
	q := `SELECT c.id, c.name, c.weight, c.volume,
  c.packaging, c.dimensions, COALESCE(c.photo_urls, ARRAY[]::text[]),
  c.ready_enabled, c.ready_at, c.load_comment,
  c.truck_type, c.temp_min, c.temp_max, c.adr_enabled, c.adr_class,
  c.loading_types, c.shipment_type, c.belts_count,
  c.documents, c.contact_name, c.contact_phone, c.status,
  c.created_at, c.updated_at, c.deleted_at,
  c.moderation_rejection_reason, c.created_by_type, c.created_by_id, c.company_id, c.cargo_type_id,
  ct.code, ct.name_ru, ct.name_uz, ct.name_en, ct.name_tr, ct.name_zh,
  rp.lat, rp.lng, ` + distExpr + ` AS distance_km
FROM cargo c
LEFT JOIN cargo_types ct ON ct.id = c.cargo_type_id
JOIN route_points rp ON rp.cargo_id = c.id AND rp.is_main_load = true
WHERE ` + where + `
ORDER BY distance_km ASC
LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)

	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return NearbyResult{}, err
	}
	defer rows.Close()

	var items []NearbyItem
	for rows.Next() {
		var item NearbyItem
		var docBytes []byte
		var loadingTypes []string
		var packaging, dimensions sql.NullString
		var ctCode, ctRU, ctUZ, ctEN, ctTR, ctZH sql.NullString
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Weight, &item.Volume,
			&packaging, &dimensions, &item.PhotoURLs,
			&item.ReadyEnabled, &item.ReadyAt, &item.Comment,
			&item.TruckType, &item.TempMin, &item.TempMax, &item.ADREnabled, &item.ADRClass,
			&loadingTypes, &item.ShipmentType, &item.BeltsCount,
			&docBytes, &item.ContactName, &item.ContactPhone, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
			&item.ModerationRejectionReason, &item.CreatedByType, &item.CreatedByID, &item.CompanyID, &item.CargoTypeID,
			&ctCode, &ctRU, &ctUZ, &ctEN, &ctTR, &ctZH,
			&item.OriginLat, &item.OriginLng, &item.DistanceKM,
		); err != nil {
			return NearbyResult{}, err
		}
		item.LoadingTypes = loadingTypes
		if packaging.Valid {
			s := packaging.String
			item.Packaging = &s
		}
		if dimensions.Valid {
			s := dimensions.String
			item.Dimensions = &s
		}
		if ctCode.Valid {
			item.CargoTypeCode = &ctCode.String
			item.CargoTypeNameRU = &ctRU.String
			item.CargoTypeNameUZ = &ctUZ.String
			item.CargoTypeNameEN = &ctEN.String
			item.CargoTypeNameTR = &ctTR.String
			item.CargoTypeNameZH = &ctZH.String
		}
		if len(docBytes) > 0 {
			item.Documents, _ = DocumentsFromJSON(docBytes)
		}
		items = append(items, item)
	}
	return NearbyResult{Items: items, Total: total}, rows.Err()
}

// TrailerToTruckTypes maps driver trailer_plate_type to matching cargo truck_type(s).
var TrailerToTruckTypes = map[string][]string{
	"TENTED":      {"TENT"},
	"REEFER":      {"REFRIGERATOR"},
	"FLATBED":     {"FLATBED"},
	"TANKER":      {"TANKER"},
	"BOX":         {"TENT", "OTHER"},
	"CONTAINER":   {"OTHER"},
	"TIPPER":      {"OTHER"},
	"LOWBED":      {"FLATBED", "OTHER"},
	"CAR_CARRIER": {"OTHER"},
}

// MatchingFilter for matching-cargo search by truck type.
type MatchingFilter struct {
	TruckTypes         []string
	ForDriverCompanyID *uuid.UUID
	Page               int
	Limit              int
}

// ListMatching returns cargo filtered by truck_type match, paginated.
func (r *Repo) ListMatching(ctx context.Context, f MatchingFilter) (ListResult, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (f.Page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	var args []any
	argN := 1
	var conds []string
	conds = append(conds, "c.deleted_at IS NULL")

	if len(f.TruckTypes) > 0 {
		conds = append(conds, "c.truck_type = ANY($"+strconv.Itoa(argN)+")")
		args = append(args, f.TruckTypes)
		argN++
	}

	if f.ForDriverCompanyID != nil {
		conds = append(conds, "(c.status = 'SEARCHING_ALL' OR (c.status = 'SEARCHING_COMPANY' AND c.company_id = $"+strconv.Itoa(argN)+"))")
		args = append(args, *f.ForDriverCompanyID)
		argN++
	} else {
		conds = append(conds, "c.status = 'SEARCHING_ALL'")
	}

	where := strings.Join(conds, " AND ")

	var total int
	if err := r.pg.QueryRow(ctx, "SELECT COUNT(*) FROM cargo c WHERE "+where, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}

	args = append(args, limit, offset)
	q := cargoListSelectFrom() + where + ` ORDER BY c.created_at DESC LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)

	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	var items []Cargo
	for rows.Next() {
		c, err := scanCargo(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, *c)
	}
	return ListResult{Items: items, Total: total}, rows.Err()
}
