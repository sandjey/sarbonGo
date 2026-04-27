package cargo

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"sarbonNew/internal/cargodrivers"
)

type Repo struct {
	pg *pgxpool.Pool
}

type offerStore interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func NewRepo(pg *pgxpool.Pool) *Repo {
	return &Repo{pg: pg}
}

func (r *Repo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pg.Begin(ctx)
}

// tripStatusConsumesVehiclesLeft is true when vehicles_left was already decremented (trip reached IN_TRANSIT or later in the new model).
func tripStatusConsumesVehiclesLeft(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "IN_TRANSIT", "DELIVERED", "COMPLETED",
		"LOADING", "EN_ROUTE", "UNLOADING": // legacy rows
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
	Status                []string   // status=SEARCHING_ALL,SEARCHING_COMPANY
	ForDriverCompanyID    *uuid.UUID // when set, "searching" filter shows SEARCHING_ALL + SEARCHING_COMPANY for this company only
	CreatedByDispatcherID *uuid.UUID // only cargo created by this dispatcher (export / «мои грузы»)
	CompanyID             *uuid.UUID // optional: only cargo for this company_id (marketplace filter)
	NameContains          string     // optional: ILIKE substring on c.name (from q=)
	WeightMin             *float64
	WeightMax             *float64
	TruckType             string
	FromCityCode          string // main LOAD city_code (route_points.is_main_load=true)
	ToCityCode            string // main UNLOAD city_code (route_points.is_main_unload=true)
	CreatedFrom           string // YYYY-MM-DD
	CreatedTo             string
	WithOffers            *bool // only cargo that have at least one offer
	Page                  int
	Limit                 int
	Sort                  string // "created_at:desc" or "created_at:asc"
}

// ListResult for paginated list.
type ListResult struct {
	Items []Cargo
	Total int
}

// CreateParams for creating cargo with route points and payment.
type CreateParams struct {
	// Шаг 1 — Груз
	Name            *string
	Weight          float64 `validate:"required,gt=0"`
	Volume          float64
	VehiclesAmount  int      `validate:"required,gt=0"` // Количество машин
	Packaging       *string  // Упаковка
	PackagingAmount *int     // Количество упаковок (шт.)
	Dimensions      *string  // Габариты
	Photos          []string // Фото (max 5, каждая ≤10MB)
	WayPoints       []WayPoint

	// Шаг 2 — Готовность
	ReadyEnabled bool
	ReadyAt      *string
	Comment      *string

	// Шаг 3 — Транспорт
	TruckType            string `validate:"required"`
	PowerPlateType       string `validate:"required"` // TRUCK|TRACTOR (GET /v1/driver/transport-options)
	TrailerPlateType     string `validate:"required"` // depends on PowerPlateType
	TempMin              *float64
	TempMax              *float64
	ADREnabled           bool
	ADRClass             *string `validate:"required_if=ADREnabled true"`
	LoadingTypes         []string
	UnloadingTypes       []string
	IsTwoDriversRequired bool
	ShipmentType         *ShipmentType
	BeltsCount           *int
	Documents            *Documents // TIR, T1, CMR, Medbook, GLONASS, Seal, Permit

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
	Type         string `validate:"required,oneof=load unload customs transit"`
	CountryCode  string
	CityCode     string
	RegionCode   string
	Address      string `validate:"required"`
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
	PaymentNote        *string
	PaymentTermsNote   *string
}

// Create creates cargo, route_points and payment in a transaction.
func (r *Repo) Create(ctx context.Context, p CreateParams) (uuid.UUID, error) {
	if p.ReadyEnabled && p.ReadyAt != nil && strings.TrimSpace(*p.ReadyAt) != "" {
		return uuid.Nil, ErrReadyAtNotAllowedWhenReadyEnabledTrue
	}
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
  packaging, packaging_amount, dimensions, photo_urls, way_points,
  ready_enabled, ready_at, load_comment,
  truck_type, power_plate_type, trailer_plate_type,
  temp_min, temp_max, adr_enabled, adr_class,
  loading_types, unloading_types, is_two_drivers_required, shipment_type, belts_count,
  documents, contact_name, contact_phone,
  status, created_at, updated_at, deleted_at,
  created_by_type, created_by_id, company_id, cargo_type_id
)
-- NOTE: load_comment column is used as generic comment in API (field name: comment).
VALUES (
  $1, $2, $3, $4, $4,
  $5, $6, $7, $8, $9,
  $10, $11, $12,
  $13, $14, $15,
  $16, $17, $18, $19,
  $20, $21, $22, $23, $24,
  $25, $26, $27,
  COALESCE(NULLIF(TRIM($28),''), 'PENDING_MODERATION'), now(), now(), NULL,
  $29, $30, $31, $32
)
RETURNING id`
	err = tx.QueryRow(ctx, q,
		p.Name,
		p.Weight, p.Volume, p.VehiclesAmount,
		p.Packaging, p.PackagingAmount, p.Dimensions, p.Photos, p.WayPoints,
		p.ReadyEnabled, p.ReadyAt, p.Comment,
		p.TruckType, p.PowerPlateType, p.TrailerPlateType,
		p.TempMin, p.TempMax, p.ADREnabled, p.ADRClass,
		p.LoadingTypes, p.UnloadingTypes, p.IsTwoDriversRequired, p.ShipmentType, p.BeltsCount,
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
  remaining_amount, remaining_currency, remaining_type, payment_note, payment_terms_note
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			id, p.Payment.IsNegotiable, p.Payment.PriceRequest, p.Payment.TotalAmount, p.Payment.TotalCurrency,
			p.Payment.WithPrepayment,
			p.Payment.PrepaymentAmount, p.Payment.PrepaymentCurrency, p.Payment.PrepaymentType,
			p.Payment.RemainingAmount, p.Payment.RemainingCurrency, p.Payment.RemainingType, p.Payment.PaymentNote, p.Payment.PaymentTermsNote)
		if err != nil {
			return uuid.Nil, err
		}
	}

	return id, tx.Commit(ctx)
}

// GetByID returns cargo by id (excluding soft-deleted if needAll=false).
func (r *Repo) GetByID(ctx context.Context, id uuid.UUID, includeDeleted bool) (*Cargo, error) {
	q := `SELECT c.id, c.name, c.weight, c.volume, COALESCE(c.vehicles_amount, 0), COALESCE(c.vehicles_left, 0), c.packaging, c.packaging_amount, c.dimensions, COALESCE(c.photo_urls, ARRAY[]::text[]), c.way_points,
  c.ready_enabled, c.ready_at, c.load_comment, c.truck_type, COALESCE(c.power_plate_type,''), COALESCE(c.trailer_plate_type,''),
  c.temp_min, c.temp_max, c.adr_enabled, c.adr_class, c.loading_types, c.unloading_types, c.is_two_drivers_required, c.shipment_type, c.belts_count,
  c.documents, c.contact_name, c.contact_phone, c.status, c.created_at, c.updated_at, c.deleted_at,
  c.moderation_rejection_reason, c.created_by_type, c.created_by_id, c.company_id, c.cargo_type_id,
  ct.code, ct.name_ru, ct.name_uz, ct.name_en, ct.name_tr, ct.name_zh
FROM cargo c
LEFT JOIN cargo_types ct ON ct.id = c.cargo_type_id
WHERE c.id = $1`
	if !includeDeleted {
		q += ` AND c.deleted_at IS NULL`
	}
	cg, err := scanCargo(r.pg.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cg, nil
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
  prepayment_amount, prepayment_currency, prepayment_type, remaining_amount, remaining_currency, remaining_type, payment_note, payment_terms_note
FROM payments WHERE cargo_id = $1`, cargoID).Scan(
		&pay.ID, &pay.CargoID, &pay.IsNegotiable, &pay.PriceRequest, &pay.TotalAmount, &pay.TotalCurrency,
		&pay.WithPrepayment, &pay.PrepaymentAmount, &pay.PrepaymentCurrency,
		&pay.PrepaymentType, &pay.RemainingAmount, &pay.RemainingCurrency, &pay.RemainingType, &pay.PaymentNote, &pay.PaymentTermsNote)
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
	var wayPoints []WayPoint
	var loadingTypes, unloadingTypes []string
	var packaging, dimensions sql.NullString
	var packagingAmount sql.NullInt64
	var ctCode, ctRU, ctUZ, ctEN, ctTR, ctZH sql.NullString
	err := row.Scan(
		&c.ID, &c.Name, &c.Weight, &c.Volume, &c.VehiclesAmount, &c.VehiclesLeft, &packaging, &packagingAmount, &dimensions, &c.PhotoURLs, &wayPoints, &c.ReadyEnabled, &c.ReadyAt, &c.Comment, &c.TruckType, &c.PowerPlateType, &c.TrailerPlateType,
		&c.TempMin, &c.TempMax, &c.ADREnabled, &c.ADRClass, &loadingTypes, &unloadingTypes, &c.IsTwoDriversRequired, &c.ShipmentType, &c.BeltsCount,
		&docBytes, &c.ContactName, &c.ContactPhone, &c.Status, &c.CreatedAt, &c.UpdatedAt, &c.DeletedAt,
		&c.ModerationRejectionReason, &c.CreatedByType, &c.CreatedByID, &c.CompanyID, &c.CargoTypeID,
		&ctCode, &ctRU, &ctUZ, &ctEN, &ctTR, &ctZH,
	)
	if err != nil {
		return nil, err
	}
	c.LoadingTypes = loadingTypes
	c.UnloadingTypes = unloadingTypes
	c.WayPoints = wayPoints
	if packagingAmount.Valid {
		v := int(packagingAmount.Int64)
		c.PackagingAmount = &v
	}
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
	return `SELECT c.id, c.name, c.weight, c.volume, COALESCE(c.vehicles_amount, 0), COALESCE(c.vehicles_left, 0), c.packaging, c.packaging_amount, c.dimensions, COALESCE(c.photo_urls, ARRAY[]::text[]), c.way_points,
  c.ready_enabled, c.ready_at, c.load_comment, c.truck_type, COALESCE(c.power_plate_type,''), COALESCE(c.trailer_plate_type,''),
  c.temp_min, c.temp_max, c.adr_enabled, c.adr_class, c.loading_types, c.unloading_types, c.is_two_drivers_required, c.shipment_type, c.belts_count,
  c.documents, c.contact_name, c.contact_phone, c.status, c.created_at, c.updated_at, c.deleted_at,
  c.moderation_rejection_reason, c.created_by_type, c.created_by_id, c.company_id, c.cargo_type_id,
  ct.code, ct.name_ru, ct.name_uz, ct.name_en, ct.name_tr, ct.name_zh
FROM cargo c
LEFT JOIN cargo_types ct ON ct.id = c.cargo_type_id
WHERE `
}

