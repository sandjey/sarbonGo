package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/triprating"
)

func tripNotifMessageKey(eventKind string) string {
	switch eventKind {
	case tripnotif.EventInTransit:
		return "trip_notif_in_transit"
	case tripnotif.EventDelivered:
		return "trip_notif_delivered"
	case tripnotif.EventCompleted:
		return "trip_notif_completed"
	case tripnotif.EventCancelled:
		return "trip_notif_cancelled"
	case tripnotif.EventCompletionPending:
		return "trip_notif_completion_pending_manager"
	default:
		return "ok"
	}
}

func isClientDisconnect(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func notificationType(eventKind string) string {
	ek := strings.ToLower(strings.TrimSpace(eventKind))
	switch {
	case strings.HasPrefix(ek, "connection_offer"):
		return tripnotif.TypeConnectionOffer
	case strings.HasPrefix(ek, "cargo_offer"):
		return tripnotif.TypeCargoOffer
	case strings.HasPrefix(ek, "call."):
		return tripnotif.TypeCall
	case strings.HasPrefix(ek, "message"):
		return tripnotif.TypeMessage
	case strings.HasPrefix(ek, "driver.profile."), strings.HasPrefix(ek, "driver_update"), strings.HasPrefix(ek, "dispatcher.driver."):
		return tripnotif.TypeDriverProfileEdit
	default:
		return tripnotif.TypeTripNotification
	}
}

func tripNotificationRowToGin(c *gin.Context, n tripnotif.Row) gin.H {
	step := n.EventKind
	if n.ToStatus != nil && strings.TrimSpace(*n.ToStatus) != "" {
		step = strings.TrimSpace(*n.ToStatus)
	}

	typ := notificationType(n.EventKind)
	if n.EventType != nil && strings.TrimSpace(*n.EventType) != "" {
		typ = strings.TrimSpace(*n.EventType)
	}

	var tripID any
	if n.TripID != nil && *n.TripID != uuid.Nil {
		tripID = n.TripID.String()
	}

	var payload any
	if len(n.Payload) > 0 {
		var decoded any
		if err := json.Unmarshal(n.Payload, &decoded); err == nil {
			payload = decoded
		}
	}

	messageKey := tripNotifMessageKey(n.EventKind)
	message := resp.Msg(messageKey, resp.Lang(c))
	if typ != tripnotif.TypeTripNotification {
		// Non-trip notifications do not have a i18n key; surface the event kind as a human-friendly fallback.
		message = strings.TrimSpace(n.EventKind)
		if message == "" {
			message = typ
		}
	}

	return gin.H{
		"id":         n.ID.String(),
		"trip_id":    tripID,
		"event_kind": n.EventKind,
		"type":       typ,
		"step":       step,
		"from_status": func() any {
			if n.FromStatus == nil {
				return nil
			}
			return *n.FromStatus
		}(),
		"to_status": func() any {
			if n.ToStatus == nil {
				return nil
			}
			return *n.ToStatus
		}(),
		"message":    message,
		"payload":    payload,
		"read":       n.ReadAt != nil,
		"read_at":    n.ReadAt,
		"created_at": n.CreatedAt,
	}
}

func (h *TripsHandler) listTripNotifications(c *gin.Context, recipientKind string, recipientID uuid.UUID) {
	if h.notif == nil {
		resp.OKLang(c, "ok", gin.H{"items": []gin.H{}, "unread_count": 0})
		return
	}
	unreadOnly := strings.EqualFold(strings.TrimSpace(c.Query("unread_only")), "true") || c.Query("unread_only") == "1"
	limit := 30
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	list, err := h.notif.List(c.Request.Context(), recipientKind, recipientID, unreadOnly, limit)
	if err != nil {
		if isClientDisconnect(err) {
			// Browser cancelled the request (new fetch, navigation, React Strict Mode double-mount, etc.).
			c.Abort()
			return
		}
		h.logger.Error("trip notifications list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	unread, uerr := h.notif.CountUnread(c.Request.Context(), recipientKind, recipientID)
	if uerr != nil {
		if isClientDisconnect(uerr) {
			c.Abort()
			return
		}
		h.logger.Warn("trip notifications unread count", zap.Error(uerr))
		unread = 0
	}
	items := make([]gin.H, 0, len(list))
	for i := range list {
		items = append(items, tripNotificationRowToGin(c, list[i]))
	}
	resp.OKLang(c, "ok", gin.H{"items": items, "unread_count": unread})
}

// ListTripNotificationsDriver GET /v1/driver/trip-notifications
func (h *TripsHandler) ListTripNotificationsDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.listTripNotifications(c, tripnotif.RecipientDriver, driverID)
}

// ListTripNotificationsDispatcher GET /v1/dispatchers/trip-notifications
func (h *TripsHandler) ListTripNotificationsDispatcher(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.listTripNotifications(c, tripnotif.RecipientDispatcher, dispID)
}

func (h *TripsHandler) markTripNotificationRead(c *gin.Context, recipientKind string, recipientID uuid.UUID) {
	if h.notif == nil {
		resp.OKLang(c, "ok", gin.H{"read": false})
		return
	}
	nid, err := uuid.Parse(c.Param("id"))
	if err != nil || nid == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	ok, err := h.notif.MarkRead(c.Request.Context(), recipientKind, recipientID, nid)
	if err != nil {
		h.logger.Error("trip notification mark read", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusNotFound, "trip_notification_not_found")
		return
	}
	resp.OKLang(c, "ok", gin.H{"id": nid.String(), "read": true})
}

// MarkTripNotificationReadDriver POST /v1/driver/trip-notifications/:id/read
func (h *TripsHandler) MarkTripNotificationReadDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.markTripNotificationRead(c, tripnotif.RecipientDriver, driverID)
}

// MarkTripNotificationReadDispatcher POST /v1/dispatchers/trip-notifications/:id/read
func (h *TripsHandler) MarkTripNotificationReadDispatcher(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.markTripNotificationRead(c, tripnotif.RecipientDispatcher, dispID)
}

func (h *TripsHandler) markAllTripNotificationsRead(c *gin.Context, recipientKind string, recipientID uuid.UUID) {
	if h.notif == nil {
		resp.OKLang(c, "ok", gin.H{"updated": 0})
		return
	}
	n, err := h.notif.MarkAllRead(c.Request.Context(), recipientKind, recipientID)
	if err != nil {
		h.logger.Error("trip notifications mark all read", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	resp.OKLang(c, "ok", gin.H{"updated": n})
}

// MarkAllTripNotificationsReadDriver POST /v1/driver/trip-notifications/read-all
func (h *TripsHandler) MarkAllTripNotificationsReadDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.markAllTripNotificationsRead(c, tripnotif.RecipientDriver, driverID)
}

// MarkAllTripNotificationsReadDispatcher POST /v1/dispatchers/trip-notifications/read-all
func (h *TripsHandler) MarkAllTripNotificationsReadDispatcher(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.markAllTripNotificationsRead(c, tripnotif.RecipientDispatcher, dispID)
}

// --- Mobile-friendly aliases: /v1/driver/notifications and /v1/dispatchers/notifications ---

// ListNotificationsDriver GET /v1/driver/notifications — alias for driver mobile app.
func (h *TripsHandler) ListNotificationsDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.listTripNotifications(c, tripnotif.RecipientDriver, driverID)
}

// ListNotificationsDispatcher GET /v1/dispatchers/notifications
func (h *TripsHandler) ListNotificationsDispatcher(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.listTripNotifications(c, tripnotif.RecipientDispatcher, dispID)
}

// PatchNotificationReadDriver PATCH /v1/driver/notifications/:id — mark one notification as read.
func (h *TripsHandler) PatchNotificationReadDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.markTripNotificationRead(c, tripnotif.RecipientDriver, driverID)
}

