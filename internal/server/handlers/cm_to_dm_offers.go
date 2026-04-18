package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

type CMToDMOffersHandler struct {
	logger *zap.Logger
	cargo  *cargo.Repo
	disp   *dispatchers.Repo
	drv    *drivers.Repo
	stream *userstream.Hub
}

func NewCMToDMOffersHandler(logger *zap.Logger, cargoRepo *cargo.Repo, dispRepo *dispatchers.Repo, drvRepo *drivers.Repo, stream *userstream.Hub) *CMToDMOffersHandler {
	return &CMToDMOffersHandler{logger: logger, cargo: cargoRepo, disp: dispRepo, drv: drvRepo, stream: stream}
}

type sendToDMReq struct {
	DriverManagerID uuid.UUID `json:"driver_manager_id" binding:"required"`
	Price           float64   `json:"price" binding:"required"`
	Currency        string    `json:"currency" binding:"required"`
	Comment         string    `json:"comment"`
}

// SendToDriverManager POST /v1/dispatchers/cargo/:id/offers/send-to-driver-manager
func (h *CMToDMOffersHandler) SendToDriverManager(c *gin.Context) {
	cargoManagerID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var req sendToDMReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if req.DriverManagerID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	dm, err := h.disp.FindByID(c.Request.Context(), req.DriverManagerID)
	if err != nil || dm == nil {
		resp.ErrorLang(c, http.StatusNotFound, "dispatcher_not_found")
		return
	}
	if dm.ManagerRole == nil || strings.TrimSpace(*dm.ManagerRole) != dispatchers.ManagerRoleDriverManager {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_manager_role")
		return
	}

	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	cargoObj, _ := h.cargo.GetByID(c.Request.Context(), cargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if !dispatcherOwnsCargoForNegotiation(cargoObj, cargoManagerID, companyID) {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	// Ensure sender is Cargo Manager role.
	cm, err := h.disp.FindByID(c.Request.Context(), cargoManagerID)
	if err != nil || cm == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return
	}
	if cm.ManagerRole == nil || strings.TrimSpace(*cm.ManagerRole) != dispatchers.ManagerRoleCargoManager {
		resp.ErrorLang(c, http.StatusForbidden, "invalid_manager_role")
		return
	}

	reqID, err := h.cargo.CreateCargoManagerDMOffer(c.Request.Context(), cargoID, cargoManagerID, req.DriverManagerID, req.Price, req.Currency, req.Comment)
	if err != nil {
		h.logger.Error("cm->dm offer create", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_offer")
		return
	}

	if h.stream != nil {
		p := gin.H{
			"kind":       "cargo_offer",
			"event":      "cargo_offer_created",
			"direction":  "incoming",
			"request_id": reqID.String(),
			"cargo_id":   cargoID.String(),
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		}
		ensureSSEID(p)
		h.stream.PublishNotification(tripnotif.RecipientDispatcher, req.DriverManagerID, p)
	}

	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{
		"request_id": reqID.String(),
		"status":     "PENDING",
	})
}

type dmAcceptReq struct {
	DriverID uuid.UUID `json:"driver_id" binding:"required"`
}

// AcceptFromCargoManager POST /v1/dispatchers/driver-manager-offers/:id/accept
// DM selects driver; system creates a normal offer in WAITING_DRIVER_CONFIRM and pushes to driver.
func (h *CMToDMOffersHandler) AcceptFromCargoManager(c *gin.Context) {
	driverManagerID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	// ensure DM role
	dm, err := h.disp.FindByID(c.Request.Context(), driverManagerID)
	if err != nil || dm == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return
	}
	if dm.ManagerRole == nil || strings.TrimSpace(*dm.ManagerRole) != dispatchers.ManagerRoleDriverManager {
		resp.ErrorLang(c, http.StatusForbidden, "invalid_manager_role")
		return
	}

	reqID, err := uuid.Parse(c.Param("id"))
	if err != nil || reqID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var body dmAcceptReq
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if body.DriverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	// driver must be linked to this DM
	linked, _ := h.drv.IsLinked(c.Request.Context(), body.DriverID, driverManagerID)
	if !linked {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_driver_or_cargo")
		return
	}

	reqRow, err := h.cargo.GetCargoManagerDMOfferByID(c.Request.Context(), reqID)
	if err != nil || reqRow == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found")
		return
	}
	if reqRow.DriverManagerID != driverManagerID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
		return
	}
	if strings.ToUpper(strings.TrimSpace(reqRow.Status)) != "PENDING" {
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_found_or_not_pending")
		return
	}

	offerID, err := h.cargo.CreateOffer(
		c.Request.Context(),
		reqRow.CargoID,
		body.DriverID,
		reqRow.Price,
		reqRow.Currency,
		func() string {
			if reqRow.Comment == nil {
				return ""
			}
			return *reqRow.Comment
		}(),
		cargo.OfferProposedByDispatcher,
		&reqRow.CargoManagerID,
	)
	if err != nil {
		h.logger.Error("cm->dm accept create offer", zap.Error(err))
		switch {
		case errors.Is(err, cargo.ErrDriverOfferAlreadyExists):
			resp.ErrorLang(c, http.StatusConflict, "driver_offer_already_exists")
		case errors.Is(err, cargo.ErrDispatcherOfferAlreadyExists):
			resp.ErrorLang(c, http.StatusConflict, "dispatcher_offer_already_exists")
		case errors.Is(err, cargo.ErrCargoSlotsFull):
			resp.ErrorLang(c, http.StatusConflict, "cargo_slots_full")
		case errors.Is(err, cargo.ErrCargoNotSearching):
			resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
		default:
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_offer")
		}
		return
	}
	if err := h.cargo.SetOfferStatusWaitingDriver(c.Request.Context(), offerID, &driverManagerID); err != nil {
		if errors.Is(err, cargo.ErrOfferNotFoundOrNotPending) {
			resp.ErrorLang(c, http.StatusNotFound, "offer_not_found_or_not_pending")
			return
		}
		h.logger.Error("cm->dm accept set waiting driver", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	_ = h.cargo.AcceptCargoManagerDMOffer(c.Request.Context(), reqID, body.DriverID, offerID)

	if h.stream != nil {
		pubOffer, err := h.cargo.GetOfferByID(c.Request.Context(), offerID)
		if err != nil || pubOffer == nil {
			cm := reqRow.CargoManagerID
			dm := driverManagerID
			pubOffer = &cargo.Offer{
				ID:                      offerID,
				CargoID:                 reqRow.CargoID,
				CarrierID:               body.DriverID,
				ProposedBy:              cargo.OfferProposedByDispatcher,
				ProposedByID:            &cm,
				NegotiationDispatcherID: &dm,
			}
		}
		cg, _ := h.cargo.GetByID(c.Request.Context(), reqRow.CargoID, false)
		p := gin.H{
			"kind":       "cargo_offer",
			"event":      "cargo_offer_waiting_driver",
			"offer_id":   offerID.String(),
			"cargo_id":   reqRow.CargoID.String(),
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		}
		ensureSSEID(p)
		h.stream.PublishNotification(tripnotif.RecipientDriver, body.DriverID, p)
		publishCargoOfferToDispatchers(h.stream, cg, pubOffer, p)
	}

	resp.OKLang(c, "waiting_driver_confirmation", gin.H{
		"status":   "waiting_driver_confirm",
		"offer_id": offerID.String(),
	})
}