var ErrOfferNotFoundOrNotPending = errors.New("cargo: offer not found or not pending")
var ErrCargoNotFound = errors.New("cargo: cargo not found")
var ErrCargoNotSearching = errors.New("cargo: cargo not searching")
var ErrCargoSlotsFull = errors.New("cargo: cargo has no vehicles_left")
var ErrCargoTripSlotsFull = errors.New("cargo: active trips already fill vehicles_amount")
var ErrReadyAtNotAllowedWhenReadyEnabledTrue = errors.New("cargo: ready_at not allowed when ready_enabled is true")
var ErrDispatcherOfferAlreadyExists = errors.New("cargo: pending dispatcher offer already exists for this cargo and driver")
var ErrDriverOfferAlreadyExists = errors.New("cargo: driver already has a pending or accepted offer for this cargo")
var ErrCargoManagerDMOfferAlreadyExists = errors.New("cargo: pending cargo manager to driver manager offer already exists")
var ErrDriverBusy = errors.New("cargo: driver already has active cargo")
var ErrOfferPriceOutOfRange = errors.New("cargo: offer price is out of NUMERIC(18,2) range")

const maxOfferPriceNumeric18_2 = 9999999999999999.99

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
	if cc := strings.TrimSpace(f.FromCityCode); cc != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM route_points rp WHERE rp.cargo_id = c.id AND rp.is_main_load = true AND rp.city_code = $"+strconv.Itoa(argNum)+")")
		args = append(args, cc)
		argNum++
	}
	if cc := strings.TrimSpace(f.ToCityCode); cc != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM route_points rp WHERE rp.cargo_id = c.id AND rp.is_main_unload = true AND rp.city_code = $"+strconv.Itoa(argNum)+")")
		args = append(args, cc)
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
  prepayment_amount, prepayment_currency, prepayment_type, remaining_amount, remaining_currency, remaining_type, payment_note, payment_terms_note
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
			&pay.PrepaymentType, &pay.RemainingAmount, &pay.RemainingCurrency, &pay.RemainingType, &pay.PaymentNote, &pay.PaymentTermsNote); err != nil {
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
	Name                 *string
	Weight               *float64
	Volume               *float64
	Packaging            *string
	PackagingAmount      *int
	Dimensions           *string
	Photos               []string
	WayPoints            []WayPoint
	ReadyEnabled         *bool
	ReadyAt              *string
	Comment              *string
	TruckType            *string
	TempMin              *float64
	TempMax              *float64
	ADREnabled           *bool
	ADRClass             *string
	LoadingTypes         []string
	UnloadingTypes       []string
	IsTwoDriversRequired *bool
	ShipmentType         *ShipmentType
	BeltsCount           *int
	Documents            *Documents
	ContactName          *string
	ContactPhone         *string
	RoutePoints          []RoutePointInput
	Payment              *PaymentInput
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
	if !CanEditCargoRoutePayment(existing.Status) {
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
	if p.PackagingAmount != nil {
		add("packaging_amount", *p.PackagingAmount)
	}
	if p.Dimensions != nil {
		add("dimensions", *p.Dimensions)
	}
	if p.Photos != nil {
		add("photo_urls", p.Photos)
	}
	if p.WayPoints != nil {
		add("way_points", p.WayPoints)
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
	if p.UnloadingTypes != nil {
		add("unloading_types", p.UnloadingTypes)
	}
	if p.IsTwoDriversRequired != nil {
		add("is_two_drivers_required", *p.IsTwoDriversRequired)
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

	if len(p.RoutePoints) > 0 && CanEditCargoRoutePayment(existing.Status) {
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

	if p.Payment != nil && CanEditCargoRoutePayment(existing.Status) {
		_, err = tx.Exec(ctx, `
UPDATE payments SET is_negotiable=$2, price_request=$3, total_amount=$4, total_currency=$5, with_prepayment=$6,
  prepayment_amount=$7, prepayment_currency=$8, prepayment_type=$9, remaining_amount=$10, remaining_currency=$11, remaining_type=$12,
  payment_note=$13, payment_terms_note=$14
WHERE cargo_id = $1`,
			id, p.Payment.IsNegotiable, p.Payment.PriceRequest, p.Payment.TotalAmount, p.Payment.TotalCurrency,
			p.Payment.WithPrepayment,
			p.Payment.PrepaymentAmount, p.Payment.PrepaymentCurrency, p.Payment.PrepaymentType,
			p.Payment.RemainingAmount, p.Payment.RemainingCurrency, p.Payment.RemainingType,
			p.Payment.PaymentNote, p.Payment.PaymentTermsNote)
		if err != nil {
			return err
		}
		// If no row updated, insert
		var n int
		_ = tx.QueryRow(ctx, "SELECT 1 FROM payments WHERE cargo_id = $1", id).Scan(&n)
		if n == 0 {
			_, err = tx.Exec(ctx, `
INSERT INTO payments (cargo_id, is_negotiable, price_request, total_amount, total_currency, with_prepayment,
  prepayment_amount, prepayment_currency, prepayment_type, remaining_amount, remaining_currency, remaining_type, payment_note, payment_terms_note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
				id, p.Payment.IsNegotiable, p.Payment.PriceRequest, p.Payment.TotalAmount, p.Payment.TotalCurrency,
				p.Payment.WithPrepayment,
				p.Payment.PrepaymentAmount, p.Payment.PrepaymentCurrency, p.Payment.PrepaymentType,
				p.Payment.RemainingAmount, p.Payment.RemainingCurrency, p.Payment.RemainingType,
				p.Payment.PaymentNote, p.Payment.PaymentTermsNote)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

var ErrCannotEditAfterAssigned = errors.New("cargo: cannot edit route or payment after assigned")
var ErrRejectionReasonRequired = errors.New("cargo: rejection reason is required")

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
// COMPLETED is set only when all trips are completed (see ArchiveCompletedCargoTx), not via PATCH.
func (r *Repo) SetStatus(ctx context.Context, id uuid.UUID, newStatus string) error {
	allowed := map[CargoStatus][]CargoStatus{
		StatusPendingModeration: {StatusSearchingAll, StatusSearchingCompany, StatusCancelled},
		StatusSearchingAll:      {StatusCancelled},
		StatusSearchingCompany:  {StatusCancelled},
		// PROCESSING is reached only via refreshCargoProcessingStatusTx (all offers ACCEPTED).
		// Admin can only hard-cancel it; transitions back to SEARCHING_* happen automatically
		// when a trip cancellation frees a slot.
		StatusProcessing: {StatusCancelled},
		StatusCompleted:  nil,
		StatusCancelled:  nil,
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
	var proposedByIDStr, negotiationDispStr string
	err := r.pg.QueryRow(ctx, `
SELECT id, cargo_id, carrier_id, price, currency, comment, COALESCE(proposed_by, 'DRIVER'), status, COALESCE(rejection_reason, ''), created_at,
  COALESCE(proposed_by_id::text, ''), COALESCE(negotiation_dispatcher_id::text, '')
FROM offers WHERE id = $1`, offerID).Scan(
		&o.ID, &o.CargoID, &o.CarrierID, &o.Price, &o.Currency, &o.Comment, &o.ProposedBy, &o.Status, &rejReason, &o.CreatedAt,
		&proposedByIDStr, &negotiationDispStr,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if rejReason != "" {
		o.RejectionReason = &rejReason
	}
	if s := strings.TrimSpace(proposedByIDStr); s != "" {
		if u, perr := uuid.Parse(s); perr == nil && u != uuid.Nil {
			o.ProposedByID = &u
		}
	}
	if s := strings.TrimSpace(negotiationDispStr); s != "" {
		if u, perr := uuid.Parse(s); perr == nil && u != uuid.Nil {
			o.NegotiationDispatcherID = &u
		}
	}
	return &o, nil
}

// GetOffers returns all offers for a cargo.
func (r *Repo) GetOffers(ctx context.Context, cargoID uuid.UUID) ([]Offer, error) {
	return r.GetOffersFiltered(ctx, cargoID, "", "", nil)
}

// GetOffersFiltered lists offers for one cargo with optional filters (same semantics as dispatcher offers/all).
// direction: empty = all; outgoing/from_me/sent/by → DISPATCHER; incoming/to_me/received → DRIVER.
// status: empty = any; else PENDING|ACCEPTED|REJECTED.
// counterpartyID: filter by carrier_id (driver).
func (r *Repo) GetOffersFiltered(ctx context.Context, cargoID uuid.UUID, direction, status string, counterpartyID *uuid.UUID) ([]Offer, error) {
	where := "o.cargo_id = $1"
	args := []any{cargoID}
	argN := 2
	if d := strings.ToLower(strings.TrimSpace(direction)); d != "" {
		switch d {
		case "outgoing", "from_me", "sent", "by":
			where += " AND COALESCE(o.proposed_by, 'DRIVER') = 'DISPATCHER'"
		case "incoming", "to_me", "received":
			// Incoming for cargo owner: offers from drivers and driver managers.
			where += " AND COALESCE(o.proposed_by, 'DRIVER') IN ('DRIVER', 'DRIVER_MANAGER')"
		default:
			return nil, errors.New("cargo: invalid offers direction")
		}
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		where += " AND o.status = $" + strconv.Itoa(argN)
		args = append(args, s)
		argN++
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		where += " AND o.carrier_id = $" + strconv.Itoa(argN)
		args = append(args, *counterpartyID)
		argN++
	}
	q := `
SELECT o.id, o.cargo_id, o.carrier_id, o.price, o.currency, o.comment, COALESCE(o.proposed_by, 'DRIVER'), o.status, COALESCE(o.rejection_reason, ''), o.created_at,
  COALESCE(o.proposed_by_id::text, ''), COALESCE(o.negotiation_dispatcher_id::text, '')
FROM offers o
WHERE ` + where + ` ORDER BY o.created_at DESC`
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Offer
	for rows.Next() {
		var o Offer
		var rejReason string
		var proposedByIDStr, negotiationDispStr string
		err := rows.Scan(
			&o.ID, &o.CargoID, &o.CarrierID, &o.Price, &o.Currency, &o.Comment, &o.ProposedBy, &o.Status, &rejReason, &o.CreatedAt,
			&proposedByIDStr, &negotiationDispStr,
		)
		if err != nil {
			return nil, err
		}
		if rejReason != "" {
			o.RejectionReason = &rejReason
		}
		if s := strings.TrimSpace(proposedByIDStr); s != "" {
			if u, perr := uuid.Parse(s); perr == nil && u != uuid.Nil {
				o.ProposedByID = &u
			}
		}
		if s := strings.TrimSpace(negotiationDispStr); s != "" {
			if u, perr := uuid.Parse(s); perr == nil && u != uuid.Nil {
				o.NegotiationDispatcherID = &u
			}
		}
		list = append(list, o)
	}
	return list, rows.Err()
}

// ListDriverManagerOffers lists offers for a driver manager:
// 1. Offers proposed by this manager (proposed_by_id = managerID).
// 2. Offers proposed to drivers linked to this manager (carrier_id in driver_manager_relations).
func (r *Repo) ListDriverManagerOffers(ctx context.Context, managerID uuid.UUID, limit, offset int) ([]DriverCargoOffer, error) {
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
  o.price, o.currency, o.comment, COALESCE(o.proposed_by, 'DRIVER'), o.status,
  COALESCE(o.rejection_reason, ''), o.created_at,
  c.status, c.name, c.weight, c.volume, c.truck_type, c.vehicles_amount, COALESCE(c.vehicles_left, 0),
  (
    SELECT rp.city_code
    FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_load = true
    ORDER BY rp.point_order
    LIMIT 1
  ) AS from_city_code,
  (
    SELECT rp.city_code
    FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_unload = true
    ORDER BY rp.point_order
    LIMIT 1
  ) AS to_city_code,
  p.total_amount, p.total_currency, c.created_by_type, c.created_by_id,
  t.id, t.status
FROM offers o
INNER JOIN cargo c ON c.id = o.cargo_id AND c.deleted_at IS NULL
LEFT JOIN payments p ON p.cargo_id = c.id
LEFT JOIN trips t ON t.offer_id = o.id
WHERE (o.proposed_by_id = $1)
   OR (o.proposed_by = 'DISPATCHER' AND o.carrier_id IN (SELECT driver_id FROM driver_manager_relations WHERE manager_id = $1))
ORDER BY o.created_at DESC
LIMIT $2 OFFSET $3`, managerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DriverCargoOffer
	for rows.Next() {
		var row DriverCargoOffer
		var rejReason string
		var cargoName sql.NullString
		var cargoFromCityCode sql.NullString
		var cargoToCityCode sql.NullString
		var cargoCurrentPrice sql.NullFloat64
		var cargoCurrentCurrency sql.NullString
		var cargoCreatedByType sql.NullString
		var cargoCreatedByID *uuid.UUID
		var tripID *uuid.UUID
		var tripStatus sql.NullString
		err := rows.Scan(
			&row.ID, &row.CargoID, &row.CarrierID,
			&row.Price, &row.Currency, &row.Comment, &row.ProposedBy, &row.Status,
			&rejReason, &row.CreatedAt,
			&row.CargoStatus, &cargoName, &row.CargoWeight, &row.CargoVolume, &row.CargoTruckType, &row.CargoVehiclesAmount, &row.CargoVehiclesLeft,
			&cargoFromCityCode, &cargoToCityCode,
			&cargoCurrentPrice, &cargoCurrentCurrency, &cargoCreatedByType, &cargoCreatedByID,
			&tripID, &tripStatus)
		if err != nil {
			return nil, err
		}
		if rejReason != "" {
			row.RejectionReason = &rejReason
		}
		if cargoName.Valid {
			v := cargoName.String
			row.CargoName = &v
		}
		if cargoFromCityCode.Valid {
			v := cargoFromCityCode.String
			row.CargoFromCityCode = &v
		}
		if cargoToCityCode.Valid {
			v := cargoToCityCode.String
			row.CargoToCityCode = &v
		}
		if cargoCurrentPrice.Valid {
			v := cargoCurrentPrice.Float64
			row.CargoCurrentPrice = &v
		}
		if cargoCurrentCurrency.Valid {
			v := cargoCurrentCurrency.String
			row.CargoCurrentCurrency = &v
		}
		if cargoCreatedByType.Valid {
			v := cargoCreatedByType.String
			row.CargoCreatedByType = &v
		}
		row.CargoCreatedByID = cargoCreatedByID
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

// CountDriverManagerOffers counts offers for a driver manager.
func (r *Repo) CountDriverManagerOffers(ctx context.Context, managerID uuid.UUID) (int, error) {
	const q = `
SELECT COUNT(*)
FROM offers o
INNER JOIN cargo c ON c.id = o.cargo_id AND c.deleted_at IS NULL
WHERE (o.proposed_by_id = $1)
   OR (o.proposed_by = 'DISPATCHER' AND o.carrier_id IN (SELECT driver_id FROM driver_manager_relations WHERE manager_id = $1))`
	var count int
	err := r.pg.QueryRow(ctx, q, managerID).Scan(&count)
	return count, err
}

// CountDriverCargoOffersByBucket returns count of offers by driver (carrier_id) for selected bucket.
// bucket: sent|accepted|completed|rejected|canceled|cancelled.
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
// bucket: sent|accepted|completed|rejected|canceled|cancelled.
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
  o.price, o.currency, o.comment, COALESCE(o.proposed_by, 'DRIVER'), o.status,
  COALESCE(o.rejection_reason, ''), o.created_at,
  c.status, c.name, c.weight, c.volume, c.truck_type, c.vehicles_amount, COALESCE(c.vehicles_left, 0),
  (
    SELECT rp.city_code
    FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_load = true
    ORDER BY rp.point_order
    LIMIT 1
  ) AS from_city_code,
  (
    SELECT rp.city_code
    FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_unload = true
    ORDER BY rp.point_order
    LIMIT 1
  ) AS to_city_code,
  p.total_amount, p.total_currency, c.created_by_type, c.created_by_id,
  t.id, t.status
FROM offers o
INNER JOIN cargo c ON c.id = o.cargo_id AND c.deleted_at IS NULL
LEFT JOIN payments p ON p.cargo_id = c.id
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
		var cargoFromCityCode sql.NullString
		var cargoToCityCode sql.NullString
		var cargoCurrentPrice sql.NullFloat64
		var cargoCurrentCurrency sql.NullString
		var cargoCreatedByType sql.NullString
		var cargoCreatedByID *uuid.UUID
		var tripID *uuid.UUID
		var tripStatus sql.NullString
		err := rows.Scan(
			&row.ID, &row.CargoID, &row.CarrierID,
			&row.Price, &row.Currency, &row.Comment, &row.ProposedBy, &row.Status,
			&rejReason, &row.CreatedAt,
			&row.CargoStatus, &cargoName, &row.CargoWeight, &row.CargoVolume, &row.CargoTruckType, &row.CargoVehiclesAmount, &row.CargoVehiclesLeft,
			&cargoFromCityCode, &cargoToCityCode,
			&cargoCurrentPrice, &cargoCurrentCurrency, &cargoCreatedByType, &cargoCreatedByID,
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
		if cargoFromCityCode.Valid {
			v := cargoFromCityCode.String
			row.CargoFromCityCode = &v
		}
		if cargoToCityCode.Valid {
			v := cargoToCityCode.String
			row.CargoToCityCode = &v
		}
		if cargoCurrentPrice.Valid {
			v := cargoCurrentPrice.Float64
			row.CargoCurrentPrice = &v
		}
		if cargoCurrentCurrency.Valid {
			v := cargoCurrentCurrency.String
			row.CargoCurrentCurrency = &v
		}
		if cargoCreatedByType.Valid {
			v := cargoCreatedByType.String
			row.CargoCreatedByType = &v
		}
		row.CargoCreatedByID = cargoCreatedByID
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
	case "canceled", "cancelled":
		return "o.status = 'CANCELED'", nil
	default:
		return "", errors.New("cargo: invalid bucket")
	}
}

func (r *Repo) createOffer(ctx context.Context, db offerStore, cargoID, carrierID uuid.UUID, price float64, currency, comment, proposedBy string, proposedByID *uuid.UUID) (uuid.UUID, error) {
	if math.IsNaN(price) || math.IsInf(price, 0) || math.Abs(price) > maxOfferPriceNumeric18_2 {
		return uuid.Nil, ErrOfferPriceOutOfRange
	}
	if proposedBy != OfferProposedByDispatcher && proposedBy != OfferProposedByDriverManager {
		proposedBy = OfferProposedByDriver
	}
	var status CargoStatus
	var vehiclesLeft int
	var vehiclesAmount int
	if err := db.QueryRow(ctx, `
SELECT status, COALESCE(vehicles_left, 0), COALESCE(vehicles_amount, 0)
FROM cargo
WHERE id = $1 AND deleted_at IS NULL`, cargoID).Scan(&status, &vehiclesLeft, &vehiclesAmount); err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, ErrCargoNotFound
		}
		return uuid.Nil, err
	}
	if !IsSearching(status) {
		return uuid.Nil, ErrCargoNotSearching
	}
	if vehiclesLeft <= 0 {
		return uuid.Nil, ErrCargoSlotsFull
	}
	// Protect against over-sending offers after trips are already opened.
	// A cargo can have at most vehicles_amount active execution trips.
	if vehiclesAmount > 0 {
		var activeTrips int
		if err := db.QueryRow(ctx, `
SELECT COUNT(*)
FROM trips
WHERE cargo_id = $1
  AND status IN ('IN_PROGRESS', 'IN_TRANSIT', 'DELIVERED')`, cargoID).Scan(&activeTrips); err != nil {
			return uuid.Nil, err
		}
		if activeTrips >= vehiclesAmount {
			return uuid.Nil, ErrCargoTripSlotsFull
		}
	}
	if proposedBy == OfferProposedByDriver {
		var driverDup bool
		if err := db.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM offers
  WHERE cargo_id = $1 AND carrier_id = $2
    AND COALESCE(proposed_by, 'DRIVER') = 'DRIVER'
    AND status IN ('PENDING', 'ACCEPTED', 'WAITING_DRIVER_CONFIRM')
)`, cargoID, carrierID).Scan(&driverDup); err != nil {
			return uuid.Nil, err
		}
		if driverDup {
			return uuid.Nil, ErrDriverOfferAlreadyExists
		}
	}
	if proposedBy == OfferProposedByDriverManager {
		var managerDup bool
		if err := db.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM offers
  WHERE cargo_id = $1 AND carrier_id = $2
    AND proposed_by = 'DRIVER_MANAGER'
    AND status IN ('PENDING', 'ACCEPTED', 'WAITING_DRIVER_CONFIRM')
)`, cargoID, carrierID).Scan(&managerDup); err != nil {
			return uuid.Nil, err
		}
		if managerDup {
			return uuid.Nil, ErrDriverOfferAlreadyExists
		}
	}
	if proposedBy == OfferProposedByDispatcher {
		var alreadyExists bool
		if err := db.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM offers
  WHERE cargo_id = $1 AND carrier_id = $2 AND COALESCE(proposed_by, 'DRIVER') = 'DISPATCHER'
    AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM', 'ACCEPTED')
)`, cargoID, carrierID).Scan(&alreadyExists); err != nil {
			return uuid.Nil, err
		}
		if alreadyExists {
			return uuid.Nil, ErrDispatcherOfferAlreadyExists
		}
	}
	var id uuid.UUID
	err := db.QueryRow(ctx, `
INSERT INTO offers (cargo_id, carrier_id, price, currency, comment, status, proposed_by, proposed_by_id, created_at)
VALUES ($1, $2, $3, $4, $5, 'PENDING', $6, $7, now()) RETURNING id`,
		cargoID, carrierID, price, currency, nullStr(comment), proposedBy, proposedByID).Scan(&id)
	return id, err
}

// CreateOffer inserts an offer for a cargo. proposedBy: DRIVER | DISPATCHER | DRIVER_MANAGER.
func (r *Repo) CreateOffer(ctx context.Context, cargoID, carrierID uuid.UUID, price float64, currency, comment, proposedBy string, proposedByID *uuid.UUID) (uuid.UUID, error) {
	return r.createOffer(ctx, r.pg, cargoID, carrierID, price, currency, comment, proposedBy, proposedByID)
}

// CreateOfferTx inserts an offer inside an existing transaction.
func (r *Repo) CreateOfferTx(ctx context.Context, tx pgx.Tx, cargoID, carrierID uuid.UUID, price float64, currency, comment, proposedBy string, proposedByID *uuid.UUID) (uuid.UUID, error) {
	return r.createOffer(ctx, tx, cargoID, carrierID, price, currency, comment, proposedBy, proposedByID)
}

func (r *Repo) CreateCargoManagerDMOffer(ctx context.Context, cargoID, cargoManagerID, driverManagerID uuid.UUID, price float64, currency, comment string) (uuid.UUID, error) {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 || math.Abs(price) > maxOfferPriceNumeric18_2 {
		return uuid.Nil, ErrOfferPriceOutOfRange
	}

	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var status CargoStatus
	var vehiclesLeft int
	if err := tx.QueryRow(ctx, `
SELECT status, COALESCE(vehicles_left, 0)
FROM cargo
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE`, cargoID).Scan(&status, &vehiclesLeft); err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, ErrCargoNotFound
		}
		return uuid.Nil, err
	}
	if !IsSearching(status) {
		return uuid.Nil, ErrCargoNotSearching
	}
	if vehiclesLeft <= 0 {
		return uuid.Nil, ErrCargoSlotsFull
	}

	var dup bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(
	SELECT 1
	FROM cargo_manager_dm_offers
	WHERE cargo_id = $1
	  AND cargo_manager_id = $2
	  AND driver_manager_id = $3
	  AND status = 'PENDING'
)`, cargoID, cargoManagerID, driverManagerID).Scan(&dup); err != nil {
		return uuid.Nil, err
	}
	if dup {
		return uuid.Nil, ErrCargoManagerDMOfferAlreadyExists
	}

	var id uuid.UUID
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		cur = "UZS"
	}
	cmt := strings.TrimSpace(comment)
	if cmt == "" {
		cmt = ""
	}
	err = tx.QueryRow(ctx, `
INSERT INTO cargo_manager_dm_offers (cargo_id, cargo_manager_id, driver_manager_id, price, currency, comment, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), 'PENDING', now(), now())
RETURNING id`,
		cargoID, cargoManagerID, driverManagerID, price, cur, cmt,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	return id, tx.Commit(ctx)
}

func (r *Repo) GetCargoManagerDMOfferByID(ctx context.Context, id uuid.UUID) (*CargoManagerDMOffer, error) {
	var row CargoManagerDMOffer
	var comment sql.NullString
	var driverID *uuid.UUID
	var offerID *uuid.UUID
	var rej sql.NullString
	err := r.pg.QueryRow(ctx, `
SELECT id, cargo_id, cargo_manager_id, driver_manager_id, driver_id, offer_id, price, currency, comment, status, rejection_reason, created_at, updated_at
FROM cargo_manager_dm_offers
WHERE id = $1`, id).Scan(
		&row.ID, &row.CargoID, &row.CargoManagerID, &row.DriverManagerID,
		&driverID, &offerID,
		&row.Price, &row.Currency, &comment, &row.Status, &rej,
		&row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	row.DriverID = driverID
	row.OfferID = offerID
	if comment.Valid {
		v := comment.String
		row.Comment = &v
	}
	if rej.Valid {
		v := rej.String
		row.RejectionReason = &v
	}
	return &row, nil
}

func acceptCargoManagerDMOfferWithExec(ctx context.Context, db offerStore, reqID uuid.UUID, driverID uuid.UUID, offerID uuid.UUID) error {
	tag, err := db.Exec(ctx, `
UPDATE cargo_manager_dm_offers
SET status = 'ACCEPTED',
    driver_id = $2,
    offer_id = $3,
    updated_at = now()
WHERE id = $1 AND status = 'PENDING'`, reqID, driverID, offerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOfferNotFoundOrNotPending
	}
	return nil
}

func (r *Repo) AcceptCargoManagerDMOffer(ctx context.Context, reqID uuid.UUID, driverID uuid.UUID, offerID uuid.UUID) error {
	return acceptCargoManagerDMOfferWithExec(ctx, r.pg, reqID, driverID, offerID)
}

func (r *Repo) AcceptCargoManagerDMOfferTx(ctx context.Context, tx pgx.Tx, reqID uuid.UUID, driverID uuid.UUID, offerID uuid.UUID) error {
	return acceptCargoManagerDMOfferWithExec(ctx, tx, reqID, driverID, offerID)
}

func (r *Repo) RejectCargoManagerDMOfferByDriverManager(ctx context.Context, reqID, driverManagerID uuid.UUID, reason string) error {
	tag, err := r.pg.Exec(ctx, `
UPDATE cargo_manager_dm_offers
SET status = 'REJECTED',
    rejection_reason = NULLIF(TRIM($3), ''),
    updated_at = now()
WHERE id = $1 AND driver_manager_id = $2 AND status = 'PENDING'`, reqID, driverManagerID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOfferNotFoundOrNotPending
	}
	return nil
}

func (r *Repo) CancelCargoManagerDMOfferByCargoManager(ctx context.Context, reqID, cargoManagerID uuid.UUID, reason string) error {
	tag, err := r.pg.Exec(ctx, `
UPDATE cargo_manager_dm_offers
SET status = 'CANCELED',
    rejection_reason = NULLIF(TRIM($3), ''),
    updated_at = now()
WHERE id = $1 AND cargo_manager_id = $2 AND status = 'PENDING'`, reqID, cargoManagerID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOfferNotFoundOrNotPending
	}
	return nil
}

// buildDispatcherOffersAllWhere builds WHERE for GET /v1/dispatchers/offers/all (Count + List):
// - Cargo manager: offers on cargo created by this dispatcher (DISPATCHER + created_by_id).
// - Driver manager: offers this dispatcher proposed as DRIVER_MANAGER on someone else's cargo.
func buildDispatcherOffersAllWhere(direction string, includeCompany bool) (where string, err error) {
	ownerCond := "(UPPER(COALESCE(c.created_by_type,'')) = 'DISPATCHER' AND c.created_by_id = $1)"
	if includeCompany {
		ownerCond = "(" + ownerCond + " OR c.company_id = $2)"
	}
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", "all", "both":
		return `c.deleted_at IS NULL AND (
	(` + ownerCond + `)
	OR (o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1)
)`, nil
	case "outgoing", "from_me", "sent", "by":
		return `c.deleted_at IS NULL AND (
	((` + ownerCond + `) AND COALESCE(o.proposed_by, 'DRIVER') = 'DISPATCHER')
	OR (o.proposed_by = 'DRIVER_MANAGER' AND o.proposed_by_id = $1)
)`, nil
	case "incoming", "to_me", "received":
		return `c.deleted_at IS NULL AND (` + ownerCond + `)
	AND COALESCE(o.proposed_by, 'DRIVER') IN ('DRIVER', 'DRIVER_MANAGER')`, nil
	default:
		return "", errors.New("cargo: invalid offers direction")
	}
}

func (r *Repo) CountDispatcherSentOffers(ctx context.Context, dispatcherID uuid.UUID, companyID *uuid.UUID, status, direction string, counterpartyID *uuid.UUID) (int, error) {
	hasCompany := companyID != nil && *companyID != uuid.Nil
	where, err := buildDispatcherOffersAllWhere(direction, hasCompany)
	if err != nil {
		return 0, err
	}
	args := []any{dispatcherID}
	argN := 2
	if hasCompany {
		args = append(args, *companyID)
		argN++
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		where += " AND o.status = $" + strconv.Itoa(argN)
		args = append(args, s)
		argN++
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		where += " AND o.carrier_id = $" + strconv.Itoa(argN)
		args = append(args, *counterpartyID)
	}
	var total int
	err = r.pg.QueryRow(ctx, `SELECT COUNT(*) FROM offers o INNER JOIN cargo c ON c.id = o.cargo_id WHERE `+where, args...).Scan(&total)
	return total, err
}

func (r *Repo) ListDispatcherSentOffers(ctx context.Context, dispatcherID uuid.UUID, companyID *uuid.UUID, status, direction string, counterpartyID *uuid.UUID, limit, offset int) ([]DispatcherSentOffer, error) {
	if limit < 1 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	hasCompany := companyID != nil && *companyID != uuid.Nil
	where, err := buildDispatcherOffersAllWhere(direction, hasCompany)
	if err != nil {
		return nil, err
	}
	args := []any{dispatcherID}
	argN := 2
	if hasCompany {
		args = append(args, *companyID)
		argN++
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		where += " AND o.status = $" + strconv.Itoa(argN)
		args = append(args, s)
		argN++
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		where += " AND o.carrier_id = $" + strconv.Itoa(argN)
		args = append(args, *counterpartyID)
		argN++
	}
	q := `
SELECT
  o.id, o.cargo_id, o.carrier_id, o.price, o.currency, o.comment, COALESCE(o.proposed_by_id::text, ''), COALESCE(o.proposed_by, 'DISPATCHER'), o.status, COALESCE(o.rejection_reason, ''), o.created_at,
  c.status, c.name, c.vehicles_amount, COALESCE(c.vehicles_left, 0),
  (
    SELECT rp.city_code FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_load = true
    ORDER BY rp.point_order LIMIT 1
  ) AS from_city_code,
  (
    SELECT rp.city_code FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_unload = true
    ORDER BY rp.point_order LIMIT 1
  ) AS to_city_code,
  p.total_amount, p.total_currency,
  t.id, t.status
FROM offers o
INNER JOIN cargo c ON c.id = o.cargo_id
LEFT JOIN payments p ON p.cargo_id = c.id
LEFT JOIN trips t ON t.offer_id = o.id
WHERE ` + where + `
ORDER BY o.created_at DESC
LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	args = append(args, limit, offset)
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DispatcherSentOffer
	for rows.Next() {
		var row DispatcherSentOffer
		var rejReason string
		var cargoName sql.NullString
		var fromCity sql.NullString
		var toCity sql.NullString
		var curPrice sql.NullFloat64
		var curCurrency sql.NullString
		var tripID *uuid.UUID
		var tripStatus sql.NullString
		var proposedByIDStr string
		if err := rows.Scan(
			&row.ID, &row.CargoID, &row.CarrierID, &row.Price, &row.Currency, &row.Comment, &proposedByIDStr, &row.ProposedBy, &row.Status, &rejReason, &row.CreatedAt,
			&row.CargoStatus, &cargoName, &row.CargoVehiclesAmount, &row.CargoVehiclesLeft,
			&fromCity, &toCity,
			&curPrice, &curCurrency,
			&tripID, &tripStatus,
		); err != nil {
			return nil, err
		}
		if proposedByIDStr != "" {
			if u, perr := uuid.Parse(strings.TrimSpace(proposedByIDStr)); perr == nil && u != uuid.Nil {
				row.ProposedByID = &u
			}
		}
		if rejReason != "" {
			row.RejectionReason = &rejReason
		}
		if cargoName.Valid {
			v := cargoName.String
			row.CargoName = &v
		}
		if fromCity.Valid {
			v := fromCity.String
			row.CargoFromCityCode = &v
		}
		if toCity.Valid {
			v := toCity.String
			row.CargoToCityCode = &v
		}
		if curPrice.Valid {
			v := curPrice.Float64
			row.CargoCurrentPrice = &v
		}
		if curCurrency.Valid {
			v := curCurrency.String
			row.CargoCurrentCurrency = &v
		}
		row.TripID = tripID
		if tripStatus.Valid {
			v := tripStatus.String
			row.TripStatus = &v
		}
		list = append(list, row)
	}
	return list, rows.Err()
}

// CountCargoManagerDMOffersForDispatcher counts pending/accepted CM<->DM pre-offers for dispatcher list.
func (r *Repo) CountCargoManagerDMOffersForDispatcher(ctx context.Context, dispatcherID uuid.UUID, status, direction string, counterpartyID *uuid.UUID) (int, error) {
	where := []string{"1=1"}
	args := []any{}
	argN := 1
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", "all", "both":
		where = append(where, "(r.cargo_manager_id = $"+strconv.Itoa(argN)+" OR r.driver_manager_id = $"+strconv.Itoa(argN)+")")
		args = append(args, dispatcherID)
		argN++
	case "outgoing", "from_me", "sent", "by":
		where = append(where, "r.cargo_manager_id = $"+strconv.Itoa(argN))
		args = append(args, dispatcherID)
		argN++
	case "incoming", "to_me", "received":
		where = append(where, "r.driver_manager_id = $"+strconv.Itoa(argN))
		args = append(args, dispatcherID)
		argN++
	default:
		return 0, errors.New("cargo: invalid offers direction")
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		where = append(where, "r.status = $"+strconv.Itoa(argN))
		args = append(args, s)
		argN++
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		switch strings.ToLower(strings.TrimSpace(direction)) {
		case "incoming", "to_me", "received":
			where = append(where, "r.cargo_manager_id = $"+strconv.Itoa(argN))
		default:
			where = append(where, "r.driver_manager_id = $"+strconv.Itoa(argN))
		}
		args = append(args, *counterpartyID)
	}
	var total int
	err := r.pg.QueryRow(ctx, `SELECT COUNT(*) FROM cargo_manager_dm_offers r INNER JOIN cargo c ON c.id=r.cargo_id WHERE c.deleted_at IS NULL AND `+strings.Join(where, " AND "), args...).Scan(&total)
	return total, err
}

// ListCargoManagerDMOffersForDispatcher lists CM->DM requests as dispatcher offers rows.
func (r *Repo) ListCargoManagerDMOffersForDispatcher(ctx context.Context, dispatcherID uuid.UUID, status, direction string, counterpartyID *uuid.UUID, limit, offset int) ([]DispatcherSentOffer, error) {
	if limit < 1 {
		limit = 30
	}
	if limit > 10000 {
		limit = 10000
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"1=1"}
	args := []any{}
	argN := 1
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", "all", "both":
		where = append(where, "(r.cargo_manager_id = $"+strconv.Itoa(argN)+" OR r.driver_manager_id = $"+strconv.Itoa(argN)+")")
		args = append(args, dispatcherID)
		argN++
	case "outgoing", "from_me", "sent", "by":
		where = append(where, "r.cargo_manager_id = $"+strconv.Itoa(argN))
		args = append(args, dispatcherID)
		argN++
	case "incoming", "to_me", "received":
		where = append(where, "r.driver_manager_id = $"+strconv.Itoa(argN))
		args = append(args, dispatcherID)
		argN++
	default:
		return nil, errors.New("cargo: invalid offers direction")
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		where = append(where, "r.status = $"+strconv.Itoa(argN))
		args = append(args, s)
		argN++
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		switch strings.ToLower(strings.TrimSpace(direction)) {
		case "incoming", "to_me", "received":
			where = append(where, "r.cargo_manager_id = $"+strconv.Itoa(argN))
		default:
			where = append(where, "r.driver_manager_id = $"+strconv.Itoa(argN))
		}
		args = append(args, *counterpartyID)
		argN++
	}
	q := `SELECT
  r.id, r.cargo_id, r.driver_manager_id AS carrier_id, r.price, r.currency, r.comment, r.cargo_manager_id::text AS proposed_by_id, 'DISPATCHER' AS proposed_by, COALESCE(o.status, r.status) AS status, '' AS rejection_reason, r.created_at,
  c.status, c.name, c.vehicles_amount, COALESCE(c.vehicles_left, 0),
  (
    SELECT rp.city_code FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_load = true
    ORDER BY rp.point_order LIMIT 1
  ) AS from_city_code,
  (
    SELECT rp.city_code FROM route_points rp
    WHERE rp.cargo_id = c.id AND rp.is_main_unload = true
    ORDER BY rp.point_order LIMIT 1
  ) AS to_city_code,
  p.total_amount, p.total_currency,
  t.id AS trip_id, t.status AS trip_status
FROM cargo_manager_dm_offers r
INNER JOIN cargo c ON c.id = r.cargo_id
LEFT JOIN payments p ON p.cargo_id = c.id
LEFT JOIN offers o ON o.id = r.offer_id
LEFT JOIN trips t ON t.offer_id = r.offer_id
WHERE c.deleted_at IS NULL AND ` + strings.Join(where, " AND ") + `
ORDER BY r.created_at DESC
LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	args = append(args, limit, offset)
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DispatcherSentOffer
	for rows.Next() {
		var row DispatcherSentOffer
		var rejReason string
		var cargoName sql.NullString
		var fromCity sql.NullString
		var toCity sql.NullString
		var curPrice sql.NullFloat64
		var curCurrency sql.NullString
		var tripID *uuid.UUID
		var tripStatus sql.NullString
		var proposedByIDStr string
		if err := rows.Scan(
			&row.ID, &row.CargoID, &row.CarrierID, &row.Price, &row.Currency, &row.Comment, &proposedByIDStr, &row.ProposedBy, &row.Status, &rejReason, &row.CreatedAt,
			&row.CargoStatus, &cargoName, &row.CargoVehiclesAmount, &row.CargoVehiclesLeft,
			&fromCity, &toCity,
			&curPrice, &curCurrency,
			&tripID, &tripStatus,
		); err != nil {
			return nil, err
		}
		if proposedByIDStr != "" {
			if u, perr := uuid.Parse(strings.TrimSpace(proposedByIDStr)); perr == nil && u != uuid.Nil {
				row.ProposedByID = &u
			}
		}
		if cargoName.Valid {
			v := cargoName.String
			row.CargoName = &v
		}
		if fromCity.Valid {
			v := fromCity.String
			row.CargoFromCityCode = &v
		}
		if toCity.Valid {
			v := toCity.String
			row.CargoToCityCode = &v
		}
		if curPrice.Valid {
			v := curPrice.Float64
			row.CargoCurrentPrice = &v
		}
		if curCurrency.Valid {
			v := curCurrency.String
			row.CargoCurrentCurrency = &v
		}
		row.TripID = tripID
		if tripStatus.Valid {
			v := tripStatus.String
			row.TripStatus = &v
		}
		list = append(list, row)
	}
	return list, rows.Err()
}

// GetCargoManagerDMOffersForCargo maps CM->DM requests to Offer for /api/cargo/:id/offers.
func (r *Repo) GetCargoManagerDMOffersForCargo(ctx context.Context, cargoID uuid.UUID, direction, status string, counterpartyID *uuid.UUID) ([]Offer, error) {
	where := []string{"r.cargo_id = $1"}
	args := []any{cargoID}
	argN := 2
	if d := strings.ToLower(strings.TrimSpace(direction)); d != "" {
		switch d {
		case "outgoing", "from_me", "sent", "by":
			// CM->DM requests are outgoing from cargo owner side.
		case "incoming", "to_me", "received":
			return []Offer{}, nil
		default:
			return nil, errors.New("cargo: invalid offers direction")
		}
	}
	if s := strings.ToUpper(strings.TrimSpace(status)); s != "" {
		where = append(where, "r.status = $"+strconv.Itoa(argN))
		args = append(args, s)
		argN++
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		where = append(where, "r.driver_manager_id = $"+strconv.Itoa(argN))
		args = append(args, *counterpartyID)
	}
	q := `SELECT r.id, r.cargo_id, r.driver_manager_id, r.price, r.currency, r.comment, COALESCE(o.status, r.status), r.created_at,
COALESCE(r.cargo_manager_id::text, '')
FROM cargo_manager_dm_offers r
LEFT JOIN offers o ON o.id = r.offer_id
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY r.created_at DESC`
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Offer, 0)
	for rows.Next() {
		var o Offer
		var proposedByIDStr string
		if err := rows.Scan(&o.ID, &o.CargoID, &o.CarrierID, &o.Price, &o.Currency, &o.Comment, &o.Status, &o.CreatedAt, &proposedByIDStr); err != nil {
			return nil, err
		}
		o.ProposedBy = OfferProposedByDispatcher
		if s := strings.TrimSpace(proposedByIDStr); s != "" {
			if u, perr := uuid.Parse(s); perr == nil && u != uuid.Nil {
				o.ProposedByID = &u
			}
		}
		list = append(list, o)
	}
	return list, rows.Err()
}

func (r *Repo) CountDriverOffersAll(ctx context.Context, driverID uuid.UUID, status, direction string, counterpartyID *uuid.UUID) (int, error) {
	where := []string{"o.carrier_id = $1"}
	args := []any{driverID}
	argN := 2
	if status != "" {
		where = append(where, "o.status = $"+strconv.Itoa(argN))
		args = append(args, strings.ToUpper(strings.TrimSpace(status)))
		argN++
	}
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", "all", "both":
		// both incoming and outgoing
	case "outgoing", "from_me", "sent", "by":
		where = append(where, "COALESCE(o.proposed_by, 'DRIVER') = 'DRIVER'")
	case "incoming", "to_me", "received":
		where = append(where, "COALESCE(o.proposed_by, 'DRIVER') IN ('DISPATCHER', 'DRIVER_MANAGER')")
	default:
		return 0, errors.New("cargo: invalid offers direction")
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		where = append(where, "c.created_by_id = $"+strconv.Itoa(argN))
		args = append(args, *counterpartyID)
	}
	q := `SELECT COUNT(*)
FROM offers o
JOIN cargo c ON c.id = o.cargo_id
WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := r.pg.QueryRow(ctx, q, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repo) ListDriverOffersAll(ctx context.Context, driverID uuid.UUID, status, direction string, counterpartyID *uuid.UUID, limit, offset int) ([]DriverAllOffer, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"o.carrier_id = $1"}
	args := []any{driverID}
	argN := 2
	if status != "" {
		where = append(where, "o.status = $"+strconv.Itoa(argN))
		args = append(args, strings.ToUpper(strings.TrimSpace(status)))
		argN++
	}
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", "all", "both":
		// both incoming and outgoing
	case "outgoing", "from_me", "sent", "by":
		where = append(where, "COALESCE(o.proposed_by, 'DRIVER') = 'DRIVER'")
	case "incoming", "to_me", "received":
		where = append(where, "COALESCE(o.proposed_by, 'DRIVER') IN ('DISPATCHER', 'DRIVER_MANAGER')")
	default:
		return nil, errors.New("cargo: invalid offers direction")
	}
	if counterpartyID != nil && *counterpartyID != uuid.Nil {
		where = append(where, "c.created_by_id = $"+strconv.Itoa(argN))
		args = append(args, *counterpartyID)
		argN++
	}
	args = append(args, limit, offset)
	q := `SELECT
  o.id, o.cargo_id, o.carrier_id, o.price, o.currency, o.comment, COALESCE(o.proposed_by, 'DRIVER') AS proposed_by, o.proposed_by_id, o.status, o.rejection_reason, o.created_at,
  c.status, c.name, rpf.city_code AS from_city_code, rpt.city_code AS to_city_code,
  COALESCE(c.vehicles_amount, 0), COALESCE(c.vehicles_left, 0),
  p.total_amount, p.total_currency, c.created_by_type, c.created_by_id,
  t.id, t.status
FROM offers o
JOIN cargo c ON c.id = o.cargo_id
LEFT JOIN LATERAL (
  SELECT rp.city_code
  FROM route_points rp
  WHERE rp.cargo_id = c.id AND rp.is_main_load = true
  ORDER BY rp.point_order ASC
  LIMIT 1
) rpf ON true
LEFT JOIN LATERAL (
  SELECT rp.city_code
  FROM route_points rp
  WHERE rp.cargo_id = c.id AND rp.is_main_unload = true
  ORDER BY rp.point_order ASC
  LIMIT 1
) rpt ON true
LEFT JOIN payments p ON p.cargo_id = c.id
LEFT JOIN trips t ON t.cargo_id = c.id AND t.driver_id = o.carrier_id
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY o.created_at DESC
LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	rows, err := r.pg.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DriverAllOffer
	for rows.Next() {
		var row DriverAllOffer
		var rej sql.NullString
		if err := rows.Scan(
			&row.ID, &row.CargoID, &row.CarrierID, &row.Price, &row.Currency, &row.Comment, &row.ProposedBy, &row.ProposedByID, &row.Status, &rej, &row.CreatedAt,
			&row.CargoStatus, &row.CargoName, &row.CargoFromCityCode, &row.CargoToCityCode,
			&row.CargoVehiclesAmount, &row.CargoVehiclesLeft,
			&row.CargoCurrentPrice, &row.CargoCurrentCurrency, &row.CargoCreatedByType, &row.CargoCreatedByID,
			&row.TripID, &row.TripStatus,
		); err != nil {
			return nil, err
		}
		if rej.Valid {
			s := strings.TrimSpace(rej.String)
			if s != "" {
				row.RejectionReason = &s
			}
		}
		list = append(list, row)
	}
	return list, rows.Err()
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

	cargoID, carrierID, err = r.AcceptOfferTx(ctx, tx, offerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return cargoID, carrierID, tx.Commit(ctx)
}

// AcceptOfferTx is the transactional core of AcceptOffer.
func (r *Repo) AcceptOfferTx(ctx context.Context, tx pgx.Tx, offerID uuid.UUID) (cargoID, carrierID uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `
SELECT cargo_id, carrier_id
FROM offers WHERE id = $1 AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM')`, offerID).Scan(&cargoID, &carrierID)
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
	// Slots = vehicles_amount: count ACCEPTED offers (not trips).
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

	// If this ACCEPTED fills all slots (accepted_count == vehicles_amount),
	// move cargo from SEARCHING_* to PROCESSING so it disappears from driver search.
	if err := r.refreshCargoProcessingStatusTx(ctx, tx, cargoID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return cargoID, carrierID, nil
}

// SetOfferStatusWaitingDriver sets offer status to WAITING_DRIVER_CONFIRM and records which dispatcher is the driver-manager counterpart (ratings + notifications).
func (r *Repo) SetOfferStatusWaitingDriver(ctx context.Context, offerID uuid.UUID, negotiationDispatcherID *uuid.UUID) error {
	return setOfferStatusWaitingDriverWithExec(ctx, r.pg, offerID, negotiationDispatcherID)
}

// SetOfferStatusWaitingDriverTx sets offer status inside an existing transaction.
func (r *Repo) SetOfferStatusWaitingDriverTx(ctx context.Context, tx pgx.Tx, offerID uuid.UUID, negotiationDispatcherID *uuid.UUID) error {
	return setOfferStatusWaitingDriverWithExec(ctx, tx, offerID, negotiationDispatcherID)
}

func setOfferStatusWaitingDriverWithExec(ctx context.Context, db offerStore, offerID uuid.UUID, negotiationDispatcherID *uuid.UUID) error {
	var res pgconn.CommandTag
	var err error
	if negotiationDispatcherID != nil && *negotiationDispatcherID != uuid.Nil {
		res, err = db.Exec(ctx, `
UPDATE offers SET status = 'WAITING_DRIVER_CONFIRM', negotiation_dispatcher_id = $2
WHERE id = $1 AND status = 'PENDING'`, offerID, *negotiationDispatcherID)
	} else {
		res, err = db.Exec(ctx, `
UPDATE offers SET status = 'WAITING_DRIVER_CONFIRM'
WHERE id = $1 AND status = 'PENDING'`, offerID)
	}
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrOfferNotFoundOrNotPending
	}
	return nil
}

// endOfferWithTerminalStatus sets REJECTED (отказ по входящему) or CANCELED (отзыв своего исходящего / отмена цепочки).
// Allowed from PENDING or WAITING_DRIVER_CONFIRM.
func (r *Repo) endOfferWithTerminalStatus(ctx context.Context, offerID uuid.UUID, reason, terminalStatus string) error {
	rsn := strings.TrimSpace(reason)
	if rsn == "" {
		return ErrRejectionReasonRequired
	}
	if terminalStatus != OfferStatusRejected && terminalStatus != OfferStatusCanceled {
		return errors.New("cargo: invalid terminal offer status")
	}
	res, err := r.pg.Exec(ctx,
		"UPDATE offers SET status = $3, rejection_reason = $2 WHERE id = $1 AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM')",
		offerID, rsn, terminalStatus)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return errors.New("cargo: offer not found or not pending")
	}
	return nil
}

