package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

// CargoManagerDriverManagersHandler provides Cargo Manager views over Driver Managers.
type CargoManagerDriverManagersHandler struct {
	logger *zap.Logger
	disp   *dispatchers.Repo
	drv    *drivers.Repo
}

func NewCargoManagerDriverManagersHandler(logger *zap.Logger, disp *dispatchers.Repo, drv *drivers.Repo) *CargoManagerDriverManagersHandler {
	return &CargoManagerDriverManagersHandler{logger: logger, disp: disp, drv: drv}
}

func (h *CargoManagerDriverManagersHandler) ensureCargoManager(c *gin.Context, dispatcherID uuid.UUID) bool {
	d, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
	if err != nil || d == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return false
	}
	role := ""
	if d.ManagerRole != nil {
		role = strings.TrimSpace(*d.ManagerRole)
	}
	if role != dispatchers.ManagerRoleCargoManager {
		resp.ErrorLang(c, http.StatusForbidden, "invalid_manager_role")
		return false
	}
	return true
}

// ListDriverManagersForCargoManager GET /v1/dispatchers/driver-managers
func (h *CargoManagerDriverManagersHandler) ListDriverManagersForCargoManager(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	if !h.ensureCargoManager(c, dispatcherID) {
		return
	}

	page := 1
	if v := strings.TrimSpace(c.Query("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	limit := 20
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	var hasPhoto *bool
	if v := strings.TrimSpace(c.Query("has_photo")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		hasPhoto = &b
	}
	var ratingMin *float64
	if v := strings.TrimSpace(c.Query("rating_min")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		ratingMin = &f
	}
	var ratingMax *float64
	if v := strings.TrimSpace(c.Query("rating_max")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		ratingMax = &f
	}

	role := dispatchers.ManagerRoleDriverManager
	filter := dispatchers.CatalogFilter{
		Q:           strings.TrimSpace(c.Query("q")),
		Status:      strPtrOrNil(strings.TrimSpace(c.Query("status"))),
		WorkStatus:  strPtrOrNil(strings.TrimSpace(c.Query("work_status"))),
		HasPhoto:    hasPhoto,
		RatingMin:   ratingMin,
		RatingMax:   ratingMax,
		ManagerRole: &role,
		Limit:       limit,
		Offset:      (page - 1) * limit,
	}
	list, total, err := h.disp.ListCatalog(c.Request.Context(), filter)
	if err != nil {
		h.logger.Error("list driver managers for cargo manager", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}

	items := make([]gin.H, 0, len(list))
	for i := range list {
		d := list[i]
		item := gin.H{
			"id":             d.ID,
			"name":           d.Name,
			"phone":          d.Phone,
			"rating":         d.Rating,
			"work_status":    d.WorkStatus,
			"status":         d.Status,
			"role":           d.ManagerRole,
			"has_photo":      d.HasPhoto,
			"last_online_at": d.LastOnlineAt,
		}
		if d.HasPhoto {
			item["photo_url"] = "/v1/chat/users/" + d.ID + "/photo"
		}
		if dmID, err := uuid.Parse(d.ID); err == nil && dmID != uuid.Nil {
			if cnt, err := h.drv.GetDriverCount(c.Request.Context(), dmID); err == nil {
				item["drivers_count"] = cnt
			}
		}
		items = append(items, item)
	}
	resp.OKLang(c, "ok", gin.H{
		"items":  items,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// ListDriversByDriverManagerForCargoManager GET /v1/dispatchers/driver-managers/:dispatcherId/drivers
func (h *CargoManagerDriverManagersHandler) ListDriversByDriverManagerForCargoManager(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	if !h.ensureCargoManager(c, dispatcherID) {
		return
	}

	managerID, err := uuid.Parse(c.Param("dispatcherId"))
	if err != nil || managerID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	dm, err := h.disp.FindByID(c.Request.Context(), managerID)
	if err != nil || dm == nil {
		resp.ErrorLang(c, http.StatusNotFound, "dispatcher_not_found")
		return
	}
	if dm.ManagerRole == nil || strings.TrimSpace(*dm.ManagerRole) != dispatchers.ManagerRoleDriverManager {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_manager_role")
		return
	}

	f := drivers.ListDriversFilter{
		Phone:         strings.TrimSpace(c.Query("phone")),
		Name:          strings.TrimSpace(c.Query("name")),
		WorkStatus:    strings.TrimSpace(c.Query("work_status")),
		TruckType:     strings.TrimSpace(c.Query("truck_type")),
		DriverType:    strings.TrimSpace(c.Query("driver_type")),
		AccountStatus: strings.TrimSpace(c.Query("account_status")),
		Page:          1,
		Limit:         20,
		Sort:          strings.TrimSpace(c.DefaultQuery("sort", "updated_at:desc")),
	}
	if p := strings.TrimSpace(c.Query("page")); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			f.Page = n
		}
	}
	if l := strings.TrimSpace(c.Query("limit")); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			f.Limit = n
		}
	}
	if v := strings.TrimSpace(c.Query("has_photo")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		f.HasPhoto = &b
	}

	list, total, err := h.drv.ListByManagerIDFilter(c.Request.Context(), managerID, f)
	if err != nil {
		h.logger.Error("list drivers by driver manager for cargo manager", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_drivers")
		return
	}
	if list == nil {
		list = []*drivers.Driver{}
	}
	totalPages := 0
	if f.Limit > 0 {
		totalPages = (total + f.Limit - 1) / f.Limit
	}
	resp.OKLang(c, "ok", gin.H{
		"driver_manager": gin.H{
			"id":             dm.ID,
			"name":           dm.Name,
			"phone":          dm.Phone,
			"rating":         dm.Rating,
			"work_status":    dm.WorkStatus,
			"status":         dm.Status,
			"has_photo":      dm.HasPhoto,
			"last_online_at": dm.LastOnlineAt,
		},
		"items":       list,
		"total":       total,
		"page":        f.Page,
		"limit":       f.Limit,
		"total_pages": totalPages,
		"has_next":    f.Page < totalPages,
	})
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

