package cargo

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Типы для строгой типизации.
type ShipmentType string

const (
	ShipmentFTL ShipmentType = "FTL"
	ShipmentLTL ShipmentType = "LTL"
)

type CargoStatus string

// CargoStatus values (UPPERCASE everywhere in API and DB).
const (
	StatusPendingModeration CargoStatus = "PENDING_MODERATION"
	StatusSearchingAll      CargoStatus = "SEARCHING_ALL"     // visible to all drivers
	StatusSearchingCompany  CargoStatus = "SEARCHING_COMPANY" // visible only to company drivers
	StatusCompleted         CargoStatus = "COMPLETED"
	StatusCancelled         CargoStatus = "CANCELLED"
)

// IsSearching returns true if status is one of the "searching" variants (cargo visible for offers).
func IsSearching(status CargoStatus) bool {
	return status == StatusSearchingAll || status == StatusSearchingCompany
}

// CanEditCargoRoutePayment returns true while route/payment may still be replaced (not completed / in-flight).
func CanEditCargoRoutePayment(status CargoStatus) bool {
	switch status {
	case StatusPendingModeration, StatusSearchingAll, StatusSearchingCompany:
		return true
	default:
		return false
	}
}

// Documents is the JSON object for cargo.documents (TIR, T1, CMR, etc.).
type Documents struct {
	TIR     bool `json:"TIR,omitempty"`
	T1      bool `json:"T1,omitempty"`
	CMR     bool `json:"CMR,omitempty"`
	Medbook bool `json:"Medbook,omitempty"`
	GLONASS bool `json:"GLONASS,omitempty"`
	Seal    bool `json:"Seal,omitempty"`
	Permit  bool `json:"Permit,omitempty"`
}

// WayPoint is an optional intermediate point for route hints ("drive via"/customs).
type WayPoint struct {
	Type        string  `json:"type"` // TRANSIT | CUSTOMS
	CountryCode string  `json:"country_code,omitempty"`
	CityCode    string  `json:"city_code,omitempty"`
	RegionCode  string  `json:"region_code,omitempty"`
	Address     string  `json:"address,omitempty"`
	Orientir    string  `json:"orientir,omitempty"`
	Lat         float64 `json:"lat,omitempty"`
	Lng         float64 `json:"lng,omitempty"`
	PlaceID     *string `json:"place_id,omitempty"`
	Comment     *string `json:"comment,omitempty"`
}

// Cargo model (table cargo).
type Cargo struct {
	ID     uuid.UUID
	Name   *string
	Weight float64
	Volume float64
	// VehiclesAmount — сколько машин требуется для этого груза.
	VehiclesAmount int
	// VehiclesLeft — сколько машин ещё не «вышли в путь» (уменьшается при переходе рейса в IN_TRANSIT).
	VehiclesLeft         int
	Packaging            *string
	PackagingAmount      *int
	Dimensions           *string
	PhotoURLs            []string
	WayPoints            []WayPoint
	ReadyEnabled         bool
	ReadyAt              *time.Time
	Comment              *string
	TruckType            string
	PowerPlateType       string
	TrailerPlateType     string
	TempMin              *float64
	TempMax              *float64
	ADREnabled           bool
	ADRClass             *string
	LoadingTypes         []string
	UnloadingTypes       []string
	IsTwoDriversRequired bool
	ShipmentType         *ShipmentType
	BeltsCount           *int
	Documents            *Documents
	ContactName          *string
	ContactPhone         *string
	Status               CargoStatus
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time
	// Moderation: admin reject reason (mandatory when status = rejected)
	ModerationRejectionReason *string
	// Кто создал: admin, dispatcher или company (admins, freelance_dispatchers или companies)
	CreatedByType *string // "admin" | "dispatcher" | "company"
	CreatedByID   *uuid.UUID
	// От какой компании груз (опционально; при created_by_type=company совпадает с created_by_id)
	CompanyID   *uuid.UUID
	CargoTypeID *uuid.UUID
	// Denormalised from cargo_types (LEFT JOIN); nil when cargo_type_id is NULL.
	CargoTypeCode   *string
	CargoTypeNameRU *string
	CargoTypeNameUZ *string
	CargoTypeNameEN *string
	CargoTypeNameTR *string
	CargoTypeNameZH *string
}

// RoutePoint model (table route_points).
type RoutePoint struct {
	ID           uuid.UUID
	CargoID      uuid.UUID
	Type         string // load, unload, customs, transit
	CountryCode  string // код страны (UZ, AE, RU и т.д.)
	CityCode     string // код города (TAS, SAM, DXB и т.д.) — из справочника cities
	RegionCode   string // код региона/области — из справочника regions
	Address      string // адрес (улица, дом)
	Orientir     string // ориентир (что написать для водителя)
	Lat          float64
	Lng          float64
	PlaceID      *string
	Comment      *string
	PointOrder   int
	IsMainLoad   bool
	IsMainUnload bool
	// PointAt — плановая дата/время по точке (UTC в API как date).
	PointAt *time.Time
}