// RejectOffer sets status REJECTED (входящий оффер отклонён).
func (r *Repo) RejectOffer(ctx context.Context, offerID uuid.UUID, reason string) error {
	return r.endOfferWithTerminalStatus(ctx, offerID, reason, OfferStatusRejected)
}

// CancelOffer sets status CANCELED (автор отозвал своё предложение или отменил согласование до рейса).
func (r *Repo) CancelOffer(ctx context.Context, offerID uuid.UUID, reason string) error {
	return r.endOfferWithTerminalStatus(ctx, offerID, reason, OfferStatusCanceled)
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
		StatusCancelled, strings.TrimSpace(reason), cargoID, StatusPendingModeration)
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

// OfferInvitationStats counts offers (“invitations”) by direction for one cargo.
type OfferInvitationStats struct {
	IncomingTotal   int // proposed_by = DRIVER (offers to cargo owner)
	OutgoingTotal   int // proposed_by = DISPATCHER
	IncomingPending int
	OutgoingPending int
}

// GetOfferInvitationStats returns invitation/offer counters for a cargo.
func (r *Repo) GetOfferInvitationStats(ctx context.Context, cargoID uuid.UUID) (OfferInvitationStats, error) {
	var s OfferInvitationStats
	err := r.pg.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') IN ('DRIVER','DRIVER_MANAGER')),
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') = 'DISPATCHER'),
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') IN ('DRIVER','DRIVER_MANAGER') AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM')),
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') = 'DISPATCHER' AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM'))
FROM offers WHERE cargo_id = $1`, cargoID).Scan(
		&s.IncomingTotal, &s.OutgoingTotal, &s.IncomingPending, &s.OutgoingPending)
	return s, err
}

// GetDriverOfferInvitationStats returns offer counters for one driver (all cargos).
func (r *Repo) GetDriverOfferInvitationStats(ctx context.Context, driverID uuid.UUID) (OfferInvitationStats, error) {
	var s OfferInvitationStats
	err := r.pg.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') = 'DRIVER'),
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') = 'DISPATCHER'),
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') = 'DRIVER' AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM')),
  COUNT(*) FILTER (WHERE COALESCE(proposed_by,'DRIVER') = 'DISPATCHER' AND status IN ('PENDING', 'WAITING_DRIVER_CONFIRM'))
FROM offers WHERE carrier_id = $1`, driverID).Scan(
		&s.IncomingTotal, &s.OutgoingTotal, &s.IncomingPending, &s.OutgoingPending)
	return s, err
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

