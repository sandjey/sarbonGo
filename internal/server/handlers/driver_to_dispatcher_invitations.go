package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/drivertodispatcherinvitations"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

// DriverToDispatcherInvitationsHandler handles invitations FROM driver TO dispatcher (by phone). Driver sends, dispatcher accepts/declines.
type DriverToDispatcherInvitationsHandler struct {
	logger *zap.Logger
	repo   *drivertodispatcherinvitations.Repo
	drv    *drivers.Repo
	disp   *dispatchers.Repo
	stream *userstream.Hub
}

// NewDriverToDispatcherInvitationsHandler creates the handler.
func NewDriverToDispatcherInvitationsHandler(logger *zap.Logger, repo *drivertodispatcherinvitations.Repo, drv *drivers.Repo, disp *dispatchers.Repo, stream *userstream.Hub) *DriverToDispatcherInvitationsHandler {
	return &DriverToDispatcherInvitationsHandler{logger: logger, repo: repo, drv: drv, disp: disp, stream: stream}
}

// CreateDriverToDispatcherReq body for POST /v1/driver/dispatcher-invitations
type CreateDriverToDispatcherReq struct {
	Phone string `json:"phone" binding:"required"`
}

// CreateFromDriver creates invitation from current driver to dispatcher (by phone). GET /v1/driver/dispatcher-invitations (list) and POST (create).
func (h *DriverToDispatcherInvitationsHandler) CreateFromDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req CreateDriverToDispatcherReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "phone_required")
		return
	}
	if dup, err := h.repo.HasPendingForDriverPhone(c.Request.Context(), driverID, phone); err != nil {
		h.logger.Error("driver to dispatcher duplicate check", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	} else if dup {
		resp.ErrorLang(c, http.StatusConflict, "driver_invitation_already_pending")
		return
	}
	// Optional: check dispatcher exists by phone (so we don't invite non-existent)
	disp, _ := h.disp.FindByPhone(c.Request.Context(), phone)
	if disp == nil {
		// still allow sending (dispatcher might register later)
	}
	// If driver already linked to this dispatcher, no need to invite
	if disp != nil {
		drv, _ := h.drv.FindByID(c.Request.Context(), driverID)
		if drv != nil && drv.FreelancerID != nil && *drv.FreelancerID == disp.ID {
			resp.ErrorLang(c, http.StatusConflict, "already_linked_to_this_dispatcher")
			return
		}
	}
	token, err := h.repo.Create(c.Request.Context(), driverID, phone, 7*24*time.Hour)
	if err != nil {
		h.logger.Error("driver to dispatcher invitation create", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if h.stream != nil && disp != nil {
		if dispID, err := uuid.Parse(disp.ID); err == nil && dispID != uuid.Nil {
			h.stream.PublishNotification(tripnotif.RecipientDispatcher, dispID, gin.H{
				"kind":       "connection_offer",
				"event":      "connection_offer_created",
				"direction":  "incoming",
				"token":      token,
				"driver_id":  driverID.String(),
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
	}
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"token": token, "expires_in_hours": 168})
}

// ListSentByDriver returns invitations sent by the current driver (to dispatchers).
func (h *DriverToDispatcherInvitationsHandler) ListSentByDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	list, err := h.repo.ListByDriverID(c.Request.Context(), driverID)
	if err != nil {
		h.logger.Error("driver to dispatcher invitations list sent", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
		return
	}
	if list == nil {
		list = []drivertodispatcherinvitations.Invitation{}
	}
	items := make([]gin.H, 0, len(list))
	for _, inv := range list {
		st := drivertodispatcherinvitations.EffectiveStatus(inv)
		item := gin.H{
			"token":               inv.Token,
			"dispatcher_phone":    inv.DispatcherPhone,
			"to_dispatcher_phone": inv.DispatcherPhone,
			"driver_id":           inv.DriverID.String(),
			"expires_at":          inv.ExpiresAt,
			"created_at":          inv.CreatedAt,
			"status":              st,
		}
		if inv.RespondedAt != nil {
			item["responded_at"] = inv.RespondedAt
		}
		items = append(items, item)
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

// CancelByDriver cancels an invitation sent by the current driver.
func (h *DriverToDispatcherInvitationsHandler) CancelByDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "token_required")
		return
	}
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_required")
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 3 {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_too_short")
		return
	}
	inv, err := h.repo.GetPendingByToken(c.Request.Context(), token)
	if err != nil || inv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "invitation_not_found_or_expired")
		return
	}
	if inv.DriverID != driverID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_invitation")
		return
	}
	ok, err := h.repo.DeletePendingByToken(c.Request.Context(), token)
	if err != nil {
		h.logger.Error("driver to dispatcher invitation cancel", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_cancel_invitation")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusNotFound, "invitation_not_found_or_expired")
		return
	}
	if h.stream != nil {
		if disp, _ := h.disp.FindByPhone(c.Request.Context(), inv.DispatcherPhone); disp != nil {
			if dispID, err := uuid.Parse(disp.ID); err == nil && dispID != uuid.Nil {
				h.stream.PublishNotification(tripnotif.RecipientDispatcher, dispID, gin.H{
					"kind":       "connection_offer",
					"event":      "connection_offer_cancelled",
					"direction":  "incoming",
					"token":      token,
					"driver_id":  driverID.String(),
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				})
			}
		}
	}
	resp.OKLang(c, "ok", nil)
}

