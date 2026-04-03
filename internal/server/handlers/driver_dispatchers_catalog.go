package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/server/resp"
)

// DriverDispatchersCatalogHandler provides driver access to freelance dispatcher catalog.
type DriverDispatchersCatalogHandler struct {
	logger *zap.Logger
	disp   *dispatchers.Repo
}

func NewDriverDispatchersCatalogHandler(logger *zap.Logger, disp *dispatchers.Repo) *DriverDispatchersCatalogHandler {
	return &DriverDispatchersCatalogHandler{logger: logger, disp: disp}
}

// ListCatalog returns all freelance dispatchers with pagination + filters.
// GET /v1/driver/dispatchers/catalog
func (h *DriverDispatchersCatalogHandler) ListCatalog(c *gin.Context) {
	limit := parseIntDefault(c.Query("limit"), 20)
	offset := parseIntDefault(c.Query("offset"), 0)
	if limit < 1 || limit > 100 || offset < 0 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	phoneHint := strings.TrimSpace(c.Query("phone_hint"))
	status := strings.TrimSpace(c.Query("status"))
	workStatus := strings.TrimSpace(c.Query("work_status"))
	managerRole := strings.TrimSpace(c.Query("role"))

	var statusPtr *string
	if status != "" {
		statusPtr = &status
	}
	var workStatusPtr *string
	if workStatus != "" {
		workStatusPtr = &workStatus
	}

	var hasPhotoPtr *bool
	if v := strings.TrimSpace(c.Query("has_photo")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		hasPhotoPtr = &b
	}

	var ratingMinPtr *float64
	if v := strings.TrimSpace(c.Query("rating_min")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		ratingMinPtr = &f
	}
	var ratingMaxPtr *float64
	if v := strings.TrimSpace(c.Query("rating_max")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		ratingMaxPtr = &f
	}

	var managerRolePtr *string
	if managerRole != "" {
		if errKey := validateFreelanceDispatcherRole(managerRole); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
		managerRolePtr = &managerRole
	}

	items, total, err := h.disp.ListCatalog(c.Request.Context(), dispatchers.CatalogFilter{
		Q:           q,
		PhoneHint:   phoneHint,
		Status:      statusPtr,
		WorkStatus:  workStatusPtr,
		HasPhoto:    hasPhotoPtr,
		RatingMin:   ratingMinPtr,
		RatingMax:   ratingMaxPtr,
		ManagerRole: managerRolePtr,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		h.logger.Error("driver list dispatchers catalog", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_dispatchers")
		return
	}

	resp.OKLang(c, "ok", gin.H{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// HintByPhone returns matching dispatchers by phone prefix (typeahead).
// GET /v1/driver/dispatchers/hint?phone=+9989&limit=10
func (h *DriverDispatchersCatalogHandler) HintByPhone(c *gin.Context) {
	phone := strings.TrimSpace(c.Query("phone"))
	if phone == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "phone_required")
		return
	}
	limit := parseIntDefault(c.Query("limit"), 10)
	if limit < 1 || limit > 50 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	items, err := h.disp.HintByPhonePrefix(c.Request.Context(), phone, limit)
	if err != nil {
		h.logger.Error("driver hint dispatchers by phone", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_dispatchers")
		return
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

func parseIntDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

