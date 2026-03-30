package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/cargodrivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

type CargoDriversHandler struct {
	logger     *zap.Logger
	cargoRepo  *cargo.Repo
	cdRepo     *cargodrivers.Repo
}

func NewCargoDriversHandler(logger *zap.Logger, cargoRepo *cargo.Repo, cdRepo *cargodrivers.Repo) *CargoDriversHandler {
	return &CargoDriversHandler{logger: logger, cargoRepo: cargoRepo, cdRepo: cdRepo}
}

// ListByCargo GET /v1/dispatchers/cargo/:id/drivers (dispatcher).
func (h *CargoDriversHandler) ListByCargo(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	obj, _ := h.cargoRepo.GetByID(c.Request.Context(), cargoID, false)
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if obj.CreatedByType == nil || *obj.CreatedByType != "DISPATCHER" || obj.CreatedByID == nil || *obj.CreatedByID != dispatcherID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	list, err := h.cdRepo.ListByCargo(c.Request.Context(), cargoID, limit)
	if err != nil {
		h.logger.Error("cargo drivers list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	items := make([]gin.H, 0, len(list))
	for i := range list {
		cd := &list[i]
		items = append(items, gin.H{
			"id":         cd.ID.String(),
			"cargo_id":   cd.CargoID.String(),
			"driver_id":  cd.DriverID.String(),
			"status":     cd.Status,
			"created_at": cd.CreatedAt,
			"updated_at": cd.UpdatedAt,
		})
	}
	resp.OKLang(c, "ok", gin.H{
		"cargo_id":       cargoID.String(),
		"vehicles_amount": obj.VehiclesAmount,
		"vehicles_left":   obj.VehiclesLeft,
		"items":           items,
	})
}

// RemoveReq body for POST /v1/dispatchers/cargo/:id/drivers/remove
type RemoveReq struct {
	DriverID string `json:"driver_id" binding:"required,uuid"`
}

// RemoveFromCargo POST /v1/dispatchers/cargo/:id/drivers/remove (dispatcher).
// NOTE: current behavior uses trip CANCELLED flow for returning slot; here we just mark as CANCELLED to free driver and slot.
func (h *CargoDriversHandler) RemoveFromCargo(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	obj, _ := h.cargoRepo.GetByID(c.Request.Context(), cargoID, false)
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if obj.CreatedByType == nil || *obj.CreatedByType != "DISPATCHER" || obj.CreatedByID == nil || *obj.CreatedByID != dispatcherID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	var req RemoveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	driverID, _ := uuid.Parse(req.DriverID)
	if driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}

	// Mark cancelled and return slot back.
	if err := h.cargoRepo.MarkDriverCancelled(c.Request.Context(), cargoID, driverID); err != nil {
		h.logger.Error("cargo drivers remove", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_update")
		return
	}
	resp.OKLang(c, "updated", gin.H{"cargo_id": cargoID.String(), "driver_id": driverID.String(), "status": "cancelled"})
}

// GetMyActiveCargo GET /v1/driver/active-cargo (driver).
func (h *CargoDriversHandler) GetMyActiveCargo(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	cargoID, err := h.cdRepo.GetActiveCargoIDByDriver(c.Request.Context(), driverID)
	if err != nil {
		h.logger.Error("active cargo get", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if cargoID == nil {
		resp.OKLang(c, "ok", gin.H{"active": false})
		return
	}
	obj, _ := h.cargoRepo.GetByID(c.Request.Context(), *cargoID, false)
	if obj == nil {
		resp.OKLang(c, "ok", gin.H{"active": false})
		return
	}
	resp.OKLang(c, "ok", gin.H{"active": true, "cargo": toCargoItem(obj)})
}

