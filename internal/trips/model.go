package trips

import (
	"time"

	"github.com/google/uuid"
)

// Trip operational statuses (linear flow). Legacy statuses are migrated in DB.
const (
	StatusInProgress = "IN_PROGRESS"
	StatusInTransit  = "IN_TRANSIT"
	StatusDelivered  = "DELIVERED"
	StatusCompleted  = "COMPLETED"
	StatusCancelled  = "CANCELLED"
)

type Trip struct {
	ID        uuid.UUID
	CargoID   uuid.UUID
	OfferID   uuid.UUID
	DriverID  *uuid.UUID
	Status    string
	AgreedPrice    float64
	AgreedCurrency string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Legacy bilateral fields (optional; unused in new unilateral flow).
	PendingConfirmTo      *string
	DriverConfirmedAt     *time.Time
	DispatcherConfirmedAt *time.Time
}
