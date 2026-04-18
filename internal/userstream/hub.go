package userstream

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const subChanBuf = 48

// Hub delivers JSON payloads to SSE subscribers keyed by recipient_kind + recipient_id (UUID of driver or dispatcher app user).
// One process only: for horizontal scaling add Redis Pub/Sub and bridge to this hub per instance.
type Hub struct {
	mu              sync.RWMutex
	notif           map[string][]chan []byte // full inbox (trip + cargo_offer + connection_offer)
	tripUserNotif   map[string][]chan []byte // kind=trip_notification only
	cargoOfferNotif map[string][]chan []byte // kind=cargo_offer only
	connNotif       map[string][]chan []byte // kind=connection_offer only
	trips           map[string][]chan []byte
	drv             map[string][]chan []byte
	onPublish       func(streamKind, recipientKind string, recipientID uuid.UUID, payload []byte)
}

func NewHub() *Hub {
	return &Hub{
		notif:           make(map[string][]chan []byte),
		tripUserNotif:   make(map[string][]chan []byte),
		cargoOfferNotif: make(map[string][]chan []byte),
		connNotif:       make(map[string][]chan []byte),
		trips:           make(map[string][]chan []byte),
		drv:             make(map[string][]chan []byte),
	}
}

func subKey(recipientKind string, recipientID uuid.UUID) string {
	return recipientKind + ":" + recipientID.String()
}

// SubscribeNotifications registers a subscriber for the full notification inbox
// (trip_notification, cargo_offer, connection_offer) for this recipient.
func (h *Hub) SubscribeNotifications(recipientKind string, recipientID uuid.UUID) (ch <-chan []byte, unsubscribe func()) {
	if h == nil || recipientID == uuid.Nil {
		return nil, func() {}
	}
	c := make(chan []byte, subChanBuf)
	k := subKey(recipientKind, recipientID)
	h.mu.Lock()
	h.notif[k] = append(h.notif[k], c)
	h.mu.Unlock()
	return c, func() { h.remove(h.notif, k, c) }
}

// SubscribeTripUserNotifications registers only trip_notification payloads (рейсы / trip_user_notifications).
func (h *Hub) SubscribeTripUserNotifications(recipientKind string, recipientID uuid.UUID) (ch <-chan []byte, unsubscribe func()) {
	if h == nil || recipientID == uuid.Nil {
		return nil, func() {}
	}
	c := make(chan []byte, subChanBuf)
	k := subKey(recipientKind, recipientID)
	h.mu.Lock()
	h.tripUserNotif[k] = append(h.tripUserNotif[k], c)
	h.mu.Unlock()
	return c, func() { h.remove(h.tripUserNotif, k, c) }
}

// SubscribeCargoOfferNotifications registers only cargo_offer SSE payloads.
func (h *Hub) SubscribeCargoOfferNotifications(recipientKind string, recipientID uuid.UUID) (ch <-chan []byte, unsubscribe func()) {
	if h == nil || recipientID == uuid.Nil {
		return nil, func() {}
	}
	c := make(chan []byte, subChanBuf)
	k := subKey(recipientKind, recipientID)
	h.mu.Lock()
	h.cargoOfferNotif[k] = append(h.cargoOfferNotif[k], c)
	h.mu.Unlock()
	return c, func() { h.remove(h.cargoOfferNotif, k, c) }
}

// SubscribeConnectionNotifications registers only connection_offer payloads (приглашения, связи).
func (h *Hub) SubscribeConnectionNotifications(recipientKind string, recipientID uuid.UUID) (ch <-chan []byte, unsubscribe func()) {
	if h == nil || recipientID == uuid.Nil {
		return nil, func() {}
	}
	c := make(chan []byte, subChanBuf)
	k := subKey(recipientKind, recipientID)
	h.mu.Lock()
	h.connNotif[k] = append(h.connNotif[k], c)
	h.mu.Unlock()
	return c, func() { h.remove(h.connNotif, k, c) }
}

