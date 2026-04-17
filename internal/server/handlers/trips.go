package handlers

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
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
	"sarbonNew/internal/triprating"
	"sarbonNew/internal/trips"
	"sarbonNew/internal/userstream"
)

type TripsHandler struct {
	logger      *zap.Logger
	repo        *trips.Repo
	cargoRepo   *cargo.Repo
	drivers     *drivers.Repo
	dispatchers *dispatchers.Repo
	notif       *tripnotif.Repo
	rating      *triprating.Repo
	stream      *userstream.Hub
}

func NewTripsHandler(logger *zap.Logger, repo *trips.Repo, cargoRepo *cargo.Repo, driversRepo *drivers.Repo, dispatchersRepo *dispatchers.Repo, notif *tripnotif.Repo, rating *triprating.Repo, stream *userstream.Hub) *TripsHandler {
	return &TripsHandler{logger: logger, repo: repo, cargoRepo: cargoRepo, drivers: driversRepo, dispatchers: dispatchersRepo, notif: notif, rating: rating, stream: stream}
}

func dispatcherOwnsCargo(c *cargo.Cargo, dispatcherID uuid.UUID, companyID *uuid.UUID) bool {
	if c == nil {
		return false
	}
	if c.CreatedByType != nil && strings.EqualFold(*c.CreatedByType, "DISPATCHER") && c.CreatedByID != nil && *c.CreatedByID == dispatcherID {
		return true
	}
	if c.CreatedByType != nil && strings.EqualFold(*c.CreatedByType, "COMPANY") && c.CompanyID != nil && companyID != nil && *c.CompanyID == *companyID {
		return true
	}
	return false
}

func (h *TripsHandler) dispatcherCanAccessTrip(ctx *gin.Context, t *trips.Trip, cargoObj *cargo.Cargo, dispatcherID uuid.UUID, companyID *uuid.UUID) bool {
	if dispatcherOwnsCargo(cargoObj, dispatcherID, companyID) {
		return true
	}
	if h.cargoRepo == nil || t == nil {
		return false
	}
	offer, err := h.cargoRepo.GetOfferByID(ctx.Request.Context(), t.OfferID)
	if err != nil || offer == nil {
		return false
	}
	pb := strings.ToUpper(strings.TrimSpace(offer.ProposedBy))
	if pb == cargo.OfferProposedByDriverManager && offer.ProposedByID != nil && *offer.ProposedByID == dispatcherID {
		return true
	}
	if offer.NegotiationDispatcherID != nil && *offer.NegotiationDispatcherID == dispatcherID {
		return true
	}
	return false
}

// Get returns trip by id.
func (h *TripsHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	resp.OKLang(c, "ok", toTripResp(t))
}

