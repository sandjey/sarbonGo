package tripnotif

import (
	"time"

	"github.com/google/uuid"
)

const (
	RecipientDriver     = "driver"
	RecipientDispatcher = "dispatcher"
	EventInTransit      = "IN_TRANSIT"
	EventDelivered       = "DELIVERED"
	EventCompleted       = "COMPLETED"
	EventCancelled       = "CANCELLED"
	EventCompletionPending = "COMPLETION_PENDING_MANAGER"
)

// Row is a persisted notification.
type Row struct {
	ID             uuid.UUID
	TripID         uuid.UUID
	RecipientKind  string
	RecipientID    uuid.UUID
	EventKind      string
	FromStatus     *string
	ToStatus       *string
	ReadAt         *time.Time
	CreatedAt      time.Time
}
