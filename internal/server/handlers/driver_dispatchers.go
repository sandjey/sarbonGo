package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchercompanies"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

// DriverDispatchersHandler handles driver's "my dispatchers" list and unlink (leave freelance dispatcher).
type DriverDispatchersHandler struct {
	logger *zap.Logger
	drv    *drivers.Repo
	disp   *dispatchers.Repo
	dcr    *dispatchercompanies.Repo
}

// NewDriverDispatchersHandler creates the handler.
func NewDriverDispatchersHandler(logger *zap.Logger, drv *drivers.Repo, disp *dispatchers.Repo, dcr *dispatchercompanies.Repo) *DriverDispatchersHandler {
	return &DriverDispatchersHandler{logger: logger, drv: drv, disp: disp, dcr: dcr}
}

// ListMyDispatchers returns dispatchers linked to the current driver: freelance (freelancer_id) + company dispatchers (if driver has company_id).
// GET /v1/driver/dispatchers
func (h *DriverDispatchersHandler) ListMyDispatchers(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
		return
	}
	items := make([]gin.H, 0)

	// 1) Freelance dispatcher (driver accepted invitation from this dispatcher)
	if drv.FreelancerID != nil && *drv.FreelancerID != "" {
		dispID, err := uuid.Parse(*drv.FreelancerID)
		if err == nil {
			d, err := h.disp.FindByID(c.Request.Context(), dispID)
			if err == nil && d != nil {
				items = append(items, dispatcherToItem(d, "freelance", nil))
			}
		}
	}

	// 2) Company dispatchers (owner + roles)
	if drv.CompanyID != nil && *drv.CompanyID != "" {
		companyID, err := uuid.Parse(*drv.CompanyID)
		if err == nil {
			list, err := h.dcr.ListDispatchersByCompany(c.Request.Context(), companyID)
			if err != nil {
				h.logger.Error("list dispatchers by company", zap.Error(err))
				resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_dispatchers")
				return
			}
			seen := make(map[uuid.UUID]bool)
			for _, row := range list {
				if seen[row.DispatcherID] {
					continue
				}
				seen[row.DispatcherID] = true
				d, err := h.disp.FindByID(c.Request.Context(), row.DispatcherID)
				if err != nil || d == nil {
					continue
				}
				role := row.Role
				items = append(items, dispatcherToItem(d, "company", &role))
			}
		}
	}

	resp.OKLang(c, "ok", gin.H{"items": items})
}

// GetMyDispatcher returns one linked dispatcher by ID with full public fields.
// GET /v1/driver/my-driver-managers/:dispatcherId
func (h *DriverDispatchersHandler) GetMyDispatcher(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	dispatcherID, err := uuid.Parse(c.Param("dispatcherId"))
	if err != nil || dispatcherID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_dispatcher_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
		return
	}
	d, linkType, companyRole, ok, err := h.findLinkedDispatcher(c, drv, dispatcherID)
	if err != nil {
		h.logger.Error("get linked dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_dispatchers")
		return
	}
	if !ok || d == nil {
		resp.ErrorLang(c, http.StatusForbidden, "dispatcher_not_linked_to_you")
		return
	}

	out := gin.H{
		"id":              d.ID,
		"name":            d.Name,
		"phone":           d.Phone,
		"passport_series": d.PassportSeries,
		"passport_number": d.PassportNumber,
		"pinfl":           d.PINFL,
		"cargo_id":        d.CargoID,
		"driver_id":       d.DriverID,
		"rating":          d.Rating,
		"work_status":     d.WorkStatus,
		"status":          d.Status,
		"role":            d.ManagerRole,
		"photo":           d.Photo,
		"has_photo":       d.HasPhoto,
		"created_at":      d.CreatedAt,
		"updated_at":      d.UpdatedAt,
		"last_online_at":  d.LastOnlineAt,
		"type":            linkType,
	}
	if companyRole != nil {
		out["company_role"] = *companyRole
	}
	resp.OKLang(c, "ok", gin.H{"item": out})
}

func dispatcherToItem(d *dispatchers.Dispatcher, linkType string, companyRole *string) gin.H {
	item := gin.H{
		"id":          d.ID,
		"name":        d.Name,
		"phone":       d.Phone,
		"work_status": d.WorkStatus,
		"has_photo":   d.HasPhoto,
		"type":        linkType,
	}
	if d.ManagerRole != nil && *d.ManagerRole != "" {
		item["role"] = *d.ManagerRole
	}
	if companyRole != nil {
		item["company_role"] = *companyRole
	}
	return item
}

// UnlinkDispatcher removes the driver from the freelance dispatcher (driver leaves this dispatcher). Only allowed if driver.freelancer_id = dispatcherId.
// DELETE /v1/driver/dispatchers/:dispatcherId
func (h *DriverDispatchersHandler) UnlinkDispatcher(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	dispatcherID, err := uuid.Parse(c.Param("dispatcherId"))
	if err != nil || dispatcherID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_dispatcher_id")
		return
	}
	ok, err := h.drv.UnlinkFromFreelancer(c.Request.Context(), driverID, dispatcherID)
	if err != nil {
		h.logger.Error("driver unlink dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_unlink")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusForbidden, "dispatcher_not_linked_to_you")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": "unlinked"})
}

func (h *DriverDispatchersHandler) findLinkedDispatcher(c *gin.Context, drv *drivers.Driver, dispatcherID uuid.UUID) (*dispatchers.Dispatcher, string, *string, bool, error) {
	if drv.FreelancerID != nil && *drv.FreelancerID != "" {
		freelancerID, err := uuid.Parse(*drv.FreelancerID)
		if err == nil && freelancerID == dispatcherID {
			d, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
			if err == nil && d != nil {
				return d, "freelance", nil, true, nil
			}
		}
	}

	if drv.CompanyID != nil && *drv.CompanyID != "" {
		companyID, err := uuid.Parse(*drv.CompanyID)
		if err != nil {
			return nil, "", nil, false, nil
		}
		list, err := h.dcr.ListDispatchersByCompany(c.Request.Context(), companyID)
		if err != nil {
			return nil, "", nil, false, err
		}
		for _, row := range list {
			if row.DispatcherID != dispatcherID {
				continue
			}
			d, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
			if err != nil || d == nil {
				return nil, "", nil, false, nil
			}
			role := row.Role
			return d, "company", &role, true, nil
		}
	}
	return nil, "", nil, false, nil
}
