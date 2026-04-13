package handlers

import (
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

// DriverTripNotificationsSSE GET /v1/driver/sse/trip-notifications
func (h *SSEStreamsHandler) DriverTripNotificationsSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeNotifications(tripnotif.RecipientDriver, id)
	})
}

// DriverTripStatusSSE GET /v1/driver/sse/trip-status
func (h *SSEStreamsHandler) DriverTripStatusSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeTripStatus(tripnotif.RecipientDriver, id)
	})
}

// DispatcherTripNotificationsSSE GET /v1/dispatchers/sse/trip-notifications
func (h *SSEStreamsHandler) DispatcherTripNotificationsSSE(c *gin.Context) {
	id := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.runSSE(c, func() (<-chan []byte, func()) {
		return h.hub.SubscribeNotifications(tripnotif.RecipientDispatcher, id)
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
