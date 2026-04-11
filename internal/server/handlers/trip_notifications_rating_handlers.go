package handlers

import (
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

func tripNotificationRowToGin(c *gin.Context, n tripnotif.Row) gin.H {
	step := n.EventKind
	if n.ToStatus != nil && strings.TrimSpace(*n.ToStatus) != "" {
		step = strings.TrimSpace(*n.ToStatus)
	}
	return gin.H{
		"id":         n.ID.String(),
		"trip_id":    n.TripID.String(),
		"event_kind": n.EventKind,
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
		"message":    resp.Msg(tripNotifMessageKey(n.EventKind), resp.Lang(c)),
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
		h.logger.Error("trip notifications list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	unread, _ := h.notif.CountUnread(c.Request.Context(), recipientKind, recipientID)
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

// profileTripRatingBody: trip_id — за какой завершённый рейс ставится оценка (аудит и проверка прав);
// оцениваемый человек задаётся путём (/dispatchers/{id} или /drivers/{id}) — это рейтинг профиля.
type profileTripRatingBody struct {
	TripID uuid.UUID `json:"trip_id" binding:"required"`
	Stars  float64   `json:"stars" binding:"required"`
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
	ok, err := h.rating.TripCompleted(c.Request.Context(), tripID)
	if err != nil || !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_not_completed")
		return
	}
	cg, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
	managerID := tripNotifyDispatcherID(cg)
	if managerID == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_cargo_manager_unknown")
		return
	}
	if *managerID != rateeDispID {
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
		"trip_id":               tripID.String(),
		"stars":                 body.Stars,
		"ratee_dispatcher_id":   rateeDispID.String(),
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
	if !dispatcherOwnsCargo(cg, dispID, companyID) {
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
	if err := h.rating.Upsert(c.Request.Context(), tripID, tripnotif.RecipientDispatcher, dispID, tripnotif.RecipientDriver, rateeDriverID, body.Stars); err != nil {
		if err == triprating.ErrInvalidStars {
			resp.ErrorLang(c, http.StatusBadRequest, "trip_rating_invalid_stars")
			return
		}
		h.logger.Error("trip rating upsert dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	resp.OKLang(c, "ok", gin.H{
		"trip_id":            tripID.String(),
		"stars":              body.Stars,
		"ratee_driver_id":    rateeDriverID.String(),
	})
}
