package adminanalytics

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const zeroScopeUUID = "00000000-0000-0000-0000-000000000000"

const (
	EventUserRegistered    = "user_registered"
	EventLoginSuccess      = "login_success"
	EventLoginFailed       = "login_failed"
	EventSessionStarted    = "session_started"
	EventSessionEnded      = "session_ended"
	EventCargoCreated      = "cargo_created"
	EventOfferCreated      = "offer_created"
	EventOfferAccepted     = "offer_accepted"
	EventTripStarted       = "trip_started"
	EventTripCompleted     = "trip_completed"
	EventChatMessageSent   = "chat_message_sent"
	EventCallStarted       = "call_started"
	EventCallEnded         = "call_ended"
	EventAdminAction       = "admin_action_performed"
	EntityUser             = "user"
	EntitySession          = "session"
	EntityCargo            = "cargo"
	EntityOffer            = "offer"
	EntityTrip             = "trip"
	EntityChatConversation = "chat_conversation"
	EntityChatMessage      = "chat_message"
	EntityCall             = "call"
	EntityAdminAction      = "admin_action"
	RoleDriver             = "driver"
	RoleCargoManager       = "cargo_manager"
	RoleDriverManager      = "driver_manager"
	RoleAdmin              = "admin"
	RoleUnknown            = "unknown"
)

type EventInput struct {
	EventName  string
	EventTime  time.Time
	UserID     *uuid.UUID
	Role       string
	EntityType string
	EntityID   *uuid.UUID
	ActorID    *uuid.UUID
	SessionID  string
	DeviceType string
	Platform   string
	IPHash     string
	GeoCity    string
	Metadata   map[string]any
}

type TimeWindow struct {
	From time.Time
	To   time.Time
	TZ   string
}

type Page struct {
	Limit   int
	Offset  int
	SortBy  string
	SortDir string
}

type UserLookup struct {
	ID            uuid.UUID  `json:"id"`
	Role          string     `json:"role"`
	DisplayName   string     `json:"display_name"`
	PhoneOrLogin  string     `json:"phone_or_login"`
	Status        string     `json:"status"`
	RegisteredAt  time.Time  `json:"registered_at"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	ManagerRole   *string    `json:"manager_role,omitempty"`
	AdminType     *string    `json:"admin_type,omitempty"`
	PrimarySource string     `json:"primary_source"`
}

type MetricPoint struct {
	Bucket string         `json:"bucket,omitempty"`
	Role   string         `json:"role,omitempty"`
	UserID *uuid.UUID     `json:"user_id,omitempty"`
	Values map[string]any `json:"values"`
}

type FlowMetric struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Count       int64    `json:"count"`
	AverageSec  *float64 `json:"average_sec,omitempty"`
	MedianSec   *float64 `json:"median_sec,omitempty"`
	P95Sec      *float64 `json:"p95_sec,omitempty"`
}

func NormalizeRole(role string) string {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "DRIVER":
		return RoleDriver
	case "CARGO_MANAGER":
		return RoleCargoManager
	case "DRIVER_MANAGER":
		return RoleDriverManager
	case "ADMIN":
		return RoleAdmin
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func ScopeUserID(id *uuid.UUID) uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return uuid.MustParse(zeroScopeUUID)
	}
	return *id
}
