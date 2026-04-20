package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/chat"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

// DispatcherUnifiedSSEHandler serves two unified SSE streams per dispatcher role:
//
//   - events       — everything the UI may need to refresh in real time
//     (inbox notifications + trip_status snapshots + driver_updates + chat messages/calls).
//   - notifications — only what a user would see in the bell/inbox list
//     (inbox notifications + driver_updates + chat messages/calls).
//
// Both are available separately under /dispatchers/cargo-manager/... and /dispatchers/driver-manager/...
// with manager_role enforced by mw.RequireDispatcherManagerRole.
type DispatcherUnifiedSSEHandler struct {
	hub     *userstream.Hub
	chatHub *chat.Hub
}

func NewDispatcherUnifiedSSEHandler(hub *userstream.Hub, chatHub *chat.Hub) *DispatcherUnifiedSSEHandler {
	return &DispatcherUnifiedSSEHandler{hub: hub, chatHub: chatHub}
}

// EventsForCargoManager GET /v1/dispatchers/cargo-manager/sse/events.
func (h *DispatcherUnifiedSSEHandler) EventsForCargoManager(c *gin.Context) {
	h.run(c, dispatchers.ManagerRoleCargoManager, true)
}

// NotificationsForCargoManager GET /v1/dispatchers/cargo-manager/sse/notifications.
func (h *DispatcherUnifiedSSEHandler) NotificationsForCargoManager(c *gin.Context) {
	h.run(c, dispatchers.ManagerRoleCargoManager, false)
}

// EventsForDriverManager GET /v1/dispatchers/driver-manager/sse/events.
func (h *DispatcherUnifiedSSEHandler) EventsForDriverManager(c *gin.Context) {
	h.run(c, dispatchers.ManagerRoleDriverManager, true)
}

// NotificationsForDriverManager GET /v1/dispatchers/driver-manager/sse/notifications.
func (h *DispatcherUnifiedSSEHandler) NotificationsForDriverManager(c *gin.Context) {
	h.run(c, dispatchers.ManagerRoleDriverManager, false)
}

// run is the unified SSE loop. includeTripStatus=true enables the trip_status snapshot stream
// (only useful for the "events" endpoint — UI state sync). For notifications endpoint the trip
// state changes arrive via the notifications stream as kind=trip_notification anyway.
func (h *DispatcherUnifiedSSEHandler) run(c *gin.Context, managerRole string, includeTripStatus bool) {
	if h.hub == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}

	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	notifCh, unNotif := h.hub.SubscribeNotifications(tripnotif.RecipientDispatcher, dispID)
	drvCh, unDrv := h.hub.SubscribeDriverUpdates(tripnotif.RecipientDispatcher, dispID)
	defer unNotif()
	defer unDrv()

	var tripCh <-chan []byte
	if includeTripStatus {
		c2, un := h.hub.SubscribeTripStatus(tripnotif.RecipientDispatcher, dispID)
		tripCh = c2
		defer un()
	}

	var chatCh <-chan []byte
	if h.chatHub != nil {
		c2, un := h.chatHub.SubscribeUser(dispID)
		chatCh = c2
		defer un()
	}

	if _, err := io.WriteString(c.Writer, ": sse connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()

	writeFrame := func(streamKind string, raw []byte) bool {
		env := buildDispatcherSSEEnvelope(streamKind, managerRole, dispID, raw)
		out, err := json.Marshal(env)
		if err != nil {
			return true
		}
		if _, err := c.Writer.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := c.Writer.Write(out); err != nil {
			return false
		}
		if _, err := c.Writer.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-tick.C:
			if _, err := io.WriteString(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-notifCh:
			if !ok {
				return
			}
			if !writeFrame("notifications", msg) {
				return
			}
		case msg, ok := <-drvCh:
			if !ok {
				return
			}
			if !writeFrame("driver_updates", msg) {
				return
			}
		case msg, ok := <-tripCh:
			if !ok {
				// tripCh may be nil — receiving from a nil channel blocks forever, so this case
				// is naturally disabled when includeTripStatus == false.
				return
			}
			if !writeFrame("trip_status", msg) {
				return
			}
		case msg, ok := <-chatCh:
			if !ok {
				// same as above when chatHub == nil.
				return
			}
			if !writeFrame("chat", msg) {
				return
			}
		}
	}
}

