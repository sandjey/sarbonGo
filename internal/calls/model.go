package calls

import (
	"time"

	"github.com/google/uuid"
)

// Status is persisted in DB as enum call_status.
type Status string

const (
	StatusRinging   Status = "RINGING"
	StatusActive    Status = "ACTIVE"
	StatusEnded     Status = "ENDED"
	StatusDeclined  Status = "DECLINED"
	StatusMissed    Status = "MISSED"
	StatusCancelled Status = "CANCELLED"
	StatusFailed    Status = "FAILED"
)

type Call struct {
	ID              uuid.UUID  `json:"id"`
	ConversationID  *uuid.UUID `json:"conversation_id"`
	CallerID        uuid.UUID  `json:"caller_id"`
	CalleeID        uuid.UUID  `json:"callee_id"`
	Status          Status     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	EndedBy         *uuid.UUID `json:"ended_by,omitempty"`
	EndedReason     *string    `json:"ended_reason,omitempty"`
	ClientRequestID *string    `json:"client_request_id,omitempty"`
}

// CallListItem is a call row enriched for list endpoints.
// Name is the counterparty display name for current user context.
type CallListItem struct {
	Call
	Name string `json:"name"`
}

type Event struct {
	ID        uuid.UUID  `json:"id"`
	CallID    uuid.UUID  `json:"call_id"`
	ActorID   *uuid.UUID `json:"actor_id,omitempty"`
	EventType string     `json:"event_type"`
	Payload   []byte     `json:"payload,omitempty"` // raw JSON
	CreatedAt time.Time  `json:"created_at"`
}

