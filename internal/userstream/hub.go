package userstream

import (
	"encoding/json"
	"sync"

	"github.com/google/uuid"
)

const subChanBuf = 48

// Hub delivers JSON payloads to SSE subscribers keyed by recipient_kind + recipient_id (UUID of driver or dispatcher app user).
// One process only: for horizontal scaling add Redis Pub/Sub and bridge to this hub per instance.
type Hub struct {
	mu    sync.RWMutex
	notif map[string][]chan []byte
	trips map[string][]chan []byte
	drv   map[string][]chan []byte
}

func NewHub() *Hub {
	return &Hub{
		notif: make(map[string][]chan []byte),
		trips: make(map[string][]chan []byte),
		drv:   make(map[string][]chan []byte),
	}
}

func subKey(recipientKind string, recipientID uuid.UUID) string {
	return recipientKind + ":" + recipientID.String()
}

// SubscribeNotifications registers a subscriber for trip_user_notifications–style pushes.
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

// PublishNotification sends one SSE payload to all notification-stream subscribers for this recipient.
func (h *Hub) PublishNotification(recipientKind string, recipientID uuid.UUID, v any) {
	if h == nil || recipientID == uuid.Nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.broadcast(h.notif, subKey(recipientKind, recipientID), b)
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