// dispatcherSSEEnvelope is the wire shape of every SSE frame emitted by dispatcher unified SSE.
// It is intentionally flat and self-descriptive: a frontend can switch on `type` and use `id`
// as the primary entity reference (notification_id / offer_id / invitation_id / driver_id /
// trip_id / message_id), with the original payload preserved under `data`.
type dispatcherSSEEnvelope struct {
	Type           string          `json:"type"`
	ID             string          `json:"id,omitempty"`
	EventKind      string          `json:"event_kind,omitempty"`
	StreamKind     string          `json:"stream_kind"`
	RecipientKind  string          `json:"recipient_kind"`
	RecipientID    string          `json:"recipient_id"`
	DispatcherRole string          `json:"dispatcher_role"`
	At             string          `json:"at"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// buildDispatcherSSEEnvelope inspects the raw hub payload (userstream/chat) and extracts a
// canonical (type, id, event_kind) discriminator while keeping the original body as `data`.
//
// Mapping rules:
//
//	streamKind=notifications:
//	  kind=trip_notification  → type=trip_notification, id=notification_id|trip_id, event_kind=event_kind
//	  kind=cargo_offer        → type=cargo_offer,       id=offer_id,               event_kind=event|event_kind|action
//	  kind=connection_offer   → type=connection_offer,  id=invitation_id|token,    event_kind=event|action
//	  kind=driver_update      → type=driver_update,     id=driver_id,              event_kind=event|action
//	  (unknown kind)          → type=<kind>,            id=payload.id,             event_kind=<event>
//	streamKind=driver_updates → type=driver_update, id=driver_id, event_kind=action|event
//	streamKind=trip_status   → type=trip_status,   id=trip_id,   event_kind=status|trip_status
//	streamKind=chat           → type=message|call,  id=message_id|call_id,         event_kind=<envelope.type>
func buildDispatcherSSEEnvelope(streamKind, managerRole string, recipientID uuid.UUID, raw []byte) dispatcherSSEEnvelope {
	env := dispatcherSSEEnvelope{
		StreamKind:     streamKind,
		RecipientKind:  tripnotif.RecipientDispatcher,
		RecipientID:    recipientID.String(),
		DispatcherRole: strings.ToUpper(strings.TrimSpace(managerRole)),
		At:             time.Now().UTC().Format(time.RFC3339),
		Data:           json.RawMessage(append([]byte(nil), raw...)),
	}

	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		env.Type = streamKind
		return env
	}

	switch streamKind {
	case "trip_status":
		env.Type = "trip_status"
		env.ID = pickString(m, "trip_id", "id")
		env.EventKind = pickString(m, "status", "trip_status", "event_kind")
	case "driver_updates":
		env.Type = "driver_update"
		env.ID = pickString(m, "driver_id", "id")
		env.EventKind = pickString(m, "action", "event", "event_kind")
	case "notifications":
		kind := strings.TrimSpace(pickString(m, "kind"))
		switch kind {
		case "trip_notification":
			env.Type = "trip_notification"
			env.ID = pickString(m, "notification_id", "trip_id")
			env.EventKind = pickString(m, "event_kind", "status")
		case "cargo_offer":
			env.Type = "cargo_offer"
			env.ID = pickString(m, "offer_id", "id")
			env.EventKind = pickString(m, "event", "event_kind", "action")
		case "connection_offer":
			env.Type = "connection_offer"
			env.ID = pickString(m, "invitation_id", "token", "id")
			env.EventKind = pickString(m, "event", "event_kind", "action")
		case "driver_update":
			env.Type = "driver_update"
			env.ID = pickString(m, "driver_id", "id")
			env.EventKind = pickString(m, "event", "action")
		default:
			env.Type = firstNonEmpty(kind, "notification")
			env.ID = pickString(m, "id", "notification_id")
			env.EventKind = pickString(m, "event", "event_kind", "action")
		}
	case "chat":
		t := strings.TrimSpace(pickString(m, "type"))
		switch {
		case t == "message":
			env.Type = "message"
		case strings.HasPrefix(t, "call."):
			env.Type = "call"
		case t != "":
			env.Type = t
		default:
			env.Type = "chat"
		}
		env.EventKind = t
		// Try to reach envelope.data.{id, call_id, message_id} first; fall back to root.
		if data, ok := m["data"].(map[string]any); ok {
			env.ID = pickStringFrom(data, "message_id", "id", "call_id")
			if env.ID == "" {
				env.ID = pickString(m, "id", "call_id", "message_id")
			}
		} else {
			env.ID = pickString(m, "id", "call_id", "message_id")
		}
	default:
		env.Type = streamKind
	}

	return env
}

func pickString(m map[string]any, keys ...string) string {
	return pickStringFrom(m, keys...)
}

func pickStringFrom(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
