package tripnotif

import (
	"time"

	"github.com/google/uuid"
)

const (
	RecipientDriver        = "driver"
	RecipientDispatcher    = "dispatcher"
	EventInTransit         = "IN_TRANSIT"
	EventDelivered         = "DELIVERED"
	EventCompleted         = "COMPLETED"
	EventCancelled         = "CANCELLED"
	EventCompletionPending = "COMPLETION_PENDING_MANAGER"
)

// Notification categories exposed to the mobile clients.
const (
	TypeTripNotification  = "trip_notification"
	TypeCargoOffer        = "cargo_offer"
	TypeConnectionOffer   = "connection_offer"
	TypeMessage           = "message"
	TypeCall              = "call"
	TypeDriverProfileEdit = "driver_profile_edit"
)

// Row is a persisted notification.
// TripID is nil for non-trip notifications (cargo_offer, connection_offer, chat message, call, driver profile edit).
type Row struct {
	ID            uuid.UUID
	TripID        *uuid.UUID
	RecipientKind string
	RecipientID   uuid.UUID
	EventKind     string
	EventType     *string
	Payload       []byte
	FromStatus    *string
	ToStatus      *string
	ReadAt        *time.Time
	CreatedAt     time.Time
}
