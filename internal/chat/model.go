package chat

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Conversation between two users (user_a_id < user_b_id in DB).
type Conversation struct {
	ID        uuid.UUID `json:"id"`
	UserAID   uuid.UUID `json:"user_a_id"`
	UserBID   uuid.UUID `json:"user_b_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Message in a conversation.
type Message struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	SenderID       uuid.UUID  `json:"sender_id"`
	Type           string     `json:"type"` // text | img | audio | video | video_note | location (+ legacy TEXT, PHOTO, …)
	Body           *string    `json:"body,omitempty"` // optional caption/text
	Payload        json.RawMessage `json:"payload,omitempty"` // type-specific JSON object
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	// DeliveredAt is set once the recipient's client has received the message
	// (their WebSocket acknowledged delivery, or they were online at send time).
	// Empty while the message is queued — "single-check" UX. Compare with
	// ReadByPeer (from conversation read cursor) for the "double-check" state.
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	// Read flags (filled by ListMessages from chat_conversation_reads cursors).
	ReadByMe   bool `json:"read_by_me"`   // true if this message is from the peer and the current user has read it
	ReadByPeer bool `json:"read_by_peer"` // true if this message is from the current user and the peer has read it
}

// PeerID returns the other participant's ID for the given user.
func (c *Conversation) PeerID(me uuid.UUID) uuid.UUID {
	if c.UserAID == me {
		return c.UserBID
	}
	return c.UserAID
}

// ConversationListItem is a Telegram-like row for GET /v1/chat/conversations.
type ConversationListItem struct {
	Conversation
	PeerID       uuid.UUID  `json:"peer_id"`
	PeerName     string     `json:"peer_name"`
	PeerPhone    string     `json:"peer_phone"`
	PeerRole     string     `json:"peer_role"` // driver | dispatcher | unknown
	PeerHasPhoto bool       `json:"peer_has_photo"`
	PeerPhotoURL *string    `json:"peer_photo_url,omitempty"`

	LastMessageID      *uuid.UUID `json:"last_message_id,omitempty"`
	LastMessageAt      *time.Time `json:"last_message_at,omitempty"`
	LastMessageType    *string    `json:"last_message_type,omitempty"`
	LastMessageBody    *string    `json:"last_message_body,omitempty"`
	LastMessagePreview string     `json:"last_message_preview,omitempty"`
	LastMessageFromMe  bool       `json:"last_message_from_me"`
	UnreadCount        int        `json:"unread_count"`
	PeerReadMyLast     bool       `json:"peer_read_my_last"` // true if last message is mine and peer has read it
}