// PatchNotificationReadDispatcher PATCH /v1/dispatchers/notifications/:id
func (h *TripsHandler) PatchNotificationReadDispatcher(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.markTripNotificationRead(c, tripnotif.RecipientDispatcher, dispID)
}

// ReadAllNotificationsDriver POST /v1/driver/notifications/read-all
func (h *TripsHandler) ReadAllNotificationsDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	h.markAllTripNotificationsRead(c, tripnotif.RecipientDriver, driverID)
}

// ReadAllNotificationsDispatcher POST /v1/dispatchers/notifications/read-all
func (h *TripsHandler) ReadAllNotificationsDispatcher(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	h.markAllTripNotificationsRead(c, tripnotif.RecipientDispatcher, dispID)
}

// profileTripRatingBody: trip_id — за какой завершённый рейс ставится оценка (аудит и проверка прав);
// оцениваемый человек задаётся путём (/dispatchers/{id} или /drivers/{id}) — это рейтинг профиля.
type profileTripRatingBody struct {
	TripID uuid.UUID `json:"trip_id" binding:"required"`
	Stars  float64   `json:"stars" binding:"required"`
}

type tripDispatcherParticipants struct {
	CargoManagerID  *uuid.UUID
	DriverManagerID *uuid.UUID
}

func (h *TripsHandler) getTripDispatcherParticipants(c *gin.Context, tripID uuid.UUID) (tripDispatcherParticipants, error) {
	out := tripDispatcherParticipants{}
	t, err := h.repo.GetByID(c.Request.Context(), tripID)
	if err != nil || t == nil {
		return out, err
	}
	cg, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
	out.CargoManagerID = tripNotifyDispatcherID(cg)
	if offer, _ := h.cargoRepo.GetOfferByID(c.Request.Context(), t.OfferID); offer != nil {
		if offer.NegotiationDispatcherID != nil && *offer.NegotiationDispatcherID != uuid.Nil {
			out.DriverManagerID = offer.NegotiationDispatcherID
			return out, nil
		}
		if strings.EqualFold(strings.TrimSpace(offer.ProposedBy), "DRIVER_MANAGER") && offer.ProposedByID != nil && *offer.ProposedByID != uuid.Nil {
			out.DriverManagerID = offer.ProposedByID
		}
	}
	return out, nil
}

