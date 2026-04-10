package handlers

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/favorites"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

// DriverCargoSearchHandler handles nearby and matching cargo endpoints for drivers.
type DriverCargoSearchHandler struct {
	logger  *zap.Logger
	cargo   *cargo.Repo
	drivers *drivers.Repo
	fav     *favorites.Repo
}

func NewDriverCargoSearchHandler(logger *zap.Logger, cargoRepo *cargo.Repo, driversRepo *drivers.Repo, fav *favorites.Repo) *DriverCargoSearchHandler {
	return &DriverCargoSearchHandler{logger: logger, cargo: cargoRepo, drivers: driversRepo, fav: fav}
}

// NearbyCargoForDriver GET /v1/driver/nearby-cargo?lat=...&lng=...&page=1&limit=20
// Returns cargo sorted by distance from the given coordinates (main load point).
func (h *DriverCargoSearchHandler) NearbyCargoForDriver(c *gin.Context) {
	driverID, ok := c.Get(mw.CtxDriverID)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	uid, _ := driverID.(uuid.UUID)

	latStr := strings.TrimSpace(c.Query("lat"))
	lngStr := strings.TrimSpace(c.Query("lng"))
	if latStr == "" || lngStr == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "lat_lng_required")
		return
	}
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil || lat < -90 || lat > 90 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_lat")
		return
	}
	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil || lng < -180 || lng > 180 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_lng")
		return
	}

	f := cargo.NearbyFilter{
		Lat:   lat,
		Lng:   lng,
		Page:  getIntQuery(c, "page", 1),
		Limit: getIntQuery(c, "limit", 20),
	}

	if drv, _ := h.drivers.FindByID(c.Request.Context(), uid); drv != nil && drv.CompanyID != nil && *drv.CompanyID != "" {
		if cid, err := uuid.Parse(*drv.CompanyID); err == nil {
			f.ForDriverCompanyID = &cid
		}
	}

	result, err := h.cargo.ListNearby(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("nearby cargo", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}

	var liked map[uuid.UUID]bool
	if h.fav != nil && len(result.Items) > 0 {
		ids := make([]uuid.UUID, len(result.Items))
		for i := range result.Items {
			ids[i] = result.Items[i].Cargo.ID
		}
		var errL error
		liked, errL = h.fav.DriverLikedCargoIDs(c.Request.Context(), uid, ids)
		if errL != nil {
			h.logger.Warn("driver nearby cargo is_liked", zap.Error(errL))
			liked = map[uuid.UUID]bool{}
		}
	}

	items := make([]gin.H, 0, len(result.Items))
	for _, item := range result.Items {
		m := toCargoItem(&item.Cargo)
		if liked != nil {
			m["is_liked"] = liked[item.Cargo.ID]
		}
		m["distance_km"] = math.Round(item.DistanceKM*100) / 100
		m["origin_lat"] = item.OriginLat
		m["origin_lng"] = item.OriginLng
		items = append(items, m)
	}

	resp.OKLang(c, "ok", gin.H{
		"items": items,
		"total": result.Total,
		"page":  f.Page,
		"limit": f.Limit,
	})
}

// MatchingCargoForDriver GET /v1/driver/matching-cargo?page=1&limit=20
// Returns cargo matching the driver's trailer type, paginated.
func (h *DriverCargoSearchHandler) MatchingCargoForDriver(c *gin.Context) {
	driverID, ok := c.Get(mw.CtxDriverID)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	uid, _ := driverID.(uuid.UUID)

	drv, err := h.drivers.FindByID(c.Request.Context(), uid)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
		return
	}

	var truckTypes []string
	trailerType := ""
	if drv.TrailerPlateType != nil {
		trailerType = strings.ToUpper(strings.TrimSpace(*drv.TrailerPlateType))
	}
	if trailerType != "" {
		if matched, ok := cargo.TrailerToTruckTypes[trailerType]; ok {
			truckTypes = matched
		}
	}
	if len(truckTypes) == 0 {
		truckTypes = []string{"TENT", "REFRIGERATOR", "FLATBED", "TANKER", "OTHER"}
	}

	f := cargo.MatchingFilter{
		TruckTypes: truckTypes,
		Page:       getIntQuery(c, "page", 1),
		Limit:      getIntQuery(c, "limit", 20),
	}
	if drv.CompanyID != nil && *drv.CompanyID != "" {
		if cid, err := uuid.Parse(*drv.CompanyID); err == nil {
			f.ForDriverCompanyID = &cid
		}
	}

	result, err := h.cargo.ListMatching(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("matching cargo", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}

	listItems := toCargoListItems(result.Items)
	if h.fav != nil && len(result.Items) > 0 {
		ids := make([]uuid.UUID, len(result.Items))
		for i := range result.Items {
			ids[i] = result.Items[i].ID
		}
		flags, errL := h.fav.DriverLikedCargoIDs(c.Request.Context(), uid, ids)
		if errL != nil {
			h.logger.Warn("driver matching cargo is_liked", zap.Error(errL))
		} else {
			for i := range listItems {
				listItems[i]["is_liked"] = flags[result.Items[i].ID]
			}
		}
	}

	resp.OKLang(c, "ok", gin.H{
		"items":         listItems,
		"total":         result.Total,
		"page":          f.Page,
		"limit":         f.Limit,
		"trailer_type":  trailerType,
		"matched_types": truckTypes,
	})
}