// Payment model (table payments).
type Payment struct {
	ID                 uuid.UUID
	CargoID            uuid.UUID
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

// ProposedBy — кто задал цену в оффере (кто должен принять другую сторону).
const (
	OfferProposedByDriver        = "DRIVER"         // водитель предложил цену → принимает диспетчер
	OfferProposedByDispatcher    = "DISPATCHER"     // диспетчер предложил цену → принимает водитель
	OfferProposedByDriverManager = "DRIVER_MANAGER"  // менеджер водителя предложил цену → принимает cargo manager
)

// Offer status values
const (
	OfferStatusPending             = "PENDING"
	OfferStatusAccepted            = "ACCEPTED"
	OfferStatusRejected            = "REJECTED"
	OfferStatusCanceled            = "CANCELED" // отозван автором (своё исходящее / отмена до подтверждения водителем)
	OfferStatusWaitingDriverConfirm = "WAITING_DRIVER_CONFIRM"
)

// Offer model (table offers).
type Offer struct {
	ID                        uuid.UUID
	CargoID                   uuid.UUID
	CarrierID                 uuid.UUID
	Price                     float64
	Currency                  string
	Comment                   *string
	ProposedBy                string     // DRIVER | DISPATCHER | DRIVER_MANAGER
	ProposedByID              *uuid.UUID // ID диспетчера, если предложил диспетчер (или менеджер водителя)
	NegotiationDispatcherID   *uuid.UUID // DM в цепочке при WAITING_DRIVER_CONFIRM (кто предложил DRIVER_MANAGER или кто принял DISPATCHER от CM)
	Status                    string // PENDING, ACCEPTED, REJECTED, CANCELED, WAITING_DRIVER_CONFIRM
	RejectionReason           *string
	CreatedAt                 time.Time
}

// DriverCargoOffer is one driver offer plus minimal cargo/trip info.
// Used by GET /v1/driver/cargo-offers.
type DriverCargoOffer struct {
	Offer
	CargoStatus          CargoStatus
	CargoName            *string
	CargoWeight          float64
	CargoVolume          float64
	CargoTruckType       string
	CargoVehiclesAmount  int
	CargoVehiclesLeft    int
	CargoFromCityCode    *string
	CargoToCityCode      *string
	CargoCurrentPrice    *float64
	CargoCurrentCurrency *string
	CargoCreatedByType   *string
	CargoCreatedByID     *uuid.UUID

	TripID     *uuid.UUID
	TripStatus *string
}

// DispatcherSentOffer is one offer row for GET /v1/dispatchers/offers/all (DISPATCHER or DRIVER_MANAGER on linked cargo, plus cargo/trip context).
type DispatcherSentOffer struct {
	Offer
	CargoStatus          CargoStatus
	CargoName            *string
	CargoFromCityCode    *string
	CargoToCityCode      *string
	CargoVehiclesAmount  int
	CargoVehiclesLeft    int
	CargoCurrentPrice    *float64
	CargoCurrentCurrency *string
	TripID               *uuid.UUID
	TripStatus           *string
}

// DriverAllOffer is one driver-centric offer row for /v1/driver/offers/all (incoming/outgoing).
type DriverAllOffer struct {
	Offer
	CargoStatus          CargoStatus
	CargoName            *string
	CargoFromCityCode    *string
	CargoToCityCode      *string
	CargoVehiclesAmount  int
	CargoVehiclesLeft    int
	CargoCurrentPrice    *float64
	CargoCurrentCurrency *string
	CargoCreatedByType   *string
	CargoCreatedByID     *uuid.UUID
	TripID               *uuid.UUID
	TripStatus           *string
}

// CargoManagerDMOffer is a CM -> Driver Manager offer request. It becomes a normal `offers` row only after DM accepts and selects driver_id.
type CargoManagerDMOffer struct {
	ID             uuid.UUID
	CargoID         uuid.UUID
	CargoManagerID uuid.UUID
	DriverManagerID uuid.UUID
	DriverID       *uuid.UUID
	OfferID        *uuid.UUID
	Price          float64
	Currency       string
	Comment        *string
	Status         string
	RejectionReason *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CargoPhoto is metadata for a cargo photo stored on disk.
type CargoPhoto struct {
	ID         uuid.UUID
	CargoID    uuid.UUID
	UploaderID *uuid.UUID
	Mime       string
	SizeBytes  int64
	Path       string
	CreatedAt  time.Time
}

// CargoPendingPhoto is a photo uploaded before cargo exists (cargo_pending_photos).
type CargoPendingPhoto struct {
	ID        uuid.UUID
	Mime      string
	SizeBytes int64
	Path      string
	CreatedAt time.Time
}

// DocumentsToJSON returns JSON bytes for DB insert/update.
func DocumentsToJSON(d *Documents) ([]byte, error) {
	if d == nil {
		return nil, nil
	}
	return json.Marshal(d)
}

// DocumentsFromJSON parses jsonb from DB.
func DocumentsFromJSON(b []byte) (*Documents, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var d Documents
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