// PostDriverRateDispatcher POST /v1/driver/dispatchers/:dispatcherId/rating — оценить cargo manager (1–5, шаг 0.5) после COMPLETED.
func (h *TripsHandler) PostDriverRateDispatcher(c *gin.Context) {
	if h.rating == nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	rateeDispID, err := uuid.Parse(c.Param("dispatcherId"))
	if err != nil || rateeDispID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var body profileTripRatingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	tripID := body.TripID
	if tripID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(c.Request.Context(), tripID)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	if t.DriverID == nil || *t.DriverID != driverID {
		resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
		return
	}
	ok, err := h.rating.TripDriverFinished(c.Request.Context(), tripID)
	if err != nil || !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_not_completed")
		return
	}
	participants, _ := h.getTripDispatcherParticipants(c, tripID)
	allowed := false
	if participants.CargoManagerID != nil && *participants.CargoManagerID == rateeDispID {
		allowed = true
	}
	if participants.DriverManagerID != nil && *participants.DriverManagerID == rateeDispID {
		allowed = true
	}
	if !allowed {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_ratee_mismatch")
		return
	}
	if err := h.rating.Upsert(c.Request.Context(), tripID, tripnotif.RecipientDriver, driverID, tripnotif.RecipientDispatcher, rateeDispID, body.Stars); err != nil {
		if err == triprating.ErrInvalidStars {
			resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_invalid_stars")
			return
		}
		h.logger.Error("trip rating upsert driver", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	resp.OKLang(c, "ok", gin.H{
		"trip_id":             tripID.String(),
		"stars":               body.Stars,
		"ratee_dispatcher_id": rateeDispID.String(),
	})
}

// PostDispatcherRateDriver POST /v1/dispatchers/drivers/:driverId/rating — оценить водителя (1–5, шаг 0.5) после COMPLETED.
func (h *TripsHandler) PostDispatcherRateDriver(c *gin.Context) {
	if h.rating == nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	rateeDriverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || rateeDriverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var body profileTripRatingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	tripID := body.TripID
	if tripID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(c.Request.Context(), tripID)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	cg, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
	isCargoManager := dispatcherOwnsCargo(cg, dispID, companyID)
	participants, _ := h.getTripDispatcherParticipants(c, tripID)
	isDriverManager := participants.DriverManagerID != nil && *participants.DriverManagerID == dispID
	if !isCargoManager && !isDriverManager {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	if t.DriverID == nil || *t.DriverID != rateeDriverID {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_ratee_mismatch")
		return
	}
	ok, err := h.rating.TripCompleted(c.Request.Context(), tripID)
	if err != nil || !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_not_completed")
		return
	}
	raterKind := tripnotif.RecipientDispatcher
	if isDriverManager {
		raterKind = "driver_manager"
	}
	if err := h.rating.Upsert(c.Request.Context(), tripID, raterKind, dispID, tripnotif.RecipientDriver, rateeDriverID, body.Stars); err != nil {
		if err == triprating.ErrInvalidStars {
			resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_invalid_stars")
			return
		}
		h.logger.Error("trip rating upsert dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	resp.OKLang(c, "ok", gin.H{
		"trip_id":         tripID.String(),
		"stars":           body.Stars,
		"ratee_driver_id": rateeDriverID.String(),
	})
}

// PostDispatcherRateDispatcher POST /v1/dispatchers/dispatchers/:dispatcherId/rating — оценить второго диспетчера (cargo manager <-> driver manager) после COMPLETED.
func (h *TripsHandler) PostDispatcherRateDispatcher(c *gin.Context) {
	if h.rating == nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	raterID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	rateeID, err := uuid.Parse(c.Param("dispatcherId"))
	if err != nil || rateeID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var body profileTripRatingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	tripID := body.TripID
	if tripID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(c.Request.Context(), tripID)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	ok, err := h.rating.TripCompleted(c.Request.Context(), tripID)
	if err != nil || !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_not_completed")
		return
	}
	cg, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
	isCargoManager := dispatcherOwnsCargo(cg, raterID, companyID)
	participants, _ := h.getTripDispatcherParticipants(c, tripID)
	isDriverManager := participants.DriverManagerID != nil && *participants.DriverManagerID == raterID
	if !isCargoManager && !isDriverManager {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	allowedRatee := false
	raterKind := tripnotif.RecipientDispatcher
	if isDriverManager {
		raterKind = "driver_manager"
		if participants.CargoManagerID != nil && *participants.CargoManagerID == rateeID {
			allowedRatee = true
		}
	}
	if isCargoManager && participants.DriverManagerID != nil && *participants.DriverManagerID == rateeID {
		allowedRatee = true
	}
	if !allowedRatee {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_ratee_mismatch")
		return
	}
	if err := h.rating.Upsert(c.Request.Context(), tripID, raterKind, raterID, tripnotif.RecipientDispatcher, rateeID, body.Stars); err != nil {
		if err == triprating.ErrInvalidStars {
			resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_invalid_stars")
			return
		}
		h.logger.Error("trip rating upsert dispatcher->dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	resp.OKLang(c, "ok", gin.H{
		"trip_id":             tripID.String(),
		"stars":               body.Stars,
		"ratee_dispatcher_id": rateeID.String(),
	})
}
