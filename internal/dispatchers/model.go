package dispatchers

import "time"

// Freelance dispatcher roles (API: JSON field "role", DB: manager_role). Uppercase only.
const (
	ManagerRoleCargoManager  = "CARGO_MANAGER"
	ManagerRoleDriverManager = "DRIVER_MANAGER"
)

// Mirrors DB columns from tables:
// - freelance_dispatchers
// - deleted_freelance_dispatchers
type Dispatcher struct {
	ID string `json:"id"`

	Name     *string `json:"name"`
	Phone    string  `json:"phone"`
	Password string `json:"-"` // bcrypt hash; never serialize

	PassportSeries *string `json:"passport_series"`
	PassportNumber *string `json:"passport_number"`
	PINFL          *string `json:"pinfl"`

	CargoID  *string `json:"cargo_id"`
	DriverID *string `json:"driver_id"`

	Rating     *float64 `json:"rating"`
	WorkStatus *string  `json:"work_status"`
	Status     *string  `json:"status"`

	// ManagerRole — роль фриланс-диспетчера (грузовой / водительский менеджер). В JSON: "role".
	ManagerRole *string `json:"role,omitempty"`

	Photo     *string `json:"photo,omitempty"`      // ссылка/путь (устаревшее), для загруженного фото см. has_photo
	HasPhoto  bool    `json:"has_photo"`             // true если загружено фото в БД (получить через GET /profile/photo)

	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastOnlineAt *time.Time `json:"last_online_at"`
	DeletedAt    *time.Time `json:"deleted_at"`

	// Catalog counters (used by /v1/dispatchers/dispatchers/catalog and driver catalog).
	// For roles where metric is not applicable, backend returns 0.
	CargoCount int64 `json:"cargo_count"`
	OfferCount int64 `json:"offer_count"`
	TripCount  int64 `json:"trip_count"`
}