// ListReceivedByDispatcher returns invitations sent TO the current dispatcher (by their phone).
func (h *DriverToDispatcherInvitationsHandler) ListReceivedByDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	disp, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
	if err != nil || disp == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return
	}
	list, err := h.repo.ListByDispatcherPhone(c.Request.Context(), disp.Phone)
	if err != nil {
		h.logger.Error("driver to dispatcher invitations list received", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
		return
	}
	if list == nil {
		list = []drivertodispatcherinvitations.Invitation{}
	}
	items := make([]gin.H, 0, len(list))
	for _, inv := range list {
		st := drivertodispatcherinvitations.EffectiveStatus(inv)
		item := gin.H{
			"token":            inv.Token,
			"driver_id":        inv.DriverID.String(),
			"dispatcher_phone": inv.DispatcherPhone,
			"from_driver_id":   inv.DriverID.String(),
			"expires_at":       inv.ExpiresAt,
			"created_at":       inv.CreatedAt,
			"status":           st,
		}
		if inv.RespondedAt != nil {
			item["responded_at"] = inv.RespondedAt
		}
		drv, _ := h.drv.FindByID(c.Request.Context(), inv.DriverID)
		if drv != nil {
			item["driver_name"] = drv.Name
			item["driver_phone"] = drv.Phone
		}
		items = append(items, item)
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

// AcceptByDispatcherReq body for POST /v1/dispatchers/invitations-from-drivers/accept
type AcceptByDispatcherReq struct {
	Token string `json:"token" binding:"required"`
}

// AcceptByDispatcher dispatcher accepts driver's invitation; driver.freelancer_id = dispatcher.
func (h *DriverToDispatcherInvitationsHandler) AcceptByDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	disp, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
	if err != nil || disp == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return
	}
	var req AcceptByDispatcherReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	token := strings.TrimSpace(req.Token)
	inv, err := h.repo.GetPendingByToken(c.Request.Context(), token)
	if err != nil || inv == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	if !inv.PhoneMatches(disp.Phone) {
		resp.ErrorLang(c, http.StatusForbidden, "invitation_sent_to_another_phone")
		return
	}
	ok, err := h.repo.SetStatusIfPending(c.Request.Context(), token, drivertodispatcherinvitations.StatusAccepted)
	if err != nil || !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	if err := h.drv.SetFreelancerID(c.Request.Context(), inv.DriverID, dispatcherID); err != nil {
		_ = h.repo.RevertToPending(c.Request.Context(), token)
		h.logger.Error("dispatcher accept driver invitation", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_accept")
		return
	}
	if h.stream != nil {
		h.stream.PublishNotification(tripnotif.RecipientDriver, inv.DriverID, gin.H{
			"kind":          "connection_offer",
			"event":         "connection_offer_accepted",
			"direction":     "outgoing",
			"token":         token,
			"dispatcher_id": dispatcherID.String(),
			"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	resp.SuccessLang(c, http.StatusOK, "accepted", gin.H{"driver_id": inv.DriverID.String()})
}

// DeclineByDispatcherReq body for POST /v1/dispatchers/invitations-from-drivers/decline
type DeclineByDispatcherReq struct {
	Token string `json:"token" binding:"required"`
}

// DeclineByDispatcher dispatcher declines driver's invitation; invitation is deleted.
func (h *DriverToDispatcherInvitationsHandler) DeclineByDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	disp, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
	if err != nil || disp == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return
	}
	var req DeclineByDispatcherReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	token := strings.TrimSpace(req.Token)
	inv, err := h.repo.GetPendingByToken(c.Request.Context(), token)
	if err != nil || inv == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	if !inv.PhoneMatches(disp.Phone) {
		resp.ErrorLang(c, http.StatusForbidden, "invitation_sent_to_another_phone")
		return
	}
	ok, err := h.repo.SetStatusIfPending(c.Request.Context(), token, drivertodispatcherinvitations.StatusDeclined)
	if err != nil {
		h.logger.Error("dispatcher decline driver invitation", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	if h.stream != nil {
		h.stream.PublishNotification(tripnotif.RecipientDriver, inv.DriverID, gin.H{
			"kind":          "connection_offer",
			"event":         "connection_offer_declined",
			"direction":     "outgoing",
			"token":         token,
			"dispatcher_id": dispatcherID.String(),
			"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	resp.OKLang(c, "declined", gin.H{"status": "declined"})
}
