package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/trips"
)

type TripsHandler struct {
	logger    *zap.Logger
	repo      *trips.Repo
	cargoRepo *cargo.Repo
	drivers   *drivers.Repo
}

func NewTripsHandler(logger *zap.Logger, repo *trips.Repo, cargoRepo *cargo.Repo, driversRepo *drivers.Repo) *TripsHandler {
	return &TripsHandler{logger: logger, repo: repo, cargoRepo: cargoRepo, drivers: driversRepo}
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

// ListMy for GET /v1/driver/trips (driver): returns trips assigned to current driver.
func (h *TripsHandler) ListMy(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.repo.ListByDriver(c.Request.Context(), driverID, limit)
	if err != nil {
		h.logger.Error("trips list my", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	out := make([]interface{}, 0, len(list))
	for i := range list {
		out = append(out, toTripResp(&list[i]))
	}
	resp.OKLang(c, "ok", gin.H{"items": out})
}

// GetMy returns a single trip for the assigned driver.
func (h *TripsHandler) GetMy(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
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
	if t.DriverID == nil || *t.DriverID != driverID {
		resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
		return
	}
	resp.OKLang(c, "ok", toTripResp(t))
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
	if !dispatcherOwnsCargo(cargoObj, dispID, companyID) {
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
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
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
	items := make([]gin.H, 0, len(activeAndDone)+len(archived))
	for i := range activeAndDone {
		t := &activeAndDone[i]
		if t.Status == trips.StatusCompleted {
			items = append(items, gin.H{
				"event_type": "trip_completed",
				"event_at":   t.UpdatedAt,
				"trip":       toTripResp(t),
			})
		}
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
		items = append(items, gin.H{
			"event_type": "offer_accepted",
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
	items := make([]gin.H, 0, len(current)+len(archived))
	for i := range current {
		t := &current[i]
		if t.Status == trips.StatusCompleted {
			items = append(items, gin.H{
				"event_type": "trip_completed",
				"event_at":   t.UpdatedAt,
				"trip":       toTripResp(t),
			})
		}
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
	accepted, _ := h.cargoRepo.ListDispatcherSentOffers(c.Request.Context(), dispID, "ACCEPTED", "all", nil, limit, 0)
	for i := range accepted {
		o := accepted[i]
		items = append(items, gin.H{
			"event_type": "offer_accepted",
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
	rejected, _ := h.cargoRepo.ListDispatcherSentOffers(c.Request.Context(), dispID, "REJECTED", "all", nil, limit, 0)
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
	resp.OKLang(c, "ok", gin.H{"items": items, "total": len(items), "limit": limit})
}

// ListMyActive GET /v1/driver/trips/active — только активные рейсы (не COMPLETED/CANCELLED) с полными данными trip + cargo (как карточка груза: route_points, payment).
func (h *TripsHandler) ListMyActive(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.repo.ListActiveByDriver(c.Request.Context(), driverID, limit)
	if err != nil {
		h.logger.Error("trips list my active", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	out := make([]gin.H, 0, len(list))
	for i := range list {
		t := &list[i]
		item := toTripResp(t)
		if h.cargoRepo != nil {
			cobj, _ := h.cargoRepo.GetByID(c.Request.Context(), t.CargoID, false)
			if cobj != nil {
				pts, _ := h.cargoRepo.GetRoutePoints(c.Request.Context(), t.CargoID)
				pay, _ := h.cargoRepo.GetPayment(c.Request.Context(), t.CargoID)
				item["cargo"] = toCargoDetail(cobj, pts, pay, nil)
			} else {
				item["cargo"] = nil
			}
		}
		out = append(out, item)
	}
	resp.OKLang(c, "ok", gin.H{
		"items": out,
		"total": len(out),
		"limit": limit,
	})
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
	var driverID uuid.UUID
	if !asDispatcher {
		driverID = c.MustGet(mw.CtxDriverID).(uuid.UUID)
	}
	if asDispatcher {
		dispID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
		var companyID *uuid.UUID
		if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
			if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
				companyID = &u
			}
		}
		t0, err := h.repo.GetByID(ctx, tripID)
		if err != nil || t0 == nil {
			resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
			return
		}
		cargoObj, _ := h.cargoRepo.GetByID(ctx, t0.CargoID, false)
		if !dispatcherOwnsCargo(cargoObj, dispID, companyID) {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}
	}

	tx, err := h.repo.BeginTx(ctx)
	if err != nil {
		h.logger.Error("trips begin tx", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	defer tx.Rollback(ctx)

	tr, err := h.repo.ConfirmTransitionTx(ctx, tx, tripID, driverID, asDispatcher)
	if err != nil {
		switch err {
		case trips.ErrNotFound:
			resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
		case trips.ErrForbiddenRole:
			resp.ErrorLang(c, http.StatusForbidden, "trip_not_assigned_to_you")
		case trips.ErrInvalidTransition:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_status_transition")
		default:
			h.logger.Error("confirm transition", zap.Error(err))
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		}
		return
	}
	if tr.Status == trips.StatusInTransit {
		if err := h.cargoRepo.OnTripEnteredInTransitTx(ctx, tx, tr.CargoID); err != nil {
			h.logger.Error("on trip in transit", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
			return
		}
	}
	if tr.Status == trips.StatusCompleted {
		if tr.DriverID == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "trip_missing_driver")
			return
		}
		if err := h.cargoRepo.ArchiveCompletedCargoTx(ctx, tx, tr.CargoID, tr.ID, *tr.DriverID); err != nil {
			h.logger.Error("archive completed cargo", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_complete_trip")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("trips commit", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	resp.OKLang(c, "ok", toTripResp(tr))
}

// ConfirmTransitionDriver POST /v1/driver/trips/:id/confirm-transition — bilateral next step (with dispatcher).
func (h *TripsHandler) ConfirmTransitionDriver(c *gin.Context) {
	h.runConfirmTransition(c, false)
}

// ConfirmTransitionDispatcher POST /v1/dispatchers/trips/:id/confirm-transition.
func (h *TripsHandler) ConfirmTransitionDispatcher(c *gin.Context) {
	h.runConfirmTransition(c, true)
}

// DriverConfirm is an alias for the first bilateral step (backward compatible with POST .../confirm).
func (h *TripsHandler) DriverConfirm(c *gin.Context) {
	h.runConfirmTransition(c, false)
}

// DriverReject clears driver assignment so dispatcher can assign another.
func (h *TripsHandler) DriverReject(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	tripID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	if err := h.repo.DriverReject(c.Request.Context(), tripID, driverID); err != nil {
		if err == trips.ErrNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "trip_not_found")
			return
		}
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": trips.StatusInProgress})
}

func (h *TripsHandler) runCancelTrip(c *gin.Context, asDispatcher bool) {
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
		if !dispatcherOwnsCargo(cargoObj, dispID, companyID) {
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
	resp.OKLang(c, "ok", gin.H{
		"status":   "cancelled",
		"trip_id":  tripID.String(),
		"cargo_id": t.CargoID.String(),
		"offer_id": t.OfferID.String(),
	})
}

// CancelTripDriver POST /v1/driver/trips/:id/cancel
func (h *TripsHandler) CancelTripDriver(c *gin.Context) {
	h.runCancelTrip(c, false)
}

// CancelTripDispatcher POST /v1/dispatchers/trips/:id/cancel
func (h *TripsHandler) CancelTripDispatcher(c *gin.Context) {
	h.runCancelTrip(c, true)
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
		if !dispatcherOwnsCargo(cargoObj, dispID, companyID) {
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
	res["next_status"] = trips.NextStatus(t.Status)
	return res
}