// refreshCargoProcessingStatusTx synchronises cargo.status with the current
// number of ACCEPTED offers for the cargo:
//   - SEARCHING_ALL / SEARCHING_COMPANY → PROCESSING when accepted_count > 0
//     (cargo already entered execution and should not stay in searching state).
//     The previous searching variant is saved in cargo.prev_status so we can roll back.
//   - PROCESSING → prev_status (SEARCHING_*) when accepted_count == 0
//     (all accepted slots were released/cancelled and cargo returns to search).
//     prev_status is cleared on rollback.
//
// Called from AcceptOfferTx, OnTripCancelledTx and MarkDriverCancelledTx inside
// the same transaction so visibility and search listings stay consistent.
func (r *Repo) refreshCargoProcessingStatusTx(ctx context.Context, tx pgx.Tx, cargoID uuid.UUID) error {
	var status CargoStatus
	var vehiclesAmount int
	var prevStatus *string
	err := tx.QueryRow(ctx, `
SELECT status, COALESCE(vehicles_amount, 0), prev_status
FROM cargo
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE`, cargoID).Scan(&status, &vehiclesAmount, &prevStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	// Only SEARCHING_* and PROCESSING are involved in this transition.
	if !(IsSearching(status) || status == StatusProcessing) {
		return nil
	}
	if vehiclesAmount <= 0 {
		return nil
	}

	var accepted int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM offers WHERE cargo_id = $1 AND status = 'ACCEPTED'`,
		cargoID).Scan(&accepted); err != nil {
		return err
	}

	switch {
	case IsSearching(status) && accepted > 0:
		_, err = tx.Exec(ctx,
			`UPDATE cargo
			 SET prev_status = status::text, status = $1, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL`,
			StatusProcessing, cargoID)
		return err
	case status == StatusProcessing && accepted == 0:
		revert := StatusSearchingAll
		if prevStatus != nil {
			switch CargoStatus(strings.ToUpper(strings.TrimSpace(*prevStatus))) {
			case StatusSearchingCompany:
				revert = StatusSearchingCompany
			case StatusSearchingAll:
				revert = StatusSearchingAll
			}
		}
		_, err = tx.Exec(ctx,
			`UPDATE cargo
			 SET status = $1, prev_status = NULL, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL`,
			revert, cargoID)
		return err
	}
	return nil
}

// OnTripEnteredInTransitTx decrements vehicles_left when a trip reaches IN_TRANSIT (execution started).
// Cargo stays SEARCHING_* / PROCESSING until all trips are COMPLETED (then set in ArchiveCompletedCargoTx).
func (r *Repo) OnTripEnteredInTransitTx(ctx context.Context, tx pgx.Tx, cargoID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE cargo
		SET vehicles_left = GREATEST(0, vehicles_left - 1),
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`,
		cargoID)
	return err
}