// List for GET /api/trips: ?cargo_id= returns single trip for that cargo.
func (h *TripsHandler) List(c *gin.Context) {
	cargoIDStr := c.Query("cargo_id")
	if cargoIDStr == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "require_cargo_id")
		return
	}
	cargoID, err := uuid.Parse(cargoIDStr)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_cargo_id")
		return
	}
	list, err := h.repo.ListByCargoID(c.Request.Context(), cargoID)
	if err != nil {
		h.logger.Error("trips list by cargo", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	out := make([]interface{}, 0, len(list))
	for i := range list {
		item := toTripResp(&list[i])
		cargoObj, _ := h.cargoRepo.GetByID(c.Request.Context(), list[i].CargoID, false)
		if cargoObj != nil {
			item["cargo"] = tripCargoMini(cargoObj)
		}
		if list[i].DriverID != nil && h.drivers != nil {
			if drv, _ := h.drivers.FindByID(c.Request.Context(), *list[i].DriverID); drv != nil {
				item["driver"] = gin.H{
					"id":    drv.ID,
					"phone": drv.Phone,
					"name":  drv.Name,
				}
			}
		}
		out = append(out, item)
	}
	resp.OKLang(c, "ok", gin.H{"items": out})
}

func tripCargoMini(cg *cargo.Cargo) gin.H {
	return gin.H{
		"id":              cg.ID.String(),
		"cargo_type_name": firstNonEmptyStr(cg.CargoTypeNameRU, cg.CargoTypeNameUZ, cg.CargoTypeNameEN, cg.CargoTypeNameTR, cg.CargoTypeNameZH),
		"weight":          cg.Weight,
		"volume":          cg.Volume,
	}
}

// GetMyCurrentTrip GET /v1/driver/trips/me — один текущий исполняемый рейс (IN_PROGRESS / IN_TRANSIT / DELIVERED), иначе trip: null.
func (h *TripsHandler) GetMyCurrentTrip(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	t, err := h.repo.GetCurrentActiveTripForDriver(c.Request.Context(), driverID)
	if err != nil {
		h.logger.Error("get current trip for driver", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	if t == nil {
		resp.OKLang(c, "ok", gin.H{"trip": nil})
		return
	}
	resp.OKLang(c, "ok", gin.H{"trip": toTripResp(t)})
}

// ListForCargoManager GET /v1/dispatchers/trips — list trips for dispatcher-owned cargo (or switched company cargo).
func (h *TripsHandler) ListForCargoManager(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}

	var cargoID *uuid.UUID
	if s := strings.TrimSpace(c.Query("cargo_id")); s != "" {
		if u, err := uuid.Parse(s); err == nil && u != uuid.Nil {
			cargoID = &u
		} else {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_cargo_id")
			return
		}
	}
	var driverID *uuid.UUID
	if s := strings.TrimSpace(c.Query("driver_id")); s != "" {
		if u, err := uuid.Parse(s); err == nil && u != uuid.Nil {
			driverID = &u
		} else {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
			return
		}
	}
	var statuses []string
	if s := strings.TrimSpace(c.Query("status")); s != "" {
		parts := strings.Split(s, ",")
		for _, p := range parts {
			v := strings.ToUpper(strings.TrimSpace(p))
			if v != "" {
				statuses = append(statuses, v)
			}
		}
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	sortField := strings.TrimSpace(c.DefaultQuery("sort", "created_at"))
	sortOrder := strings.TrimSpace(c.DefaultQuery("order", "desc"))

	list, total, err := h.repo.ListForCargoManager(c.Request.Context(), dispID, companyID, cargoID, driverID, statuses, limit, offset, sortField, sortOrder)
	if err != nil {
		h.logger.Error("trips list for cargo manager", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	out := make([]interface{}, 0, len(list))
	for i := range list {
		out = append(out, toTripResp(&list[i]))
	}
	resp.OKLang(c, "ok", gin.H{
		"items":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"sort":   sortField,
		"order":  sortOrder,
	})
}

// GetForCargoManager returns a single trip by id for dispatcher-owned cargo (or switched company cargo).
func (h *TripsHandler) GetForCargoManager(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	cargoObj, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
	if !h.dispatcherCanAccessTrip(c, t, cargoObj, dispID, companyID) {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	resp.OKLang(c, "ok", toTripResp(t))
}

// ListByCargoForCargoManager GET /v1/dispatchers/cargo/:id/trips
func (h *TripsHandler) ListByCargoForCargoManager(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_cargo_id")
		return
	}
	cargoObj, _ := h.cargoRepo.GetByID(c.Request.Context(), cargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if !dispatcherOwnsCargo(cargoObj, dispID, companyID) {
		allowed := false
		offers, _ := h.cargoRepo.GetOffers(c.Request.Context(), cargoID)
		for i := range offers {
			o := offers[i]
			if o.NegotiationDispatcherID != nil && *o.NegotiationDispatcherID == dispID {
				allowed = true
				break
			}
			if strings.EqualFold(strings.TrimSpace(o.ProposedBy), cargo.OfferProposedByDriverManager) &&
				o.ProposedByID != nil && *o.ProposedByID == dispID {
				allowed = true
				break
			}
		}
		if !allowed {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}
	}
	list, err := h.repo.ListByCargoID(c.Request.Context(), cargoID)
	if err != nil {
		h.logger.Error("list trips by cargo for cargo manager", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	out := make([]interface{}, 0, len(list))
	for i := range list {
		out = append(out, toTripResp(&list[i]))
	}
	resp.OKLang(c, "ok", gin.H{
		"cargo_id": cargoID.String(),
		"items":    out,
		"total":    len(out),
	})
}

// ListMyHistory GET /v1/driver/trips/history
func (h *TripsHandler) ListMyHistory(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	activeAndDone, err := h.repo.ListByDriver(c.Request.Context(), driverID, limit)
	if err != nil {
		h.logger.Error("driver trip history current", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	archived, err := h.repo.ListArchivedByDriver(c.Request.Context(), driverID, limit)
	if err != nil {
		h.logger.Error("driver trip history archived", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	openOfferByID := make(map[uuid.UUID]struct{})
	for i := range activeAndDone {
		t := &activeAndDone[i]
		if t.Status != trips.StatusCompleted {
			openOfferByID[t.OfferID] = struct{}{}
		}
	}
	items := make([]gin.H, 0, len(activeAndDone)+len(archived)+limit*4)
	for i := range activeAndDone {
		t := &activeAndDone[i]
		if t.Status == trips.StatusCompleted {
			items = append(items, gin.H{
				"event_type": "trip_completed",
				"event_at":   t.UpdatedAt,
				"trip":       toTripResp(t),
			})
			continue
		}
		items = append(items, gin.H{
			"event_type": "trip_active",
			"event_at":   t.UpdatedAt,
			"trip":       toTripResp(t),
		})
	}
	for i := range archived {
		t := &archived[i]
		items = append(items, gin.H{
			"event_type":        "trip_cancelled",
			"event_at":          t.ArchivedAt,
			"cancelled_by_role": t.CancelledByRole,
			"trip":              toTripResp(&t.Trip),
		})
	}
	accepted, _ := h.cargoRepo.ListDriverOffersAll(c.Request.Context(), driverID, "ACCEPTED", "all", nil, limit, 0)
	for i := range accepted {
		o := accepted[i]
		_, hasOpen := openOfferByID[o.ID]
		items = append(items, gin.H{
			"event_type":    "offer_accepted",
			"event_at":      o.CreatedAt,
			"has_open_trip": hasOpen,
			"offer": gin.H{
				"id":               o.ID.String(),
				"cargo_id":         o.CargoID.String(),
				"carrier_id":       o.CarrierID.String(),
				"status":           o.Status,
				"proposed_by":      o.ProposedBy,
				"price":            o.Price,
				"invitation_price": o.Price,
				"currency":         o.Currency,
				"comment":          o.Comment,
				"rejection_reason": o.RejectionReason,
				"created_at":       o.CreatedAt,
			},
		})
	}
	rejected, _ := h.cargoRepo.ListDriverOffersAll(c.Request.Context(), driverID, "REJECTED", "all", nil, limit, 0)
	for i := range rejected {
		o := rejected[i]
		items = append(items, gin.H{
			"event_type": "offer_rejected",
			"event_at":   o.CreatedAt,
			"offer": gin.H{
				"id":               o.ID.String(),
				"cargo_id":         o.CargoID.String(),
				"carrier_id":       o.CarrierID.String(),
				"status":           o.Status,
				"proposed_by":      o.ProposedBy,
				"price":            o.Price,
				"invitation_price": o.Price,
				"currency":         o.Currency,
				"comment":          o.Comment,
				"rejection_reason": o.RejectionReason,
				"created_at":       o.CreatedAt,
			},
		})
	}
	sortHistoryByEventAtDesc(items)
	resp.OKLang(c, "ok", gin.H{"items": items, "total": len(items), "limit": limit})
}

// ListHistoryForCargoManager GET /v1/dispatchers/trips/history
func (h *TripsHandler) ListHistoryForCargoManager(c *gin.Context) {
	dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	current, _, err := h.repo.ListForCargoManager(c.Request.Context(), dispID, companyID, nil, nil, nil, limit, 0, "updated_at", "desc")
	if err != nil {
		h.logger.Error("cargo manager trip history current", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	archived, err := h.repo.ListArchivedForCargoManager(c.Request.Context(), dispID, companyID, limit)
	if err != nil {
		h.logger.Error("cargo manager trip history archived", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	openOfferByID := make(map[uuid.UUID]struct{})
	for i := range current {
		t := &current[i]
		if t.Status != trips.StatusCompleted {
			openOfferByID[t.OfferID] = struct{}{}
		}
	}
	items := make([]gin.H, 0, len(current)+len(archived)+limit*4)
	for i := range current {
		t := &current[i]
		if t.Status == trips.StatusCompleted {
			items = append(items, gin.H{
				"event_type": "trip_completed",
				"event_at":   t.UpdatedAt,
				"trip":       toTripResp(t),
			})
			continue
		}
		items = append(items, gin.H{
			"event_type": "trip_active",
			"event_at":   t.UpdatedAt,
			"trip":       toTripResp(t),
		})
	}
	for i := range archived {
		t := &archived[i]
		items = append(items, gin.H{
			"event_type":        "trip_cancelled",
			"event_at":          t.ArchivedAt,
			"cancelled_by_role": t.CancelledByRole,
			"trip":              toTripResp(&t.Trip),
		})
	}
	accepted, _ := h.cargoRepo.ListDispatcherSentOffers(c.Request.Context(), dispID, companyID, "ACCEPTED", "all", nil, limit, 0)
	for i := range accepted {
		o := accepted[i]
		_, hasOpen := openOfferByID[o.ID]
		items = append(items, gin.H{
			"event_type":    "offer_accepted",
			"event_at":      o.CreatedAt,
			"has_open_trip": hasOpen,
			"offer": gin.H{
				"id":               o.ID.String(),
				"cargo_id":         o.CargoID.String(),
				"carrier_id":       o.CarrierID.String(),
				"status":           o.Status,
				"proposed_by":      o.ProposedBy,
				"price":            o.Price,
				"invitation_price": o.Price,
				"currency":         o.Currency,
				"comment":          o.Comment,
				"rejection_reason": o.RejectionReason,
				"created_at":       o.CreatedAt,
			},
		})
	}
	rejected, _ := h.cargoRepo.ListDispatcherSentOffers(c.Request.Context(), dispID, companyID, "REJECTED", "all", nil, limit, 0)
	for i := range rejected {
		o := rejected[i]
		items = append(items, gin.H{
			"event_type": "offer_rejected",
			"event_at":   o.CreatedAt,
			"offer": gin.H{
				"id":               o.ID.String(),
				"cargo_id":         o.CargoID.String(),
				"carrier_id":       o.CarrierID.String(),
				"status":           o.Status,
				"proposed_by":      o.ProposedBy,
				"price":            o.Price,
				"invitation_price": o.Price,
				"currency":         o.Currency,
				"comment":          o.Comment,
				"rejection_reason": o.RejectionReason,
				"created_at":       o.CreatedAt,
			},
		})
	}
	sortHistoryByEventAtDesc(items)
	resp.OKLang(c, "ok", gin.H{"items": items, "total": len(items), "limit": limit})
}

// AssignDriverReq body for PATCH /v1/dispatchers/trips/:id/assign-driver (dispatcher).
type AssignDriverReq struct {
	DriverID string `json:"driver_id" binding:"required,uuid"`
}

// AssignDriver sets driver on trip (dispatcher). Trip must be pending_driver.
func (h *TripsHandler) AssignDriver(c *gin.Context) {
	tripID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(c.Request.Context(), tripID)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found_or_not_pending_driver")
		return
	}
	if h.cargoRepo != nil {
		cargoObj, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
		if cargoObj != nil && cargoObj.CreatedByType != nil && *cargoObj.CreatedByType == "DISPATCHER" {
			resp.ErrorLang(c, http.StatusBadRequest, "freelance_cargo_assign_via_offer_or_recommendation")
			return
		}
	}
	var req AssignDriverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	driverID, _ := uuid.Parse(req.DriverID)
	if driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	if err := h.repo.AssignDriver(c.Request.Context(), tripID, driverID); err != nil {
		if err == trips.ErrNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "trip_not_found_or_not_pending_driver")
			return
		}
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": trips.StatusInProgress, "driver_id": driverID.String()})
}

func (h *TripsHandler) runConfirmTransition(c *gin.Context, asDispatcher bool) {
	ctx := c.Request.Context()
	tripID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	tBefore, err := h.repo.GetByID(ctx, tripID)
	if err != nil || tBefore == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	var driverID uuid.UUID
	if !asDispatcher {
		driverID = c.MustGet(mw.CtxDriverID).(uuid.UUID)
		if tBefore.DriverID == nil || *tBefore.DriverID != driverID {
			resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
			return
		}
	}
	if asDispatcher {
		dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
		var companyID *uuid.UUID
		if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
			if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
				companyID = &u
			}
		}
		cargoObj, _ := h.cargoRepo.GetByID(ctx, tBefore.CargoID, false)
		if !h.dispatcherCanAccessTrip(c, tBefore, cargoObj, dispID, companyID) {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}
		// CARGO_MANAGER may only confirm closing the trip (COMPLETED), after the driver requested it.
		if h.dispatchers != nil {
			dRec, err := h.dispatchers.FindByID(ctx, dispID)
			if err != nil {
				if errors.Is(err, dispatchers.ErrNotFound) {
					resp.ErrorLang(c, http.StatusForbidden, "forbidden")
					return
				}
				h.logger.Error("dispatcher profile for trip confirm", zap.Error(err))
				resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
				return
			}
			role := ""
			if dRec != nil && dRec.ManagerRole != nil {
				role = strings.TrimSpace(*dRec.ManagerRole)
			}
			// Driver Manager must NOT close trip to COMPLETED. Only Cargo Manager can close.
			if strings.EqualFold(role, dispatchers.ManagerRoleDriverManager) && trips.CompletionPending(tBefore) {
				resp.ErrorLang(c, http.StatusForbidden, "trip_cargo_manager_completed_only")
				return
			}
			if strings.EqualFold(role, dispatchers.ManagerRoleCargoManager) && !trips.CompletionPending(tBefore) {
				resp.ErrorLang(c, http.StatusForbidden, "trip_cargo_manager_completed_only")
				return
			}
		}
	}

	tx, err := h.repo.BeginTx(ctx)
	if err != nil {
		h.logger.Error("trips begin tx", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	defer tx.Rollback(ctx)

	// Ensure we lock the trip for update to avoid race conditions.
	tForUpdate, err := h.repo.GetByIDForUpdateTx(ctx, tx, tripID)
	if err != nil || tForUpdate == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}

	tr, err := h.repo.ConfirmTransitionTx(ctx, tx, tripID, driverID, asDispatcher)
	if err != nil {
		switch err {
		case trips.ErrNotFound:
			resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		case trips.ErrForbiddenRole:
			resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
		case trips.ErrInvalidTransition:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_status_transition")
		case trips.ErrTripCompletionNeedsDriverFirst:
			resp.ErrorLang(c, http.StatusBadRequest, "trip_completion_needs_driver_first")
		case trips.ErrTripCompletionAlreadyPending:
			resp.ErrorLang(c, http.StatusBadRequest, "trip_completion_already_pending")
		default:
			h.logger.Error("confirm transition error", zap.Error(err))
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		}
		return
	}
	if tr.Status == trips.StatusInTransit {
		if err := h.cargoRepo.OnTripEnteredInTransitTx(ctx, tx, tr.CargoID); err != nil {
			h.logger.Error("on trip in transit error", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	if tr.Status == trips.StatusCompleted {
		if tr.DriverID == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "trip_missing_driver")
			return
		}
		if err := h.cargoRepo.ArchiveCompletedCargoTx(ctx, tx, tr.CargoID, tr.ID, *tr.DriverID); err != nil {
			h.logger.Error("archive completed cargo error", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_complete_trip")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("trips commit error", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	h.notifyTripTransition(ctx, tBefore, tr)
	resp.OKLang(c, "ok", toTripResp(tr))
}

// ConfirmTransitionDispatcher POST /v1/dispatchers/trips/:id/confirm-transition.
func (h *TripsHandler) ConfirmTransitionDispatcher(c *gin.Context) {
	h.runConfirmTransition(c, true)
}

// DriverConfirm POST /v1/driver/trips/:id/confirm — подтвердить следующий шаг рейса (переход статуса).
func (h *TripsHandler) DriverConfirm(c *gin.Context) {
	h.runConfirmTransition(c, false)
}

// DriverReject POST /v1/driver/trips/:id/reject — полный отказ от рейса на любом этапе до COMPLETED:
// рейс архивируется и удаляется из trips (как раньше cancel), оффер и слоты откатываются через OnTripCancelledTx.
func (h *TripsHandler) DriverReject(c *gin.Context) {
	h.runCancelTrip(c, false, false)
}

// runCancelTrip archives the trip (удаление из trips) и откатывает оффер/водителя.
// driverRestrictToInProgress: для водителя — разрешено только при статусе IN_PROGRESS (до выхода в IN_TRANSIT); диспетчер без ограничения.
func (h *TripsHandler) runCancelTrip(c *gin.Context, asDispatcher bool, driverRestrictToInProgress bool) {
	ctx := c.Request.Context()
	tripID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var driverID uuid.UUID
	if !asDispatcher {
		driverID = c.MustGet(mw.CtxDriverID).(uuid.UUID)
	}
	role := "driver"
	if asDispatcher {
		role = "dispatcher"
	}

	tx, err := h.repo.BeginTx(ctx)
	if err != nil {
		h.logger.Error("trips begin tx", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	defer tx.Rollback(ctx)

	t, err := h.repo.GetByIDForUpdateTx(ctx, tx, tripID)
	if err != nil {
		h.logger.Error("trip for update", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	if t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	if asDispatcher {
		dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
		var companyID *uuid.UUID
		if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
			if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
				companyID = &u
			}
		}
		cargoObj, _ := h.cargoRepo.GetByID(ctx, t.CargoID, false)
		if !h.dispatcherCanAccessTrip(c, t, cargoObj, dispID, companyID) {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}
	} else {
		if t.DriverID == nil || *t.DriverID != driverID {
			resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
			return
		}
	}
	if t.Status == trips.StatusCompleted {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_already_completed")
		return
	}
	if !asDispatcher && driverRestrictToInProgress && t.Status != trips.StatusInProgress {
		resp.ErrorLang(c, http.StatusBadRequest, "trip_cancel_only_in_progress")
		return
	}

	if err := h.repo.ArchiveTripAndDeleteTx(ctx, tx, tripID, role); err != nil {
		if err == trips.ErrNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
			return
		}
		h.logger.Error("archive trip", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	if err := h.cargoRepo.OnTripCancelledTx(ctx, tx, t.CargoID, t.OfferID, t.Status); err != nil {
		h.logger.Error("on trip cancelled", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("trips commit", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	h.notifyTripCancelled(ctx, t)
	resp.OKLang(c, "ok", gin.H{
		"status":   "cancelled",
		"trip_id":  tripID.String(),
		"cargo_id": t.CargoID.String(),
		"offer_id": t.OfferID.String(),
	})
}

// CancelTripDriver POST /v1/driver/trips/:id/cancel — только IN_PROGRESS: архив + удаление строки рейса (без отдельного «снятия» водителя через NULL).
func (h *TripsHandler) CancelTripDriver(c *gin.Context) {
	h.runCancelTrip(c, false, true)
}

// CancelTripDispatcher POST /v1/dispatchers/trips/:id/cancel
func (h *TripsHandler) CancelTripDispatcher(c *gin.Context) {
	h.runCancelTrip(c, true, false)
}

func (h *TripsHandler) runTripState(c *gin.Context, asDispatcher bool) {
	ctx := c.Request.Context()
	tripID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	t, err := h.repo.GetByID(ctx, tripID)
	if err != nil || t == nil {
		resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		return
	}
	if asDispatcher {
		dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
		var companyID *uuid.UUID
		if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
			if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
				companyID = &u
			}
		}
		cargoObj, _ := h.cargoRepo.GetByID(ctx, t.CargoID, false)
		if !h.dispatcherCanAccessTrip(c, t, cargoObj, dispID, companyID) {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}
	} else {
		driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
		if t.DriverID == nil || *t.DriverID != driverID {
			resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
			return
		}
	}
	cargoObj, _ := h.cargoRepo.GetByID(ctx, t.CargoID, false)
	out := toTripResp(t)
	if cargoObj != nil {
		out["cargo"] = gin.H{
			"id":     cargoObj.ID.String(),
			"status": string(cargoObj.Status),
		}
	}
	resp.OKLang(c, "ok", out)
}

// TripStateDriver GET /v1/driver/trips/:id/state
func (h *TripsHandler) TripStateDriver(c *gin.Context) {
	h.runTripState(c, false)
}

// TripStateDispatcher GET /v1/dispatchers/trips/:id/state
func (h *TripsHandler) TripStateDispatcher(c *gin.Context) {
	h.runTripState(c, true)
}

func eventAtFromHistoryItem(it gin.H) time.Time {
	v, ok := it["event_at"]
	if !ok {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	default:
		return time.Time{}
	}
}

func sortHistoryByEventAtDesc(items []gin.H) {
	sort.Slice(items, func(i, j int) bool {
		return eventAtFromHistoryItem(items[i]).After(eventAtFromHistoryItem(items[j]))
	})
}

func toTripResp(t *trips.Trip) gin.H {
	res := gin.H{
		"id":               t.ID.String(),
		"cargo_id":         t.CargoID.String(),
		"offer_id":         t.OfferID.String(),
		"status":           t.Status,
		"agreed_price":     t.AgreedPrice,
		"agreed_currency":  t.AgreedCurrency,
		"created_at":       t.CreatedAt,
		"updated_at":       t.UpdatedAt,
	}
	if t.DriverID != nil {
		res["driver_id"] = t.DriverID.String()
	}
	if t.PendingConfirmTo != nil {
		res["pending_confirm_to"] = *t.PendingConfirmTo
	}
	if t.DriverConfirmedAt != nil {
		res["driver_confirmed_at"] = t.DriverConfirmedAt
	}
	if t.DispatcherConfirmedAt != nil {
		res["dispatcher_confirmed_at"] = t.DispatcherConfirmedAt
	}
	if trips.CompletionPending(t) {
		res["completion_awaiting_dispatcher_confirm"] = true
	}
	if t.RatingFromDriver != nil {
		res["rating_from_driver"] = *t.RatingFromDriver
	}
	if t.RatingFromDispatcher != nil {
		res["rating_from_dispatcher"] = *t.RatingFromDispatcher
	}
	res["next_status"] = trips.NextStatus(t.Status)
	return res
}
