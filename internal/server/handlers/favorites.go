package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/favorites"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

type FavoritesHandler struct {
	logger      *zap.Logger
	favRepo     *favorites.Repo
	cargoRepo   *cargo.Repo
	driversRepo *drivers.Repo
}

func NewFavoritesHandler(logger *zap.Logger, favRepo *favorites.Repo, cargoRepo *cargo.Repo, driversRepo *drivers.Repo) *FavoritesHandler {
	return &FavoritesHandler{
		logger:      logger,
		favRepo:     favRepo,
		cargoRepo:   cargoRepo,
		driversRepo: driversRepo,
	}
}

type addFavoriteCargoReq struct {
	CargoID uuid.UUID `json:"cargo_id" binding:"required"`
}

type addFavoriteDriverReq struct {
	DriverID uuid.UUID `json:"driver_id" binding:"required"`
}

// --- Driver favorites cargo ---

func (h *FavoritesHandler) AddDriverFavoriteCargo(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)

	var req addFavoriteCargoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	cargoObj, err := h.cargoRepo.GetByID(c.Request.Context(), req.CargoID, false)
	if err != nil || cargoObj == nil {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("favorite cargo add: cargo get failed", zap.Error(err))
		}
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}

	inserted, err := h.favRepo.AddDriverCargoFavorite(c.Request.Context(), driverID, req.CargoID)
	if err != nil {
		h.logger.Error("favorite cargo add: db insert failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_favorite")
		return
	}

	if inserted {
		resp.SuccessLang(c, http.StatusCreated, "favorite_added", gin.H{"cargo_id": req.CargoID.String()})
		return
	}
	resp.OKLang(c, "favorite_already_exists", gin.H{"cargo_id": req.CargoID.String()})
}

func (h *FavoritesHandler) DeleteDriverFavoriteCargo(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	cargoID, err := uuid.Parse(c.Param("cargoId"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}

	ok, err := h.favRepo.DeleteDriverCargoFavorite(c.Request.Context(), driverID, cargoID)
	if err != nil {
		h.logger.Error("favorite cargo delete: db delete failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_delete_favorite")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusNotFound, "favorite_not_found")
		return
	}

	resp.OKLang(c, "favorite_deleted", gin.H{"cargo_id": cargoID.String()})
}

func (h *FavoritesHandler) ListDriverFavoriteCargo(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)

	limit := 30
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	list, err := h.favRepo.ListDriverCargoFavorites(c.Request.Context(), driverID, limit)
	if err != nil {
		h.logger.Error("favorite cargo list: db list failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_favorites")
		return
	}

	items := make([]gin.H, 0, len(list))
	for _, f := range list {
		cargoObj, _ := h.cargoRepo.GetByID(c.Request.Context(), f.CargoID, false)
		if cargoObj == nil {
			continue
		}
		items = append(items, gin.H{
			"cargo_id":   f.CargoID.String(),
			"created_at": f.CreatedAt,
			"cargo": gin.H{
				"id":          cargoObj.ID.String(),
				"weight":      cargoObj.Weight,
				"volume":      cargoObj.Volume,
				"truck_type":  cargoObj.TruckType,
				"status":      cargoObj.Status,
			},
		})
	}

	resp.OKLang(c, "ok", gin.H{"items": items})
}

// --- Freelance dispatcher favorites cargo ---

func (h *FavoritesHandler) AddDispatcherFavoriteCargo(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)

	var req addFavoriteCargoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	cargoObj, err := h.cargoRepo.GetByID(c.Request.Context(), req.CargoID, false)
	if err != nil || cargoObj == nil {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("dispatcher favorite cargo add: cargo get failed", zap.Error(err))
		}
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}

	inserted, err := h.favRepo.AddDispatcherCargoFavorite(c.Request.Context(), dispatcherID, req.CargoID)
	if err != nil {
		h.logger.Error("dispatcher favorite cargo add: db insert failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_favorite")
		return
	}

	if inserted {
		resp.SuccessLang(c, http.StatusCreated, "favorite_added", gin.H{"cargo_id": req.CargoID.String()})
		return
	}
	resp.OKLang(c, "favorite_already_exists", gin.H{"cargo_id": req.CargoID.String()})
}