// OnTripEnteredLoadingTx is deprecated; use OnTripEnteredInTransitTx.
func (r *Repo) OnTripEnteredLoadingTx(ctx context.Context, tx pgx.Tx, cargoID uuid.UUID) error {
	return r.OnTripEnteredInTransitTx(ctx, tx, cargoID)
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
	if err := r.MarkDriverCancelledTx(ctx, tx, cargoID, carrierID, tripStatusConsumesVehiclesLeft(tripStatus)); err != nil {
		return err
	}
	// Offer went back to PENDING → accepted_count dropped. If cargo was PROCESSING,
	// roll back to the previous searching variant so drivers can fill the slot again.
	return r.refreshCargoProcessingStatusTx(ctx, tx, cargoID)
}

// ArchiveCompletedCargoTx marks the trip as COMPLETED, updates cargo_drivers; when every trip for the cargo is COMPLETED, sets cargo to COMPLETED (row retained).
func (r *Repo) ArchiveCompletedCargoTx(ctx context.Context, tx pgx.Tx, cargoID, tripID uuid.UUID, driverID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE cargo_drivers
		 SET status = 'COMPLETED', updated_at = now()
		 WHERE cargo_id = $1 AND driver_id = $2 AND status = 'ACTIVE'`,
		cargoID, driverID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx,
		`UPDATE trips SET status = 'COMPLETED', pending_confirm_to = NULL, driver_confirmed_at = NULL, dispatcher_confirmed_at = NULL, updated_at = now()
		 WHERE id = $1 AND cargo_id = $2`,
		tripID, cargoID)
	if err != nil {
		return err
	}

	var total, completed int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'COMPLETED') FROM trips WHERE cargo_id = $1`,
		cargoID).Scan(&total, &completed)
	if err != nil {
		return err
	}
	if total > 0 && total == completed {
		_, err = tx.Exec(ctx,
			`UPDATE cargo
			 SET status = $1, prev_status = NULL, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL
			   AND status IN ('SEARCHING_ALL','SEARCHING_COMPANY','PROCESSING')`,
			StatusCompleted, cargoID)
	}
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
	RadiusKM           *float64
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

	// COUNT query (with optional radius) uses its own args.
	var countArgs []any
	countArgN := 1
	countStatusCond := "c.status = 'SEARCHING_ALL'"
	if f.ForDriverCompanyID != nil {
		countStatusCond = "(c.status = 'SEARCHING_ALL' OR (c.status = 'SEARCHING_COMPANY' AND c.company_id = $" + strconv.Itoa(countArgN) + "))"
		countArgs = append(countArgs, *f.ForDriverCompanyID)
		countArgN++
	}
	countWhere := "c.deleted_at IS NULL AND " + countStatusCond
	countDistCond := ""
	if f.RadiusKM != nil && *f.RadiusKM > 0 {
		countArgs = append(countArgs, f.Lat)
		countLatArg := "$" + strconv.Itoa(countArgN)
		countArgN++
		countArgs = append(countArgs, f.Lng)
		countLngArg := "$" + strconv.Itoa(countArgN)
		countArgN++
		countDistExpr := `(6371 * acos(GREATEST(-1.0, LEAST(1.0, cos(radians(` + countLatArg + `)) * cos(radians(rp.lat)) * cos(radians(rp.lng) - radians(` + countLngArg + `)) + sin(radians(` + countLatArg + `)) * sin(radians(rp.lat))))))`
		countArgs = append(countArgs, *f.RadiusKM)
		countRadiusArg := "$" + strconv.Itoa(countArgN)
		countDistCond = " AND " + countDistExpr + " <= " + countRadiusArg
	}
	countQ := `SELECT COUNT(*) FROM cargo c JOIN route_points rp ON rp.cargo_id = c.id AND rp.is_main_load = true WHERE ` + countWhere + countDistCond
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
	if f.RadiusKM != nil && *f.RadiusKM > 0 {
		where += " AND " + distExpr + " <= $" + strconv.Itoa(argN)
		args = append(args, *f.RadiusKM)
		argN++
	}

	args = append(args, limit, offset)
	q := `SELECT c.id, c.name, c.weight, c.volume, COALESCE(c.vehicles_amount, 0), COALESCE(c.vehicles_left, 0),
  c.packaging, c.packaging_amount, c.dimensions, COALESCE(c.photo_urls, ARRAY[]::text[]), c.way_points,
  c.ready_enabled, c.ready_at, c.load_comment,
  c.truck_type, COALESCE(c.power_plate_type,''), COALESCE(c.trailer_plate_type,''), c.temp_min, c.temp_max, c.adr_enabled, c.adr_class,
  c.loading_types, c.unloading_types, c.is_two_drivers_required, c.shipment_type, c.belts_count,
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
		var wayPoints []WayPoint
		var loadingTypes, unloadingTypes []string
		var packaging, dimensions sql.NullString
		var packagingAmount sql.NullInt64
		var ctCode, ctRU, ctUZ, ctEN, ctTR, ctZH sql.NullString
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Weight, &item.Volume, &item.VehiclesAmount, &item.VehiclesLeft,
			&packaging, &packagingAmount, &dimensions, &item.PhotoURLs, &wayPoints,
			&item.ReadyEnabled, &item.ReadyAt, &item.Comment,
			&item.TruckType, &item.PowerPlateType, &item.TrailerPlateType, &item.TempMin, &item.TempMax, &item.ADREnabled, &item.ADRClass,
			&loadingTypes, &unloadingTypes, &item.IsTwoDriversRequired, &item.ShipmentType, &item.BeltsCount,
			&docBytes, &item.ContactName, &item.ContactPhone, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
			&item.ModerationRejectionReason, &item.CreatedByType, &item.CreatedByID, &item.CompanyID, &item.CargoTypeID,
			&ctCode, &ctRU, &ctUZ, &ctEN, &ctTR, &ctZH,
			&item.OriginLat, &item.OriginLng, &item.DistanceKM,
		); err != nil {
			return NearbyResult{}, err
		}
		item.LoadingTypes = loadingTypes
		item.UnloadingTypes = unloadingTypes
		item.WayPoints = wayPoints
		if packagingAmount.Valid {
			v := int(packagingAmount.Int64)
			item.PackagingAmount = &v
		}
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