// SubscribeTripStatus registers a subscriber for trip status snapshots.
func (h *Hub) SubscribeTripStatus(recipientKind string, recipientID uuid.UUID) (ch <-chan []byte, unsubscribe func()) {
	if h == nil || recipientID == uuid.Nil {
		return nil, func() {}
	}
	c := make(chan []byte, subChanBuf)
	k := subKey(recipientKind, recipientID)
	h.mu.Lock()
	h.trips[k] = append(h.trips[k], c)
	h.mu.Unlock()
	return c, func() { h.remove(h.trips, k, c) }
}

// SubscribeDriverUpdates registers a subscriber for driver profile/vehicle update notifications.
func (h *Hub) SubscribeDriverUpdates(recipientKind string, recipientID uuid.UUID) (ch <-chan []byte, unsubscribe func()) {
	if h == nil || recipientID == uuid.Nil {
		return nil, func() {}
	}
	c := make(chan []byte, subChanBuf)
	k := subKey(recipientKind, recipientID)
	h.mu.Lock()
	h.drv[k] = append(h.drv[k], c)
	h.mu.Unlock()
	return c, func() { h.remove(h.drv, k, c) }
}

func (h *Hub) remove(m map[string][]chan []byte, k string, c chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	arr := m[k]
	for i, ch := range arr {
		if ch == c {
			m[k] = append(arr[:i], arr[i+1:]...)
			break
		}
	}
	if len(m[k]) == 0 {
		delete(m, k)
	}
}

func notificationKindFromJSON(b []byte) string {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	k, _ := m["kind"].(string)
	return strings.TrimSpace(k)
}

// PublishNotification sends to the full inbox and to a typed sub-stream by JSON `kind`
// (trip_notification | cargo_offer | connection_offer) for filtered SSE endpoints.
func (h *Hub) PublishNotification(recipientKind string, recipientID uuid.UUID, v any) {
	if h == nil || recipientID == uuid.Nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	k := subKey(recipientKind, recipientID)
	h.broadcast(h.notif, k, b)
	switch notificationKindFromJSON(b) {
	case "trip_notification":
		h.broadcast(h.tripUserNotif, k, b)
	case "cargo_offer":
		h.broadcast(h.cargoOfferNotif, k, b)
	case "connection_offer":
		h.broadcast(h.connNotif, k, b)
	}
	h.mu.RLock()
	cb := h.onPublish
	h.mu.RUnlock()
	if cb != nil {
		cb("notifications", recipientKind, recipientID, b)
	}
}

// PublishTripStatus sends one SSE payload to all trip-status subscribers for this recipient.
func (h *Hub) PublishTripStatus(recipientKind string, recipientID uuid.UUID, v any) {
	if h == nil || recipientID == uuid.Nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.broadcast(h.trips, subKey(recipientKind, recipientID), b)
	h.mu.RLock()
	cb := h.onPublish
	h.mu.RUnlock()
	if cb != nil {
		cb("trip_status", recipientKind, recipientID, b)
	}
}

// PublishDriverUpdate sends one SSE payload to all driver-update stream subscribers for this recipient.
func (h *Hub) PublishDriverUpdate(recipientKind string, recipientID uuid.UUID, v any) {
	if h == nil || recipientID == uuid.Nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.broadcast(h.drv, subKey(recipientKind, recipientID), b)
	h.mu.RLock()
	cb := h.onPublish
	h.mu.RUnlock()
	if cb != nil {
		cb("driver_updates", recipientKind, recipientID, b)
	}
}

func (h *Hub) SetOnPublish(f func(streamKind, recipientKind string, recipientID uuid.UUID, payload []byte)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onPublish = f
}

func (h *Hub) broadcast(m map[string][]chan []byte, k string, payload []byte) {
	h.mu.RLock()
	subs := append([]chan []byte(nil), m[k]...)
	h.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- payload:
		default:
			// slow consumer: drop this event
		}
	}
}