func (h *FavoritesHandler) DeleteDispatcherFavoriteCargo(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	cargoID, err := uuid.Parse(c.Param("cargoId"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}

	ok, err := h.favRepo.DeleteDispatcherCargoFavorite(c.Request.Context(), dispatcherID, cargoID)
	if err != nil {
		h.logger.Error("dispatcher favorite cargo delete: db delete failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_delete_favorite")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusNotFound, "favorite_not_found")
		return
	}

	resp.OKLang(c, "favorite_deleted", gin.H{"cargo_id": cargoID.String()})
}

func (h *FavoritesHandler) ListDispatcherFavoriteCargo(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)

	limit := 30
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	list, err := h.favRepo.ListDispatcherCargoFavorites(c.Request.Context(), dispatcherID, limit)
	if err != nil {
		h.logger.Error("dispatcher favorite cargo list: db list failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_favorites")
		return
	}

	items := make([]gin.H, 0, len(list))
	for _, f := range list {
		cargoObj, _ := h.cargoRepo.GetByID(c.Request.Context(), f.CargoID, false)
		if cargoObj == nil {
			continue
		}
		items = append(items, gin.H{
			"cargo_id":   f.CargoID.String(),
			"created_at": f.CreatedAt,
			"cargo": gin.H{
				"id":          cargoObj.ID.String(),
				"weight":      cargoObj.Weight,
				"volume":      cargoObj.Volume,
				"truck_type":  cargoObj.TruckType,
				"status":      cargoObj.Status,
			},
		})
	}

	resp.OKLang(c, "ok", gin.H{"items": items})
}

// --- Freelance dispatcher favorites drivers ---

func (h *FavoritesHandler) AddDispatcherFavoriteDriver(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)

	var req addFavoriteDriverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	drv, err := h.driversRepo.FindByID(c.Request.Context(), req.DriverID)
	if err != nil || drv == nil {
		if err != nil && !errors.Is(err, drivers.ErrNotFound) {
			h.logger.Error("dispatcher favorite driver add: driver get failed", zap.Error(err))
		}
		resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
		return
	}

	inserted, err := h.favRepo.AddDispatcherDriverFavorite(c.Request.Context(), dispatcherID, req.DriverID)
	if err != nil {
		h.logger.Error("dispatcher favorite driver add: db insert failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_favorite")
		return
	}

	if inserted {
		resp.SuccessLang(c, http.StatusCreated, "favorite_added", gin.H{"driver_id": req.DriverID.String()})
		return
	}
	resp.OKLang(c, "favorite_already_exists", gin.H{"driver_id": req.DriverID.String()})
}

func (h *FavoritesHandler) DeleteDispatcherFavoriteDriver(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	driverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}

	ok, err := h.favRepo.DeleteDispatcherDriverFavorite(c.Request.Context(), dispatcherID, driverID)
	if err != nil {
		h.logger.Error("dispatcher favorite driver delete: db delete failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_delete_favorite")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusNotFound, "favorite_not_found")
		return
	}

	resp.OKLang(c, "favorite_deleted", gin.H{"driver_id": driverID.String()})
}

func (h *FavoritesHandler) ListDispatcherFavoriteDrivers(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)

	limit := 30
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	list, err := h.favRepo.ListDispatcherDriverFavorites(c.Request.Context(), dispatcherID, limit)
	if err != nil {
		h.logger.Error("dispatcher favorite driver list: db list failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_favorites")
		return
	}

	items := make([]gin.H, 0, len(list))
	for _, f := range list {
		drv, _ := h.driversRepo.FindByID(c.Request.Context(), f.DriverID)
		if drv == nil {
			continue
		}
		items = append(items, gin.H{
			"driver_id":  drv.ID,
			"created_at": f.CreatedAt,
			"driver": gin.H{
				"id":           drv.ID,
				"phone":        drv.Phone,
				"name":         drv.Name,
				"work_status": drv.WorkStatus,
				"driver_type": drv.DriverType,
				"rating":       drv.Rating,
			},
		})
	}

	resp.OKLang(c, "ok", gin.H{"items": items})
}

