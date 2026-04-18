package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

// SSEStreamsHandler serves Server-Sent Events for trip notifications and trip status (in-process hub; one API replica unless you add Redis bridge).
type SSEStreamsHandler struct {
	hub *userstream.Hub
}

func NewSSEStreamsHandler(hub *userstream.Hub) *SSEStreamsHandler {
	return &SSEStreamsHandler{hub: hub}
}

func (h *SSEStreamsHandler) writeData(c *gin.Context, flusher http.Flusher, b []byte) error {
	if _, err := c.Writer.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := c.Writer.Write(b); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// mergeStreamMeta adds FCM-aligned top-level keys (same names as push data map) onto the JSON line.
func mergeStreamMeta(streamKind, recipientKind string, recipientID uuid.UUID, inner []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(inner, &m); err != nil {
		m = map[string]any{"_parse_error": true}
	}
	m["stream_kind"] = streamKind
	m["recipient_kind"] = recipientKind
	m["recipient_id"] = recipientID.String()
	return json.Marshal(m)
}

func (h *SSEStreamsHandler) runSSE(c *gin.Context, subscribe func() (<-chan []byte, func())) {
	if h.hub == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	ch, unsub := subscribe()
	defer unsub()

	if _, err := io.WriteString(c.Writer, ": sse connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-tick.C:
			if _, err := io.WriteString(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := h.writeData(c, flusher, msg); err != nil {
				return
			}
		}
	}
}

// DriverTripNotificationsSSE GET /v1/driver/sse/trip-notifications — полный inbox (рейсы + офферы + connection_offer).
func (h *SSEStreamsHandler) DriverTripNotificationsSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeNotifications(tripnotif.RecipientDriver, id)
	})
}

// DriverCargoOffersSSE GET /v1/driver/sse/cargo-offers — только события kind=cargo_offer.
func (h *SSEStreamsHandler) DriverCargoOffersSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeCargoOfferNotifications(tripnotif.RecipientDriver, id)
	})
}

// DriverConnectionsSSE GET /v1/driver/sse/connections — только kind=connection_offer.
func (h *SSEStreamsHandler) DriverConnectionsSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeConnectionNotifications(tripnotif.RecipientDriver, id)
	})
}

// DriverTripStatusSSE GET /v1/driver/sse/trip-status
func (h *SSEStreamsHandler) DriverTripStatusSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeTripStatus(tripnotif.RecipientDriver, id)
	})
}

// DriverRealtimeSSE GET /v1/driver/sse/realtime — один поток: уведомления + trip_status с полями как у FCM data (stream_kind, recipient_*).
func (h *SSEStreamsHandler) DriverRealtimeSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	if h.hub == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	notifCh, unNotif := h.hub.SubscribeNotifications(tripnotif.RecipientDriver, id)
	tripCh, unTrip := h.hub.SubscribeTripStatus(tripnotif.RecipientDriver, id)
	defer unNotif()
	defer unTrip()

	if _, err := io.WriteString(c.Writer, ": sse connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()

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
			out, err := mergeStreamMeta("notifications", tripnotif.RecipientDriver, id, msg)
			if err != nil {
				continue
			}
			if err := h.writeData(c, flusher, out); err != nil {
				return
			}
		case msg, ok := <-tripCh:
			if !ok {
				return
			}
			out, err := mergeStreamMeta("trip_status", tripnotif.RecipientDriver, id, msg)
			if err != nil {
				continue
			}
			if err := h.writeData(c, flusher, out); err != nil {
				return
			}
		}
	}
}

// DispatcherTripNotificationsSSE GET /v1/dispatchers/sse/trip-notifications — только trip_notification.
func (h *SSEStreamsHandler) DispatcherTripNotificationsSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeTripUserNotifications(tripnotif.RecipientDispatcher, id)
	})
}

// DispatcherCargoOffersSSE GET /v1/dispatchers/sse/cargo-offers — только cargo_offer.
func (h *SSEStreamsHandler) DispatcherCargoOffersSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeCargoOfferNotifications(tripnotif.RecipientDispatcher, id)
	})
}

// DispatcherConnectionsSSE GET /v1/dispatchers/sse/connections — только connection_offer.
func (h *SSEStreamsHandler) DispatcherConnectionsSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeConnectionNotifications(tripnotif.RecipientDispatcher, id)
	})
}

// DispatcherTripStatusSSE GET /v1/dispatchers/sse/trip-status
func (h *SSEStreamsHandler) DispatcherTripStatusSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeTripStatus(tripnotif.RecipientDispatcher, id)
	})
}

// DispatcherDriverUpdatesSSE GET /v1/dispatchers/sse/driver-updates
func (h *SSEStreamsHandler) DispatcherDriverUpdatesSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeDriverUpdates(tripnotif.RecipientDispatcher, id)
	})
}
