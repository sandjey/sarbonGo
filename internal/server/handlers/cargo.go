package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/config"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/favorites"
	"sarbonNew/internal/reference"
	"sarbonNew/internal/security"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/trips"
	"sarbonNew/internal/userstream"
)

const maxCargoPhotoSize = 10 * 1024 * 1024 // 10MB
// maxCargoPhotosOnCreate — макс. число файлов за один POST /api/cargo (multipart).
const maxCargoPhotosOnCreate = 5

var allowedCargoPhotoTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

var (
	errCargoPhotoTooLarge = errors.New("file too large")
	errCargoPhotoBadType  = errors.New("disallowed image type")
	// errDuplicatePendingCargoPhoto — одна и та же pending-фото дважды в photos[].
	errDuplicatePendingCargoPhoto = errors.New("duplicate pending cargo photo reference")
)

type CargoHandler struct {
	logger    *zap.Logger
	repo      *cargo.Repo
	tripsRepo *trips.Repo
	drivers   *drivers.Repo
	fav       *favorites.Repo
	jwtm      *security.JWTManager
	cfg       config.Config
	stream    *userstream.Hub
}

func NewCargoHandler(logger *zap.Logger, repo *cargo.Repo, tripsRepo *trips.Repo, driversRepo *drivers.Repo, fav *favorites.Repo, jwtm *security.JWTManager, cfg config.Config, stream *userstream.Hub) *CargoHandler {
	return &CargoHandler{logger: logger, repo: repo, tripsRepo: tripsRepo, drivers: driversRepo, fav: fav, jwtm: jwtm, cfg: cfg, stream: stream}
}

// CreateCargoReq body for POST /api/cargo.
type CreateCargoReq struct {
	Name                 *string          `json:"name"`
	Weight               float64          `json:"weight" binding:"required,gt=0"`
	Volume               float64          `json:"volume" binding:"required,gt=0"` // объём груза (м³)
	VehiclesAmount       int              `json:"vehicles_amount" binding:"required,gte=1,lte=100"`
	Packaging            *string          `json:"packaging"`
	PackagingAmount      *int             `json:"packaging_amount"`
	Dimensions           *string          `json:"dimensions"`
	Photos               []string         `json:"photos"` // до 5: внешние URL и/или pending с POST /api/cargo/photos (url или UUID)
	WayPoints            []WayPointReq    `json:"way_points"`
	ReadyEnabled         bool             `json:"ready_enabled"`
	ReadyAt              *string          `json:"ready_at"`
	Comment              *string          `json:"comment"`
	PowerPlateType       string           `json:"power_plate_type" binding:"required"`
	TrailerPlateType     string           `json:"trailer_plate_type" binding:"required"`
	TempMin              *float64         `json:"temp_min"`
	TempMax              *float64         `json:"temp_max"`
	ADREnabled           bool             `json:"adr_enabled"`
	ADRClass             *string          `json:"adr_class"`
	LoadingTypes         []string         `json:"loading_types"`
	UnloadingTypes       []string         `json:"unloading_types"`
	IsTwoDriversRequired bool             `json:"is_two_drivers_required"`
	ShipmentType         *string          `json:"shipment_type"`
	BeltsCount           *int             `json:"belts_count"`
	Documents            *cargo.Documents `json:"documents"`
	ContactName          *string          `json:"contact_name"`
	ContactPhone         *string          `json:"contact_phone"`
	CargoTypeID          *uuid.UUID       `json:"cargo_type_id"`
	RoutePoints          []RoutePointReq  `json:"route_points" binding:"required,dive"`
	Payment              *PaymentReq      `json:"payment"`
	CompanyID            *uuid.UUID       `json:"company_id"`
}

type RoutePointReq struct {
	Type         string  `json:"type" binding:"required,oneof=LOAD UNLOAD CUSTOMS TRANSIT"`
	CountryCode  string  `json:"country_code" binding:"required"` // код страны (UZ, AE, RU и т.д.)
	CityCode     string  `json:"city_code" binding:"required"`    // код города (TAS, SAM, DXB) — из справочника
	RegionCode   string  `json:"region_code"`                     // код региона/области (опционально)
	Address      string  `json:"address" binding:"required"`      // адрес (улица, дом)
	Orientir     string  `json:"orientir"`                        // ориентир для водителя
	Lat          float64 `json:"lat" binding:"required"`
	Lng          float64 `json:"lng" binding:"required"`
	PlaceID      *string `json:"place_id"` // ID от карт для автокомплита
	Comment      *string `json:"comment"`
	PointOrder   int     `json:"point_order" binding:"required"`
	IsMainLoad   bool    `json:"is_main_load"`
	IsMainUnload bool    `json:"is_main_unload"`
	// Date — плановая дата/время точки (RFC3339, хранится и отдаётся в UTC).
	Date *string `json:"date" binding:"required"`
}

type PaymentReq struct {
	IsNegotiable       bool     `json:"is_negotiable"`
	PriceRequest       bool     `json:"price_request"`
	TotalAmount        *float64 `json:"total_amount"`
	TotalCurrency      *string  `json:"total_currency"`
	WithPrepayment     bool     `json:"with_prepayment"`
	PrepaymentAmount   *float64 `json:"prepayment_amount"`
	PrepaymentCurrency *string  `json:"prepayment_currency"`
	PrepaymentType     *string  `json:"prepayment_type"`
	RemainingAmount    *float64 `json:"remaining_amount"`
	RemainingCurrency  *string  `json:"remaining_currency"`
	RemainingType      *string  `json:"remaining_type"`
	PaymentNote        *string  `json:"payment_note"`
	PaymentTermsNote   *string  `json:"payment_terms_note"`
}

type WayPointReq struct {
	Type        string  `json:"type"`
	CountryCode string  `json:"country_code"`
	CityCode    string  `json:"city_code"`
	RegionCode  string  `json:"region_code"`
	Address     string  `json:"address"`
	Orientir    string  `json:"orientir"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	PlaceID     *string `json:"place_id"`
	Comment     *string `json:"comment"`
}

func (h *CargoHandler) Create(c *gin.Context) {
	var req CreateCargoReq
	var multipartPhotoFiles []*multipart.FileHeader

	ct := strings.ToLower(c.ContentType())
	if strings.Contains(ct, "multipart/form-data") {
		// До 5 файлов × 10 MB + JSON — запас по памяти для парсера.
		if err := c.Request.ParseMultipartForm(64 << 20); err != nil {
			h.logger.Info("cargo create multipart parse failed", zap.Error(err))
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{
				"fields": gin.H{"_": "invalid_multipart"},
			})
			return
		}
		jsonPayload := strings.TrimSpace(c.PostForm("data"))
		if jsonPayload == "" {
			jsonPayload = strings.TrimSpace(c.PostForm("payload"))
		}
		if jsonPayload == "" {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{
				"fields": gin.H{"data": "multipart_requires_json_string_field_data_or_payload"},
			})
			return
		}
		if err := json.Unmarshal([]byte(jsonPayload), &req); err != nil {
			h.logger.Info("cargo create multipart: invalid JSON in data", zap.Error(err))
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{
				"fields": gin.H{"_": "invalid_json_or_types"},
			})
			return
		}
		multipartPhotoFiles = c.Request.MultipartForm.File["photos"]
		if len(multipartPhotoFiles) == 0 {
			multipartPhotoFiles = c.Request.MultipartForm.File["photo"]
		}
		if len(multipartPhotoFiles) > maxCargoPhotosOnCreate {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{
				"fields": gin.H{"photos": fmt.Sprintf("max_%d_files_per_request", maxCargoPhotosOnCreate)},
			})
			return
		}
		if len(req.Photos)+len(multipartPhotoFiles) > maxCargoPhotosOnCreate {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{
				"fields": gin.H{"photos": fmt.Sprintf("photos_urls_plus_files_max_%d", maxCargoPhotosOnCreate)},
			})
			return
		}
	} else {
		if err := c.ShouldBindJSON(&req); err != nil {
			h.logger.Info("cargo create validation failed (bind)", zap.Error(err))
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{
				"fields": gin.H{"_": "invalid_json_or_types"},
			})
			return
		}
	}
	if err := validateCargoCreate(req); err != nil {
		h.logger.Info("cargo create validation failed", zap.Error(err))
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{
			"fields": gin.H{"_": err.Error()},
		})
		return
	}
	routeInputs, err := buildRoutePointInputs(req.RoutePoints)
	if err != nil {
		h.logger.Info("cargo create route_points validation failed", zap.Error(err))
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{
			"fields": gin.H{"_": err.Error()},
		})
		return
	}
	if req.CompanyID != nil {
		ok, err := h.repo.CompanyExists(c.Request.Context(), *req.CompanyID)
		if err != nil {
			h.logger.Error("cargo create: company exists check failed", zap.Error(err), zap.String("company_id", req.CompanyID.String()))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if !ok {
			h.logger.Info("cargo create validation failed: company not found", zap.String("company_id", req.CompanyID.String()))
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "company_not_found", gin.H{
				"fields": gin.H{"company_id": "not_found"},
			})
			return
		}
	}
	if req.CargoTypeID != nil {
		ok, err := h.repo.CargoTypeExists(c.Request.Context(), *req.CargoTypeID)
		if err != nil {
			h.logger.Error("cargo create: cargo type exists check failed", zap.Error(err), zap.String("cargo_type_id", req.CargoTypeID.String()))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if !ok {
			h.logger.Info("cargo create validation failed: cargo type not found", zap.String("cargo_type_id", req.CargoTypeID.String()))
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "cargo_type_not_found", gin.H{
				"fields": gin.H{"cargo_type_id": "not_found"},
			})
			return
		}
	}
	externalPhotos, pendingPhotoOrder, err := h.prepareCargoCreatePhotos(c.Request.Context(), req.Photos)
	if err != nil {
		switch {
		case errors.Is(err, cargo.ErrPendingCargoPhotoNotFound):
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "pending_photo_not_found", gin.H{
				"fields": gin.H{"photos": "pending_photo_not_found"},
			})
		case errors.Is(err, errDuplicatePendingCargoPhoto):
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{
				"fields": gin.H{"photos": "duplicate_pending_photo_ref"},
			})
		default:
			h.logger.Error("cargo create: pending photos check failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	params := toCreateParams(req)
	params.Photos = externalPhotos
	params.RoutePoints = routeInputs
	params.CompanyID = req.CompanyID
	// Dev toggle: skip moderation and publish immediately.
	if !h.cfg.CargoModerationEnabled {
		params.Status = cargo.StatusSearchingAll
	}
	// Автоматически записываем, кто создал груз: admin, dispatcher или company
	raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken))
	if raw != "" && h.jwtm != nil {
		if userID, role, err := h.jwtm.ParseAccess(raw); err == nil {
			switch role {
			case "admin":
				params.CreatedByType = strPtr("ADMIN")
				params.CreatedByID = &userID
			case "dispatcher":
				params.CreatedByType = strPtr("DISPATCHER")
				params.CreatedByID = &userID
				// Лимит грузов для фриланс-диспетчера (из env)
				if h.cfg.FreelanceDispatcherCargoLimit > 0 {
					count, err := h.repo.CountByDispatcher(c.Request.Context(), userID)
					if err != nil {
						h.logger.Error("cargo count by dispatcher", zap.Error(err))
						resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_check_cargo_limit")
						return
					}
					if count >= h.cfg.FreelanceDispatcherCargoLimit {
						resp.ErrorWithData(c, http.StatusForbidden, "cargo limit reached for freelance dispatcher", gin.H{
							"limit":   h.cfg.FreelanceDispatcherCargoLimit,
							"current": count,
						})
						return
					}
				}
			}
		}
	}
	// Если создатель не определён по JWT, но передан company_id — считаем создателем компанию
	if params.CreatedByType == nil && req.CompanyID != nil {
		params.CreatedByType = strPtr("COMPANY")
		params.CreatedByID = req.CompanyID
		params.CompanyID = req.CompanyID
	}
	var uploaderID *uuid.UUID
	if raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken)); raw != "" && h.jwtm != nil {
		if userID, _, err := h.jwtm.ParseAccess(raw); err == nil && userID != uuid.Nil {
			uploaderID = &userID
		}
	}

	id, err := h.repo.Create(c.Request.Context(), params)
	if err != nil {
		// Turn FK violations into 400 with a clear field name.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "fk_cargo_company_id":
				resp.ErrorWithDataLang(c, http.StatusBadRequest, "company_not_found", gin.H{"fields": gin.H{"company_id": "not_found"}})
				return
			case "fk_cargo_cargo_type":
				resp.ErrorWithDataLang(c, http.StatusBadRequest, "cargo_type_not_found", gin.H{"fields": gin.H{"cargo_type_id": "not_found"}})
				return
			}
		}
		h.logger.Error("cargo create", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_cargo")
		return
	}
	claimedPending := make(map[uuid.UUID]uuid.UUID, len(pendingPhotoOrder))
	for _, pid := range pendingPhotoOrder {
		newPhotoID, err := h.repo.ClaimPendingCargoPhoto(c.Request.Context(), pid, id, uploaderID)
		if err != nil {
			data := gin.H{"cargo_id": id.String()}
			h.logger.Error("cargo create: claim pending photo", zap.Error(err), zap.String("cargo_id", id.String()))
			resp.ErrorWithDataLang(c, http.StatusInternalServerError, "cargo_created_pending_photo_claim_failed", data)
			return
		}
		claimedPending[pid] = newPhotoID
	}

	multipartPhotoIDs := make([]uuid.UUID, 0, len(multipartPhotoFiles))
	for _, fh := range multipartPhotoFiles {
		photoID, err := h.saveCargoPhotoFromFileHeader(c.Request.Context(), c, id, fh)
		if err != nil {
			data := gin.H{"cargo_id": id.String()}
			switch {
			case errors.Is(err, errCargoPhotoTooLarge):
				data["max_size_mb"] = 10
				data["max_size_bytes"] = maxCargoPhotoSize
				resp.ErrorWithDataLang(c, http.StatusBadRequest, "file_too_large", data)
			case errors.Is(err, errCargoPhotoBadType):
				resp.ErrorWithDataLang(c, http.StatusBadRequest, "allowed_image_types", data)
			default:
				h.logger.Error("cargo create: attached photo failed", zap.Error(err), zap.String("cargo_id", id.String()))
				resp.ErrorWithDataLang(c, http.StatusInternalServerError, "cargo_created_photo_upload_failed", data)
			}
			return
		}
		multipartPhotoIDs = append(multipartPhotoIDs, photoID)
	}
	finalPhotoURLs := h.buildCargoPhotoURLsAfterCreate(req.Photos, id, claimedPending, multipartPhotoIDs)
	if len(finalPhotoURLs) > 0 {
		if err := h.repo.SetCargoPhotoURLs(c.Request.Context(), id, finalPhotoURLs); err != nil {
			h.logger.Error("cargo create: set photo_urls", zap.Error(err), zap.String("cargo_id", id.String()))
			resp.ErrorWithDataLang(c, http.StatusInternalServerError, "cargo_created_photo_upload_failed", gin.H{"cargo_id": id.String()})
			return
		}
	}
	// Возвращаем полный объект груза (как GET /api/cargo/:id), чтобы клиент видел все сохранённые данные
	obj, err := h.repo.GetByID(c.Request.Context(), id, false)
	if err != nil || obj == nil {
		resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"id": id.String()})
		return
	}
	points, _ := h.repo.GetRoutePoints(c.Request.Context(), id)
	pay, _ := h.repo.GetPayment(c.Request.Context(), id)
	detail := toCargoDetail(obj, points, pay, nil)
	viewer := parseCargoAPIViewer(c, h.jwtm)
	if flags := h.cargoLikedFlags(c.Request.Context(), viewer, []uuid.UUID{id}); flags != nil {
		applyIsLikedToDetail(detail, flags, id)
	}
	resp.SuccessLang(c, http.StatusCreated, "created", detail)
}

func (h *CargoHandler) prepareCargoCreatePhotos(ctx context.Context, photos []string) (external []string, pendingOrdered []uuid.UUID, err error) {
	seen := make(map[uuid.UUID]struct{})
	for _, p := range photos {
		if pid, ok := cargo.ParsePendingCargoPhotoRef(p); ok {
			if _, dup := seen[pid]; dup {
				return nil, nil, errDuplicatePendingCargoPhoto
			}
			seen[pid] = struct{}{}
			exists, err := h.repo.PendingCargoPhotoExists(ctx, pid)
			if err != nil {
				return nil, nil, err
			}
			if !exists {
				return nil, nil, cargo.ErrPendingCargoPhotoNotFound
			}
			pendingOrdered = append(pendingOrdered, pid)
		} else {
			external = append(external, p)
		}
	}
	return external, pendingOrdered, nil
}

func (h *CargoHandler) buildCargoPhotoURLsAfterCreate(photos []string, cargoID uuid.UUID, claimed map[uuid.UUID]uuid.UUID, multipartIDs []uuid.UUID) []string {
	out := make([]string, 0, len(photos)+len(multipartIDs))
	cargoPrefix := "/api/cargo/" + cargoID.String() + "/photos/"
	for _, p := range photos {
		if pid, ok := cargo.ParsePendingCargoPhotoRef(p); ok {
			if nid, ok := claimed[pid]; ok {
				out = append(out, cargoPrefix+nid.String())
			}
		} else {
			out = append(out, p)
		}
	}
	for _, mid := range multipartIDs {
		out = append(out, cargoPrefix+mid.String())
	}
	return out
}

func (h *CargoHandler) saveCargoPhotoFromFileHeader(ctx context.Context, c *gin.Context, cargoID uuid.UUID, file *multipart.FileHeader) (uuid.UUID, error) {
	if file.Size > maxCargoPhotoSize {
		return uuid.Nil, errCargoPhotoTooLarge
	}
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	if !allowedCargoPhotoTypes[contentType] {
		return uuid.Nil, errCargoPhotoBadType
	}
	f, err := file.Open()
	if err != nil {
		return uuid.Nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return uuid.Nil, err
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[contentType]
	storageRoot := strings.TrimSpace(os.Getenv("CARGO_STORAGE_DIR"))
	if storageRoot == "" {
		storageRoot = "storage"
	}
	dir := filepath.Join(storageRoot, "cargo", cargoID.String(), "photos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return uuid.Nil, err
	}
	photoUUID := uuid.New()
	path := filepath.Join(dir, photoUUID.String()+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return uuid.Nil, err
	}
	var uploaderID *uuid.UUID
	if raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken)); raw != "" && h.jwtm != nil {
		if userID, _, err := h.jwtm.ParseAccess(raw); err == nil && userID != uuid.Nil {
			uploaderID = &userID
		}
	}
	return h.repo.CreateCargoPhoto(ctx, cargoID, uploaderID, contentType, int64(len(data)), path)
}

func (h *CargoHandler) savePendingCargoPhotoFromFileHeader(ctx context.Context, c *gin.Context, file *multipart.FileHeader) (uuid.UUID, error) {
	if file.Size > maxCargoPhotoSize {
		return uuid.Nil, errCargoPhotoTooLarge
	}
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	if !allowedCargoPhotoTypes[contentType] {
		return uuid.Nil, errCargoPhotoBadType
	}
	f, err := file.Open()
	if err != nil {
		return uuid.Nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return uuid.Nil, err
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[contentType]
	storageRoot := strings.TrimSpace(os.Getenv("CARGO_STORAGE_DIR"))
	if storageRoot == "" {
		storageRoot = "storage"
	}
	photoID := uuid.New()
	dir := filepath.Join(storageRoot, "cargo", "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return uuid.Nil, err
	}
	path := filepath.Join(dir, photoID.String()+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return uuid.Nil, err
	}
	var uploaderID *uuid.UUID
	if raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken)); raw != "" && h.jwtm != nil {
		if userID, _, err := h.jwtm.ParseAccess(raw); err == nil && userID != uuid.Nil {
			uploaderID = &userID
		}
	}
	if err := h.repo.InsertPendingCargoPhoto(ctx, photoID, uploaderID, contentType, int64(len(data)), path); err != nil {
		_ = os.Remove(path)
		return uuid.Nil, err
	}
	return photoID, nil
}

// UploadPendingCargoPhoto загружает фото до создания груза (без cargo_id).
// POST /api/cargo/photos multipart field "photo".
func (h *CargoHandler) UploadPendingCargoPhoto(c *gin.Context) {
	file, err := c.FormFile("photo")
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "photo_file_required")
		return
	}
	photoID, err := h.savePendingCargoPhotoFromFileHeader(c.Request.Context(), c, file)
	if err != nil {
		if errors.Is(err, errCargoPhotoTooLarge) {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "file_too_large", gin.H{
				"max_size_mb":    10,
				"max_size_bytes": maxCargoPhotoSize,
			})
			return
		}
		if errors.Is(err, errCargoPhotoBadType) {
			resp.ErrorLang(c, http.StatusBadRequest, "allowed_image_types")
			return
		}
		h.logger.Error("pending cargo photo upload", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "photo_uploaded", gin.H{
		"id":  photoID.String(),
		"url": "/api/cargo/photos/" + photoID.String(),
	})
}

// GetPendingCargoPhoto отдаёт бинарник временного фото до привязки к грузу.
// GET /api/cargo/photos/:photoId
func (h *CargoHandler) GetPendingCargoPhoto(c *gin.Context) {
	photoID, err := uuid.Parse(c.Param("photoId"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	p, err := h.repo.GetPendingCargoPhotoByID(c.Request.Context(), photoID)
	if err != nil || p == nil {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}
	etag := weakETagBytes(data)
	if inm := strings.TrimSpace(c.GetHeader("If-None-Match")); inm != "" && inm == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, p.Mime, data)
}

// UploadPhoto uploads one cargo photo and returns photo id + url.
// POST /api/cargo/:id/photos (multipart/form-data: photo=file)
func (h *CargoHandler) UploadPhoto(c *gin.Context) {
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	// ensure cargo exists
	obj, _ := h.repo.GetByID(c.Request.Context(), cargoID, false)
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}

	file, err := c.FormFile("photo")
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "photo_file_required")
		return
	}
	photoID, err := h.saveCargoPhotoFromFileHeader(c.Request.Context(), c, cargoID, file)
	if err != nil {
		if errors.Is(err, errCargoPhotoTooLarge) {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "file_too_large", gin.H{
				"max_size_mb":    10,
				"max_size_bytes": maxCargoPhotoSize,
			})
			return
		}
		if errors.Is(err, errCargoPhotoBadType) {
			resp.ErrorLang(c, http.StatusBadRequest, "allowed_image_types")
			return
		}
		h.logger.Error("cargo photo upload", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "photo_uploaded", gin.H{
		"id":  photoID.String(),
		"url": "/api/cargo/" + cargoID.String() + "/photos/" + photoID.String(),
	})
}

// ListPhotos lists cargo photos metadata.
// GET /api/cargo/:id/photos
func (h *CargoHandler) ListPhotos(c *gin.Context) {
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	obj, _ := h.repo.GetByID(c.Request.Context(), cargoID, false)
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	list, err := h.repo.ListCargoPhotos(c.Request.Context(), cargoID)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, p := range list {
		out = append(out, gin.H{
			"id":         p.ID.String(),
			"mime":       p.Mime,
			"size_bytes": p.SizeBytes,
			"created_at": p.CreatedAt,
			"url":        "/api/cargo/" + cargoID.String() + "/photos/" + p.ID.String(),
		})
	}
	resp.OKLang(c, "ok", gin.H{"items": out})
}

// GetPhoto returns binary photo.
// GET /api/cargo/:id/photos/:photoId
func (h *CargoHandler) GetPhoto(c *gin.Context) {
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	photoID, err := uuid.Parse(c.Param("photoId"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	p, err := h.repo.GetCargoPhotoForUser(c.Request.Context(), photoID)
	if err != nil || p == nil || p.CargoID != cargoID {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}
	etag := weakETagBytes(data)
	if inm := strings.TrimSpace(c.GetHeader("If-None-Match")); inm != "" && inm == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, p.Mime, data)
}

// DeletePhoto deletes photo (metadata + file).
// DELETE /api/cargo/:id/photos/:photoId
func (h *CargoHandler) DeletePhoto(c *gin.Context) {
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	photoID, err := uuid.Parse(c.Param("photoId"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	p, err := h.repo.GetCargoPhotoForUser(c.Request.Context(), photoID)
	if err != nil || p == nil || p.CargoID != cargoID {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}
	_ = os.Remove(p.Path)
	if err := h.repo.DeleteCargoPhoto(c.Request.Context(), photoID); err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "photo_deleted", gin.H{"deleted": true})
}

func (h *CargoHandler) List(c *gin.Context) {
	f := parseCargoListFilterFromQuery(c)
	var viewer *cargoAPIViewer
	// Optional JWT-based restrictions:
	// - driver: when listing "searching" cargo, show only SEARCHING_ALL + SEARCHING_COMPANY (his company)
	// - dispatcher (freelance): show only cargo created by this dispatcher (created_by_type=DISPATCHER, created_by_id=me)
	if h.jwtm != nil {
		if raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken)); raw != "" {
			if userID, role, _, _, err := h.jwtm.ParseAccessWithSID(raw); err == nil && userID != uuid.Nil {
				switch role {
				case "driver":
					viewer = &cargoAPIViewer{DriverID: &userID}
					if h.drivers != nil {
						if drv, _ := h.drivers.FindByID(c.Request.Context(), userID); drv != nil && drv.CompanyID != nil && *drv.CompanyID != "" {
							if cid, err := uuid.Parse(*drv.CompanyID); err == nil {
								f.ForDriverCompanyID = &cid
							}
						}
					}
				case "dispatcher":
					viewer = &cargoAPIViewer{DispatcherID: &userID}
					f2 := f
					f2.CreatedByDispatcherID = &userID
					f = f2
				}
			}
		}
	}
	h.listCargoPage(c, f, viewer)
}

// ListActiveCargoForDriver POST /v1/driver/cargo/active
// Возвращает активные грузы, отсортированные по расстоянию от координат водителя (от ближайшего к дальнему),
// с опциональным ограничением по радиусу (radius_km).
func (h *CargoHandler) ListActiveCargoForDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)

	var req struct {
		Lat      float64  `json:"lat"`
		Lng      float64  `json:"lng"`
		RadiusKM *float64 `json:"radius_km"`
		Page     int      `json:"page"`
		Limit    int      `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if req.Lat < -90 || req.Lat > 90 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_lat")
		return
	}
	if req.Lng < -180 || req.Lng > 180 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_lng")
		return
	}
	if req.RadiusKM != nil && *req.RadiusKM <= 0 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	f := cargo.NearbyFilter{
		Lat:      req.Lat,
		Lng:      req.Lng,
		RadiusKM: req.RadiusKM,
		Page:     req.Page,
		Limit:    req.Limit,
	}
	if h.drivers != nil {
		if drv, err := h.drivers.FindByID(c.Request.Context(), driverID); err == nil && drv != nil && drv.CompanyID != nil && *drv.CompanyID != "" {
			if cid, err := uuid.Parse(*drv.CompanyID); err == nil {
				f.ForDriverCompanyID = &cid
			}
		}
	}

	result, err := h.repo.ListNearby(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("driver cargo active nearby", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}

	viewer := &cargoAPIViewer{DriverID: &driverID}
	var liked map[uuid.UUID]bool
	if len(result.Items) > 0 {
		ids := make([]uuid.UUID, len(result.Items))
		for i := range result.Items {
			ids[i] = result.Items[i].Cargo.ID
		}
		liked = h.cargoLikedFlags(c.Request.Context(), viewer, ids)
	}
	items := make([]gin.H, 0, len(result.Items))
	for _, item := range result.Items {
		points, _ := h.repo.GetRoutePoints(c.Request.Context(), item.Cargo.ID)
		pay, _ := h.repo.GetPayment(c.Request.Context(), item.Cargo.ID)
		m := toCargoDetail(&item.Cargo, points, pay, nil)
		applyIsLikedToDetail(m, liked, item.Cargo.ID)
		distKM := math.Round(item.DistanceKM*1000) / 1000
		m["distance_km"] = distKM
		m["distance_m"] = int(math.Round(item.DistanceKM * 1000))
		m["origin_lat"] = item.OriginLat
		m["origin_lng"] = item.OriginLng
		items = append(items, m)
	}

	resp.OKLang(c, "ok", gin.H{
		"items":     items,
		"total":     result.Total,
		"page":      req.Page,
		"limit":     req.Limit,
		"radius_km": req.RadiusKM,
		"sort":      "distance:asc",
	})
}

// ListActiveCargoForDispatcher GET /v1/dispatchers/cargo/active — маркетплейс: все активные грузы в поиске по базе
// (не только созданные этим диспетчером). По умолчанию status=SEARCHING_ALL; можно расширить status, q, фильтры.
func (h *CargoHandler) ListActiveCargoForDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	f := parseCargoListFilterFromQuery(c)
	if len(f.Status) == 0 {
		f.Status = []string{string(cargo.StatusSearchingAll)}
	}
	// без CreatedByDispatcherID — полный каталог
	h.listCargoPage(c, f, &cargoAPIViewer{DispatcherID: &dispatcherID})
}

func parseCargoListFilterFromQuery(c *gin.Context) cargo.ListFilter {
	f := cargo.ListFilter{
		Page:         getIntQuery(c, "page", 1),
		Limit:        getIntQuery(c, "limit", 20),
		Sort:         c.DefaultQuery("sort", "created_at:desc"),
		TruckType:    strings.TrimSpace(c.Query("truck_type")),
		FromCityCode: strings.TrimSpace(c.Query("from_city_code")),
		ToCityCode:   strings.TrimSpace(c.Query("to_city_code")),
		CreatedFrom:  strings.TrimSpace(c.Query("created_from")),
		CreatedTo:    strings.TrimSpace(c.Query("created_to")),
	}
	if v := c.Query("status"); v != "" {
		f.Status = strings.Split(v, ",")
		for i := range f.Status {
			f.Status[i] = strings.TrimSpace(strings.ToUpper(f.Status[i]))
		}
	}
	if v := c.Query("weight_min"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.WeightMin = &n
		}
	}
	if v := c.Query("weight_max"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.WeightMax = &n
		}
	}
	if v := c.Query("with_offers"); v != "" {
		b := strings.ToLower(v) == "true" || v == "1"
		f.WithOffers = &b
	}
	q := strings.TrimSpace(c.Query("q"))
	if q != "" {
		f.NameContains = q
	}
	if v := c.Query("company_id"); v != "" {
		if id, err := uuid.Parse(strings.TrimSpace(v)); err == nil && id != uuid.Nil {
			f.CompanyID = &id
		}
	}
	return f
}

// ListMyCargoForDispatcher GET /v1/dispatchers/cargo/mine — только мои грузы (created_by=DISPATCHER me) с пагинацией/фильтрами.
func (h *CargoHandler) ListMyCargoForDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	f := parseCargoListFilterFromQuery(c)
	if len(f.Status) == 0 {
		f.Status = []string{string(cargo.StatusSearchingAll)}
	}
	f2 := f
	f2.CreatedByDispatcherID = &dispatcherID
	h.listCargoPage(c, f2, &cargoAPIViewer{DispatcherID: &dispatcherID})
}

// ListAllCargoForDispatcher GET /v1/dispatchers/cargo/all — каталог всех грузов с фильтрами/сортировкой/пагинацией.
func (h *CargoHandler) ListAllCargoForDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	f := parseCargoListFilterFromQuery(c)
	if len(f.Status) == 0 {
		f.Status = []string{string(cargo.StatusSearchingAll)}
	}
	h.listCargoPage(c, f, &cargoAPIViewer{DispatcherID: &dispatcherID})
}

func (h *CargoHandler) listCargoPage(c *gin.Context, f cargo.ListFilter, viewer *cargoAPIViewer) {
	result, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("cargo list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	var liked map[uuid.UUID]bool
	if viewer != nil && len(result.Items) > 0 {
		ids := make([]uuid.UUID, len(result.Items))
		for i := range result.Items {
			ids[i] = result.Items[i].ID
		}
		liked = h.cargoLikedFlags(c.Request.Context(), viewer, ids)
	}
	items := make([]gin.H, 0, len(result.Items))
	for i := range result.Items {
		points, _ := h.repo.GetRoutePoints(c.Request.Context(), result.Items[i].ID)
		pay, _ := h.repo.GetPayment(c.Request.Context(), result.Items[i].ID)
		m := toCargoDetail(&result.Items[i], points, pay, nil)
		applyIsLikedToDetail(m, liked, result.Items[i].ID)
		items = append(items, m)
	}
	resp.OKLang(c, "ok", gin.H{
		"items": items,
		"total": result.Total,
	})
}

func (h *CargoHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	obj, err := h.repo.GetByID(c.Request.Context(), id, false)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_get_cargo")
		return
	}
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	points, _ := h.repo.GetRoutePoints(c.Request.Context(), id)
	pay, _ := h.repo.GetPayment(c.Request.Context(), id)
	var stats *cargo.OfferInvitationStats
	if st, err := h.repo.GetOfferInvitationStats(c.Request.Context(), id); err == nil {
		stats = &st
	}
	detail := toCargoDetail(obj, points, pay, stats)
	if flags := h.cargoLikedFlags(c.Request.Context(), parseCargoAPIViewer(c, h.jwtm), []uuid.UUID{id}); flags != nil {
		applyIsLikedToDetail(detail, flags, id)
	}
	resp.OKLang(c, "ok", detail)
}

func (h *CargoHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var req UpdateCargoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if err := validateCargoUpdate(req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	params := toUpdateParams(req)
	if len(req.RoutePoints) > 0 {
		rps, err := buildRoutePointInputs(req.RoutePoints)
		if err != nil {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "validation_failed", gin.H{"fields": gin.H{"_": err.Error()}})
			return
		}
		params.RoutePoints = rps
	}
	if err := h.repo.Update(c.Request.Context(), id, params); err != nil {
		if err == cargo.ErrCannotEditAfterAssigned {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		h.logger.Error("cargo update", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_update_cargo")
		return
	}
	resp.OKLang(c, "ok", gin.H{"id": id.String()})
}

func (h *CargoHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		h.logger.Error("cargo delete", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_delete_cargo")
		return
	}
	resp.OKLang(c, "ok", gin.H{"id": id.String()})
}

func (h *CargoHandler) PatchStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var req struct {
		Status string `json:"status" binding:"required,oneof=PENDING_MODERATION SEARCHING_ALL SEARCHING_COMPANY CANCELLED"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if err := h.repo.SetStatus(c.Request.Context(), id, req.Status); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	resp.OKLang(c, "updated", gin.H{"id": id.String(), "status": req.Status})
}

func (h *CargoHandler) CreateOffer(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	obj, _ := h.repo.GetByID(c.Request.Context(), id, false)
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if !cargo.IsSearching(obj.Status) {
		resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
		return
	}
	var req struct {
		CarrierID uuid.UUID `json:"carrier_id" binding:"required"`
		Price     float64   `json:"price" binding:"required"`
		Currency  string    `json:"currency" binding:"required"`
		Comment   string    `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if obj.Status == cargo.StatusSearchingCompany {
		if obj.CompanyID == nil {
			resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
			return
		}
		if h.drivers != nil {
			if drv, _ := h.drivers.FindByID(c.Request.Context(), req.CarrierID); drv == nil || drv.CompanyID == nil || *drv.CompanyID != obj.CompanyID.String() {
				resp.ErrorLang(c, http.StatusForbidden, "cargo_visible_only_to_company_drivers")
				return
			}
		}
	}
	fullPath := strings.ToLower(strings.TrimSpace(c.FullPath()))
	forceDriverManager := strings.Contains(fullPath, "/offers/send-by-driver-manager")
	forceCargoManager := strings.Contains(fullPath, "/offers/send-by-cargo-manager")
	proposedBy := cargo.OfferProposedByDriver
	if h.jwtm != nil {
		raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken))
		if raw != "" {
			if userID, role, err := h.jwtm.ParseAccess(raw); err == nil && userID != uuid.Nil {
				switch role {
				case "dispatcher":
					var dispCompanyID *uuid.UUID
					if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
						if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
							dispCompanyID = &u
						}
					}
					if forceDriverManager {
						isManager, _ := h.drivers.IsLinked(c.Request.Context(), req.CarrierID, userID)
						if !isManager {
							resp.ErrorLang(c, http.StatusForbidden, "not_your_driver_or_cargo")
							return
						}
						proposedBy = cargo.OfferProposedByDriverManager
						break
					}
					if forceCargoManager {
						if !dispatcherOwnsCargoForNegotiation(obj, userID, dispCompanyID) {
							resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
							return
						}
						proposedBy = cargo.OfferProposedByDispatcher
						break
					}
					// If dispatcher owns cargo, it's a normal DISPATCHER offer (to driver).
					if dispatcherOwnsCargoForNegotiation(obj, userID, dispCompanyID) {
						proposedBy = cargo.OfferProposedByDispatcher
					} else {
						// Check if dispatcher is a MANAGER for this carrier (driver).
						isManager, _ := h.drivers.IsLinked(c.Request.Context(), req.CarrierID, userID)
						if !isManager {
							resp.ErrorLang(c, http.StatusForbidden, "not_your_driver_or_cargo")
							return
						}
						proposedBy = cargo.OfferProposedByDriverManager
					}
				case "driver":
					if req.CarrierID != userID {
						resp.ErrorLang(c, http.StatusForbidden, "carrier_must_be_self")
						return
					}
					proposedBy = cargo.OfferProposedByDriver
				}
			}
		}
	}
	var pByID *uuid.UUID
	if proposedBy == cargo.OfferProposedByDispatcher || proposedBy == cargo.OfferProposedByDriverManager {
		uid, role, _ := h.jwtm.ParseAccess(strings.TrimSpace(c.GetHeader(mw.HeaderUserToken)))
		if role == "dispatcher" {
			pByID = &uid
		}
	}

	offerID, err := h.repo.CreateOffer(c.Request.Context(), id, req.CarrierID, req.Price, req.Currency, req.Comment, proposedBy, pByID)
	if err != nil {
		if err == cargo.ErrCargoNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
			return
		}
		if err == cargo.ErrOfferPriceOutOfRange {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{
				"reason": "offer_price_out_of_range",
			})
			return
		}
		if err == cargo.ErrDispatcherOfferAlreadyExists {
			resp.ErrorLang(c, http.StatusConflict, "dispatcher_offer_already_exists")
			return
		}
		if err == cargo.ErrDriverOfferAlreadyExists {
			resp.ErrorLang(c, http.StatusConflict, "driver_offer_already_exists")
			return
		}
		if err == cargo.ErrCargoSlotsFull {
			resp.ErrorWithDataLang(c, http.StatusConflict, "cargo_slots_full", gin.H{
				"cargo_id":        id.String(),
				"vehicles_left":   obj.VehiclesLeft,
				"required_left":   0,
				"explanation_key": "cargo_slots_full",
			})
			return
		}
		if err == cargo.ErrCargoNotSearching {
			resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
			return
		}
		h.logger.Error("cargo create offer", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_offer")
		return
	}
	if h.stream != nil {
		if proposedBy == cargo.OfferProposedByDispatcher {
			h.stream.PublishNotification(tripnotif.RecipientDriver, req.CarrierID, gin.H{
				"kind":       "cargo_offer",
				"event":      "cargo_offer_created",
				"direction":  "incoming",
				"offer_id":   offerID.String(),
				"cargo_id":   id.String(),
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
		} else {
			if disp := tripNotifyDispatcherID(obj); disp != nil && *disp != uuid.Nil {
				h.stream.PublishNotification(tripnotif.RecipientDispatcher, *disp, gin.H{
					"kind":       "cargo_offer",
					"event":      "cargo_offer_created",
					"direction":  "incoming",
					"offer_id":   offerID.String(),
					"cargo_id":   id.String(),
					"driver_id":  req.CarrierID.String(),
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				})
			}
		}
	}
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"id": offerID.String()})
}

// ListSentOffersForDispatcher GET /v1/dispatchers/offers/all
func (h *CargoHandler) ListSentOffersForDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	page := getIntQuery(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := getIntQuery(c, "limit", 30)
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	direction := strings.ToLower(strings.TrimSpace(c.DefaultQuery("direction", "all")))
	switch direction {
	case "all", "both":
		direction = "all"
	case "outgoing", "from_me", "sent", "by":
		direction = "outgoing"
	case "incoming", "to_me", "received":
		direction = "incoming"
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	var counterpartyID *uuid.UUID
	rawCounterparty := strings.TrimSpace(c.Query("counterparty_id"))
	if rawCounterparty == "" {
		if direction == "outgoing" {
			rawCounterparty = strings.TrimSpace(c.Query("to_driver_id"))
		} else {
			rawCounterparty = strings.TrimSpace(c.Query("from_driver_id"))
		}
	}
	if rawCounterparty != "" {
		id, err := uuid.Parse(rawCounterparty)
		if err != nil || id == uuid.Nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
			return
		}
		counterpartyID = &id
	}

	total, err := h.repo.CountDispatcherSentOffers(c.Request.Context(), dispatcherID, status, direction, counterpartyID)
	if err != nil {
		h.logger.Error("dispatcher sent offers count", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_sent_offers")
		return
	}
	rows, err := h.repo.ListDispatcherSentOffers(c.Request.Context(), dispatcherID, status, direction, counterpartyID, limit, offset)
	if err != nil {
		h.logger.Error("dispatcher sent offers list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_sent_offers")
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		pb := strings.ToUpper(strings.TrimSpace(row.ProposedBy))
		sourceRole := "CARGO_MANAGER"
		sourceID := dispatcherID.String()
		switch pb {
		case cargo.OfferProposedByDriverManager:
			sourceRole = "DRIVER_MANAGER"
			if row.ProposedByID != nil && *row.ProposedByID != uuid.Nil {
				sourceID = row.ProposedByID.String()
			} else {
				sourceID = dispatcherID.String()
			}
		case cargo.OfferProposedByDriver:
			if direction == "incoming" || direction == "all" {
				sourceRole = "DRIVER"
				sourceID = row.CarrierID.String()
			}
		}
		item := gin.H{
			"cargo_id": row.CargoID.String(),
			"cargo": gin.H{
				"id":                     row.CargoID.String(),
				"name":                   row.CargoName,
				"status":                 row.CargoStatus,
				"from_city_code":         row.CargoFromCityCode,
				"to_city_code":           row.CargoToCityCode,
				"vehicles_amount":        row.CargoVehiclesAmount,
				"vehicles_left":          row.CargoVehiclesLeft,
				"current_price":          row.CargoCurrentPrice,
				"current_price_currency": row.CargoCurrentCurrency,
			},
			"offer": gin.H{
				"id":               row.ID.String(),
				"driver_id":        row.CarrierID.String(),
				"proposed_by":      row.ProposedBy,
				"price":            row.Price,
				"invitation_price": row.Price,
				"currency":         row.Currency,
				"comment":          row.Comment,
				"created_at":       row.CreatedAt,
				"rejection_reason": row.RejectionReason,
				"status":           row.Status,
				"source_role":      sourceRole,
				"source_id":        sourceID,
			},
		}
		if row.TripID != nil {
			item["trip"] = gin.H{"id": row.TripID.String(), "status": row.TripStatus}
		} else {
			item["trip"] = nil
		}
		items = append(items, item)
	}
	h.applyIsLikedToOfferListItems(c.Request.Context(), items, &cargoAPIViewer{DispatcherID: &dispatcherID})
	incomingCount, _ := h.repo.CountDispatcherSentOffers(c.Request.Context(), dispatcherID, status, "incoming", counterpartyID)
	outgoingCount, _ := h.repo.CountDispatcherSentOffers(c.Request.Context(), dispatcherID, status, "outgoing", counterpartyID)
	resp.OKLang(c, "sent_offers_listed", gin.H{
		"items":           items,
		"total":           total,
		"page":            page,
		"limit":           limit,
		"status":          status,
		"direction":       direction,
		"counterparty_id": counterpartyID,
		"counts": gin.H{
			"incoming": incomingCount,
			"outgoing": outgoingCount,
		},
	})
}

// ListOffersForDriver GET /v1/driver/offers/all
func (h *CargoHandler) ListOffersForDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	page := getIntQuery(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := getIntQuery(c, "limit", 30)
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	direction := strings.ToLower(strings.TrimSpace(c.DefaultQuery("direction", "all")))
	switch direction {
	case "all", "both":
		direction = "all"
	case "outgoing", "from_me", "sent", "by":
		direction = "outgoing"
	case "incoming", "to_me", "received":
		direction = "incoming"
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	var counterpartyID *uuid.UUID
	rawCounterparty := strings.TrimSpace(c.Query("counterparty_id"))
	if rawCounterparty == "" {
		if direction == "outgoing" {
			rawCounterparty = strings.TrimSpace(c.Query("to_dispatcher_id"))
		} else {
			rawCounterparty = strings.TrimSpace(c.Query("from_dispatcher_id"))
		}
	}
	if rawCounterparty != "" {
		id, err := uuid.Parse(rawCounterparty)
		if err != nil || id == uuid.Nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
			return
		}
		counterpartyID = &id
	}

	total, err := h.repo.CountDriverOffersAll(c.Request.Context(), driverID, status, direction, counterpartyID)
	if err != nil {
		h.logger.Error("driver offers all count", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}
	rows, err := h.repo.ListDriverOffersAll(c.Request.Context(), driverID, status, direction, counterpartyID, limit, offset)
	if err != nil {
		h.logger.Error("driver offers all list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		sourceRole := "DRIVER"
		sourceID := driverID.String()
		isIncomingRow := strings.EqualFold(strings.TrimSpace(row.ProposedBy), cargo.OfferProposedByDispatcher)
		if direction == "incoming" || (direction == "all" && isIncomingRow) {
			sourceRole = "CARGO_MANAGER"
			if row.CargoCreatedByType != nil && strings.EqualFold(strings.TrimSpace(*row.CargoCreatedByType), "driver") {
				sourceRole = "DRIVER_MANAGER"
			}
			if row.CargoCreatedByID != nil {
				sourceID = row.CargoCreatedByID.String()
			} else {
				sourceID = ""
			}
		}
		item := gin.H{
			"cargo_id": row.CargoID.String(),
			"cargo": gin.H{
				"id":                     row.CargoID.String(),
				"name":                   row.CargoName,
				"status":                 row.CargoStatus,
				"from_city_code":         row.CargoFromCityCode,
				"to_city_code":           row.CargoToCityCode,
				"vehicles_amount":        row.CargoVehiclesAmount,
				"vehicles_left":          row.CargoVehiclesLeft,
				"current_price":          row.CargoCurrentPrice,
				"current_price_currency": row.CargoCurrentCurrency,
			},
			"offer": gin.H{
				"id":               row.ID.String(),
				"proposed_by":      row.ProposedBy,
				"price":            row.Price,
				"invitation_price": row.Price,
				"currency":         row.Currency,
				"comment":          row.Comment,
				"created_at":       row.CreatedAt,
				"rejection_reason": row.RejectionReason,
				"status":           row.Status,
				"source_role":      sourceRole,
				"source_id":        sourceID,
			},
		}
		if row.CargoCreatedByType != nil && strings.EqualFold(strings.TrimSpace(*row.CargoCreatedByType), "dispatcher") && row.CargoCreatedByID != nil {
			itemOffer := item["offer"].(gin.H)
			itemOffer["cargo_manager_id"] = row.CargoCreatedByID.String()
		} else {
			itemOffer := item["offer"].(gin.H)
			itemOffer["cargo_manager_id"] = nil
		}
		if row.TripID != nil {
			item["trip"] = gin.H{"id": row.TripID.String(), "status": row.TripStatus}
		} else {
			item["trip"] = nil
		}
		items = append(items, item)
	}
	h.applyIsLikedToOfferListItems(c.Request.Context(), items, &cargoAPIViewer{DriverID: &driverID})
	incomingCount, _ := h.repo.CountDriverOffersAll(c.Request.Context(), driverID, status, "incoming", counterpartyID)
	outgoingCount, _ := h.repo.CountDriverOffersAll(c.Request.Context(), driverID, status, "outgoing", counterpartyID)
	resp.OKLang(c, "cargo_offers_listed", gin.H{
		"items":           items,
		"total":           total,
		"page":            page,
		"limit":           limit,
		"status":          status,
		"direction":       direction,
		"counterparty_id": counterpartyID,
		"counts": gin.H{
			"incoming": incomingCount,
			"outgoing": outgoingCount,
		},
	})
}

// GetOfferDriver GET /v1/driver/offers/:id — single offer for current driver with full cargo details.
func (h *CargoHandler) GetOfferDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil || offerID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	offer, err := h.repo.GetOfferByID(c.Request.Context(), offerID)
	if err != nil || offer == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found")
		return
	}
	if offer.CarrierID != driverID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
		return
	}
	h.respondSingleOfferWithCargo(c, offer)
}

// GetOfferDispatcher GET /v1/dispatchers/offers/:id — single offer for cargo manager with full cargo details.
func (h *CargoHandler) GetOfferDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}

	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil || offerID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	offer, err := h.repo.GetOfferByID(c.Request.Context(), offerID)
	if err != nil || offer == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found")
		return
	}
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), offer.CargoID, false)
	if !dispatcherCanAccessOfferForNegotiation(cargoObj, offer, dispatcherID, companyID) {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	h.respondSingleOfferWithCargo(c, offer)
}

func (h *CargoHandler) respondSingleOfferWithCargo(c *gin.Context, offer *cargo.Offer) {
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), offer.CargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	points, _ := h.repo.GetRoutePoints(c.Request.Context(), offer.CargoID)
	pay, _ := h.repo.GetPayment(c.Request.Context(), offer.CargoID)
	pb := strings.ToUpper(strings.TrimSpace(offer.ProposedBy))
	if pb == "" {
		pb = cargo.OfferProposedByDriver
	}

	cargoDetail := toCargoDetail(cargoObj, points, pay, nil)
	h.applyIsLikedToCargoMap(c.Request.Context(), cargoDetail, offer.CargoID, cargoViewerFromGin(c))
	offerOut := gin.H{
		"id":               offer.ID.String(),
		"cargo_id":         offer.CargoID.String(),
		"carrier_id":       offer.CarrierID.String(),
		"proposed_by":      pb,
		"price":            offer.Price,
		"invitation_price": offer.Price,
		"currency":         offer.Currency,
		"comment":          offer.Comment,
		"status":           offer.Status,
		"rejection_reason": offer.RejectionReason,
		"created_at":       offer.CreatedAt,
	}
	if offer.ProposedByID != nil {
		offerOut["proposed_by_id"] = offer.ProposedByID.String()
	}
	if offer.NegotiationDispatcherID != nil {
		offerOut["negotiation_dispatcher_id"] = offer.NegotiationDispatcherID.String()
	}
	out := gin.H{
		"offer": offerOut,
		"cargo": cargoDetail,
	}
	if h.tripsRepo != nil {
		if t, _ := h.tripsRepo.GetByOfferID(c.Request.Context(), offer.ID); t != nil {
			out["trip"] = toTripResp(t)
		} else {
			out["trip"] = nil
		}
	}
	resp.OKLang(c, "ok", out)
}

// DriverCreateOffer POST /v1/driver/cargo/:id/offers — водитель предлагает свою цену (proposed_by=DRIVER).
func (h *CargoHandler) DriverCreateOffer(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	obj, _ := h.repo.GetByID(c.Request.Context(), id, false)
	if obj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if !cargo.IsSearching(obj.Status) {
		resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
		return
	}
	var req struct {
		Price    float64 `json:"price" binding:"required"`
		Currency string  `json:"currency" binding:"required"`
		Comment  string  `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if req.Price <= 0 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_price")
		return
	}
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if len(req.Currency) < 3 || len(req.Currency) > 5 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_currency")
		return
	}

	if obj.Status == cargo.StatusSearchingCompany {
		if obj.CompanyID == nil {
			resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
			return
		}
		if h.drivers != nil {
			if drv, _ := h.drivers.FindByID(c.Request.Context(), driverID); drv == nil || drv.CompanyID == nil || *drv.CompanyID != obj.CompanyID.String() {
				resp.ErrorLang(c, http.StatusForbidden, "cargo_visible_only_to_company_drivers")
				return
			}
		}
	}
	offerID, err := h.repo.CreateOffer(c.Request.Context(), id, driverID, req.Price, req.Currency, req.Comment, cargo.OfferProposedByDriver, &driverID)
	if err != nil {
		if err == cargo.ErrCargoNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
			return
		}
		if err == cargo.ErrOfferPriceOutOfRange {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{
				"reason": "offer_price_out_of_range",
			})
			return
		}
		if err == cargo.ErrCargoSlotsFull {
			resp.ErrorWithDataLang(c, http.StatusConflict, "cargo_slots_full", gin.H{
				"cargo_id":        id.String(),
				"vehicles_left":   obj.VehiclesLeft,
				"required_left":   0,
				"explanation_key": "cargo_slots_full",
			})
			return
		}
		if err == cargo.ErrCargoNotSearching {
			resp.ErrorLang(c, http.StatusBadRequest, "cargo_not_searching")
			return
		}
		if err == cargo.ErrDriverOfferAlreadyExists {
			resp.ErrorLang(c, http.StatusConflict, "driver_offer_already_exists")
			return
		}
		if err == cargo.ErrDriverBusy {
			resp.ErrorLang(c, http.StatusConflict, "driver_busy_with_another_cargo")
			return
		}
		h.logger.Error("driver create offer", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_offer")
		return
	}
	if h.stream != nil {
		if disp := tripNotifyDispatcherID(obj); disp != nil && *disp != uuid.Nil {
			h.stream.PublishNotification(tripnotif.RecipientDispatcher, *disp, gin.H{
				"kind":       "cargo_offer",
				"event":      "cargo_offer_created",
				"direction":  "incoming",
				"offer_id":   offerID.String(),
				"cargo_id":   id.String(),
				"driver_id":  driverID.String(),
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
	}
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"id": offerID.String(), "proposed_by": cargo.OfferProposedByDriver})
}

func (h *CargoHandler) ListOffers(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}

	dirRaw := strings.TrimSpace(c.Query("direction"))
	direction := ""
	if dirRaw != "" {
		direction = strings.ToLower(dirRaw)
		switch direction {
		case "outgoing", "from_me", "sent", "by":
			direction = "outgoing"
		case "incoming", "to_me", "received":
			direction = "incoming"
		default:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
	}

	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	if status != "" {
		switch status {
		case "PENDING", "ACCEPTED", "REJECTED":
		default:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
	}

	var counterpartyID *uuid.UUID
	rawCounterparty := strings.TrimSpace(c.Query("counterparty_id"))
	if rawCounterparty == "" {
		if direction == "outgoing" {
			rawCounterparty = strings.TrimSpace(c.Query("to_driver_id"))
		} else if direction == "incoming" {
			rawCounterparty = strings.TrimSpace(c.Query("from_driver_id"))
		} else {
			rawCounterparty = strings.TrimSpace(c.Query("to_driver_id"))
			if rawCounterparty == "" {
				rawCounterparty = strings.TrimSpace(c.Query("from_driver_id"))
			}
		}
	}
	if rawCounterparty != "" {
		cp, perr := uuid.Parse(rawCounterparty)
		if perr != nil || cp == uuid.Nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
			return
		}
		counterpartyID = &cp
	}

	offers, err := h.repo.GetOffersFiltered(c.Request.Context(), id, direction, status, counterpartyID)
	if err != nil {
		if strings.Contains(err.Error(), "invalid offers direction") {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		h.logger.Error("cargo list offers", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_offers")
		return
	}

	cargoObj, _ := h.repo.GetByID(c.Request.Context(), id, false)
	cargoMini := gin.H(nil)
	if cargoObj != nil {
		cargoMini = gin.H{
			"id":              cargoObj.ID.String(),
			"cargo_type_name": firstNonEmptyStr(cargoObj.CargoTypeNameRU, cargoObj.CargoTypeNameUZ, cargoObj.CargoTypeNameEN, cargoObj.CargoTypeNameTR, cargoObj.CargoTypeNameZH),
			"weight":          cargoObj.Weight,
			"volume":          cargoObj.Volume,
		}
		if v := parseCargoAPIViewer(c, h.jwtm); v != nil {
			if flags := h.cargoLikedFlags(c.Request.Context(), v, []uuid.UUID{id}); flags != nil {
				cargoMini["is_liked"] = flags[id]
			}
		}
	}
	items := make([]gin.H, 0, len(offers))
	for _, o := range offers {
		item := toOfferList([]cargo.Offer{o})[0]
		if cargoMini != nil {
			item["cargo"] = cargoMini
		}
		if h.drivers != nil {
			if drv, _ := h.drivers.FindByID(c.Request.Context(), o.CarrierID); drv != nil {
				item["driver"] = gin.H{
					"id":    drv.ID,
					"phone": drv.Phone,
					"name":  drv.Name,
				}
			}
		}
		items = append(items, item)
	}
	payload := gin.H{
		"items":           items,
		"status":          status,
		"counterparty_id": counterpartyID,
	}
	if direction != "" {
		payload["direction"] = direction
	}
	resp.OKLang(c, "ok", payload)
}

// ListDriverManagerOffers GET /v1/dispatchers/cargo-offers — список офферов для менеджера водителей.
func (h *CargoHandler) ListDriverManagerOffers(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)

	page := getIntQuery(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := getIntQuery(c, "limit", 30)
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	total, err := h.repo.CountDriverManagerOffers(c.Request.Context(), dispatcherID)
	if err != nil {
		h.logger.Error("manager cargo offers count", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}

	rows, err := h.repo.ListDriverManagerOffers(c.Request.Context(), dispatcherID, limit, offset)
	if err != nil {
		h.logger.Error("manager cargo offers list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		state := strings.ToLower(row.Status)
		items = append(items, gin.H{
			"id":               row.ID,
			"cargo_id":         row.CargoID,
			"carrier_id":       row.CarrierID,
			"price":            row.Price,
			"currency":         row.Currency,
			"comment":          row.Comment,
			"proposed_by":      row.ProposedBy,
			"status":           row.Status,
			"state":            state,
			"rejection_reason": row.RejectionReason,
			"created_at":       row.CreatedAt,
			"cargo": gin.H{
				"status":          row.CargoStatus,
				"name":            row.CargoName,
				"weight":          row.CargoWeight,
				"volume":          row.CargoVolume,
				"truck_type":      row.CargoTruckType,
				"vehicles_amount": row.CargoVehiclesAmount,
				"vehicles_left":   row.CargoVehiclesLeft,
				"from_city_code":  row.CargoFromCityCode,
				"to_city_code":    row.CargoToCityCode,
			},
			"trip_id":     row.TripID,
			"trip_status": row.TripStatus,
		})
	}

	resp.OKLang(c, "ok", gin.H{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}


// ListMyCargoOffers lists driver offers (requests) grouped by bucket:
// sent (PENDING), accepted (ACCEPTED, not completed), completed (ACCEPTED + cargo COMPLETED), rejected (REJECTED), canceled (CANCELED).
// Endpoint: GET /v1/driver/cargo-offers?bucket=...
func (h *CargoHandler) ListMyCargoOffers(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)

	bucket := strings.ToLower(strings.TrimSpace(c.Query("bucket")))
	if bucket == "" {
		bucket = "sent"
	}
	switch bucket {
	case "sent", "accepted", "completed", "rejected", "canceled", "cancelled":
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_cargo_offer_bucket")
		return
	}

	page := getIntQuery(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := getIntQuery(c, "limit", 30)
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	total, err := h.repo.CountDriverCargoOffersByBucket(c.Request.Context(), driverID, bucket)
	if err != nil {
		h.logger.Error("driver cargo offers count", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}

	rows, err := h.repo.ListDriverCargoOffersByBucket(c.Request.Context(), driverID, bucket, limit, offset)
	if err != nil {
		h.logger.Error("driver cargo offers list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		step := ""
		state := "pending"
		sourceRole := "DRIVER"
		var sourceID any = row.CarrierID.String()
		proposedBy := strings.ToUpper(strings.TrimSpace(row.ProposedBy))
		cargoOwnerType := ""
		if row.CargoCreatedByType != nil {
			cargoOwnerType = strings.ToUpper(strings.TrimSpace(*row.CargoCreatedByType))
		}
		if proposedBy == cargo.OfferProposedByDispatcher {
			sourceID = nil
			if row.CargoCreatedByID != nil {
				sourceID = row.CargoCreatedByID.String()
			}
			if cargoOwnerType == "DISPATCHER" {
				sourceRole = "CARGO_MANAGER"
			} else {
				sourceRole = "DRIVER_MANAGER"
			}
		}
		switch strings.ToUpper(strings.TrimSpace(row.Status)) {
		case "PENDING":
			if proposedBy == cargo.OfferProposedByDispatcher {
				step = "WAITING_DRIVER_RESPONSE"
			} else if cargoOwnerType == "DISPATCHER" {
				step = "WAITING_CARGO_MANAGER_RESPONSE"
			} else {
				step = "WAITING_DRIVER_MANAGER_RESPONSE"
			}
		case "ACCEPTED":
			if row.TripID != nil {
				state = "active"
				step = "ACTIVE_TRIP"
			} else {
				state = "accepted"
				step = "ACCEPTED"
			}
		case "REJECTED":
			state = "rejected"
			step = "REJECTED"
		case "CANCELED":
			state = "canceled"
			step = "CANCELED"
		}
		item := gin.H{
			"cargo_id": row.CargoID.String(),
			"cargo": gin.H{
				"id":                     row.CargoID.String(),
				"name":                   row.CargoName,
				"status":                 row.CargoStatus,
				"from_city_code":         row.CargoFromCityCode,
				"to_city_code":           row.CargoToCityCode,
				"weight":                 row.CargoWeight,
				"volume":                 row.CargoVolume,
				"truck_type":             row.CargoTruckType,
				"vehicles_amount":        row.CargoVehiclesAmount,
				"vehicles_left":          row.CargoVehiclesLeft,
				"current_price":          row.CargoCurrentPrice,
				"current_price_currency": row.CargoCurrentCurrency,
			},
			"offer": gin.H{
				"id":               row.ID.String(),
				"proposed_by":      row.ProposedBy,
				"status":           row.Status,
				"price":            row.Price,
				"invitation_price": row.Price,
				"currency":         row.Currency,
				"comment":          row.Comment,
				"created_at":       row.CreatedAt,
				"rejection_reason": row.RejectionReason,
				"state":            state,
				"step":             step,
				"source_role":      sourceRole,
				"source_id":        sourceID,
			},
		}

		if row.TripID != nil {
			item["trip"] = gin.H{
				"id":     row.TripID.String(),
				"status": row.TripStatus,
			}
		} else {
			item["trip"] = nil
		}

		items = append(items, item)
	}
	h.applyIsLikedToOfferListItems(c.Request.Context(), items, &cargoAPIViewer{DriverID: &driverID})

	resp.OKLang(c, "cargo_offers_listed", gin.H{
		"items":  items,
		"total":  total,
		"page":   page,
		"limit":  limit,
		"bucket": bucket,
	})
}

// AcceptOffer POST …/offers/:id/accept — принимает оффер **только** по UUID оффера в пути (`id` = offer_id).
// Тело запроса и query-параметры для выбора оффера не используются; cargo_id/trip_id извлекаются из найденной строки оффера.
func (h *CargoHandler) AcceptOffer(c *gin.Context) {
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken))
	if raw == "" || h.jwtm == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "missing_user_token")
		return
	}
	userID, role, err := h.jwtm.ParseAccess(raw)
	if err != nil || userID == uuid.Nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "invalid_user_token")
		return
	}
	offer, err := h.repo.GetOfferByID(c.Request.Context(), offerID)
	if err != nil || offer == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found_or_not_pending")
		return
	}
	if offer.Status != "PENDING" {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found_or_not_pending")
		return
	}
	proposedBy := offer.ProposedBy
	if proposedBy == "" {
		proposedBy = cargo.OfferProposedByDriver
	}
	proposedBy = strings.ToUpper(strings.TrimSpace(proposedBy))
	tripFlow := offerTripFlowFromOffer(offer)
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), offer.CargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	switch proposedBy {
	case cargo.OfferProposedByDriver:
		if role != "dispatcher" {
			resp.ErrorLang(c, http.StatusForbidden, "only_dispatcher_accepts_driver_offer")
			return
		}
		var dispCompanyID *uuid.UUID
		if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
			if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
				dispCompanyID = &u
			}
		}
		if !dispatcherOwnsCargoForNegotiation(cargoObj, userID, dispCompanyID) {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}
	case cargo.OfferProposedByDriverManager:
		if role != "dispatcher" {
			resp.ErrorLang(c, http.StatusForbidden, "only_dispatcher_accepts_driver_offer")
			return
		}
		var dispCompanyID *uuid.UUID
		if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
			if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
				dispCompanyID = &u
			}
		}
		if !dispatcherOwnsCargoForNegotiation(cargoObj, userID, dispCompanyID) {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
			return
		}

		// SPECIAL CASE: Cargo manager accepts Driver Manager's offer.
		// Move to WAITING_DRIVER_CONFIRM, don't create trip yet.
		negID := offer.ProposedByID
		if negID == nil || *negID == uuid.Nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		if err := h.repo.SetOfferStatusWaitingDriver(c.Request.Context(), offerID, negID); err != nil {
			if errors.Is(err, cargo.ErrOfferNotFoundOrNotPending) {
				resp.ErrorLang(c, http.StatusNotFound, "offer_not_found_or_not_pending")
				return
			}
			h.logger.Error("set offer waiting driver confirm", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}

		if h.stream != nil {
			p := gin.H{
				"kind":       "cargo_offer",
				"event":      "cargo_offer_waiting_driver",
				"offer_id":   offerID.String(),
				"cargo_id":   offer.CargoID.String(),
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			}
			h.stream.PublishNotification(tripnotif.RecipientDriver, offer.CarrierID, p)
			h.stream.PublishNotification(tripnotif.RecipientDispatcher, *negID, p)
		}
		resp.OKLang(c, "waiting_driver_confirmation", gin.H{
			"status":   "waiting_driver_confirm",
			"offer_id": offerID.String(),
		})
		return

	case cargo.OfferProposedByDispatcher:
		// Cargo Manager (DISPATCHER) proposed price TO Driver (CarrierID).
		// Either the driver personally accepts it, or their Manager accepts it.
		if role == "driver" {
			if userID != offer.CarrierID {
				resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
				return
			}
			// Driver accepts personally -> proceed to AcceptOffer (Trip creation).
		} else if role == "dispatcher" {
			// Check if this dispatcher is a manager of this driver.
			isManager, _ := h.drivers.IsLinked(c.Request.Context(), offer.CarrierID, userID)
			if !isManager {
				resp.ErrorLang(c, http.StatusForbidden, "only_driver_or_manager_accepts_dispatcher_offer")
				return
			}

			// Manager accepts Cargo Manager's offer. Move to WAITING_DRIVER_CONFIRM.
			if err := h.repo.SetOfferStatusWaitingDriver(c.Request.Context(), offerID, &userID); err != nil {
				if errors.Is(err, cargo.ErrOfferNotFoundOrNotPending) {
					resp.ErrorLang(c, http.StatusNotFound, "offer_not_found_or_not_pending")
					return
				}
				h.logger.Error("manager accept dispatcher offer", zap.Error(err))
				resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
				return
			}

			if h.stream != nil {
				p := gin.H{
					"kind":       "cargo_offer",
					"event":      "cargo_offer_waiting_driver",
					"offer_id":   offerID.String(),
					"cargo_id":   offer.CargoID.String(),
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				}
				h.stream.PublishNotification(tripnotif.RecipientDriver, offer.CarrierID, p)
				if disp := tripNotifyDispatcherID(cargoObj); disp != nil {
					h.stream.PublishNotification(tripnotif.RecipientDispatcher, *disp, p)
				}
			}
			resp.OKLang(c, "waiting_driver_confirmation", gin.H{
				"status":   "waiting_driver_confirm",
				"offer_id": offerID.String(),
			})
			return
		} else {
			resp.ErrorLang(c, http.StatusForbidden, "forbidden")
			return
		}
	default:
		h.logger.Error("accept offer invalid proposed_by",
			zap.String("offer_id", offerID.String()),
			zap.String("proposed_by", offer.ProposedBy),
		)
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	cargoID, carrierID, err := h.repo.AcceptOffer(c.Request.Context(), offerID)
	agreedPrice := offer.Price
	agreedCurrency := strings.ToUpper(strings.TrimSpace(offer.Currency))
	if err != nil {
		h.logger.Error("accept offer failed",
			zap.Error(err),
			zap.String("offer_id", offerID.String()),
			zap.String("role", role),
			zap.String("proposed_by", proposedBy),
			zap.String("carrier_id", offer.CarrierID.String()),
		)
		offerIDStr := offerID.String()
		switch {
		case errors.Is(err, cargo.ErrOfferNotFoundOrNotPending):
			resp.ErrorWithDataLang(c, http.StatusNotFound, "offer_not_found_or_not_pending", gin.H{"reason": "offer_not_pending", "offer_id": offerIDStr})
		case errors.Is(err, cargo.ErrCargoSlotsFull):
			resp.ErrorWithDataLang(c, http.StatusConflict, "cargo_slots_full", gin.H{"reason": "cargo_slots_full", "offer_id": offerIDStr, "cargo_id": offer.CargoID.String()})
		case errors.Is(err, cargo.ErrDriverBusy):
			resp.ErrorWithDataLang(c, http.StatusConflict, "driver_busy_with_another_cargo", gin.H{"reason": "driver_busy", "offer_id": offerIDStr, "driver_id": offer.CarrierID.String()})
		case errors.Is(err, cargo.ErrCargoNotSearching):
			resp.ErrorWithDataLang(c, http.StatusConflict, "cargo_not_searching", gin.H{"reason": "cargo_not_searching", "offer_id": offerIDStr, "cargo_id": offer.CargoID.String()})
		case isDBConflict(err):
			resp.ErrorWithDataLang(c, http.StatusConflict, "driver_busy_with_another_cargo", gin.H{"reason": "driver_cargo_link_conflict", "offer_id": offerIDStr})
		case isTransactionAborted(err):
			resp.ErrorWithDataLang(c, http.StatusInternalServerError, "failed_to_accept", gin.H{"reason": "transaction_aborted", "offer_id": offerIDStr})
		default:
			resp.ErrorWithDataLang(c, http.StatusInternalServerError, "failed_to_accept", gin.H{"reason": "accept_failed", "offer_id": offerIDStr})
		}
		return
	}
	if h.tripsRepo != nil {
		tripID, tripErr := h.tripsRepo.Create(c.Request.Context(), cargoID, offerID, carrierID, agreedPrice, agreedCurrency)
		if tripErr != nil {
			if errors.Is(tripErr, trips.ErrAgreedPriceOutOfRange) {
				resp.ErrorWithDataLang(c, http.StatusBadRequest, "invalid_payload_detail", gin.H{
					"reason":   "agreed_price_out_of_range",
					"offer_id": offerID.String(),
					"cargo_id": cargoID.String(),
				})
				return
			}
			h.logger.Error("accept offer: trip create failed",
				zap.Error(tripErr),
				zap.String("offer_id", offerID.String()),
				zap.String("cargo_id", cargoID.String()),
			)
			resp.ErrorWithDataLang(c, http.StatusInternalServerError, "failed_to_accept", gin.H{
				"reason":   "trip_create_failed",
				"offer_id": offerID.String(),
				"cargo_id": cargoID.String(),
			})
			return
		}
		if tripID != uuid.Nil {
			if h.stream != nil && h.tripsRepo != nil {
				if tr, err := h.tripsRepo.GetByID(c.Request.Context(), tripID); err == nil && tr != nil {
					cg, _ := h.repo.GetByID(c.Request.Context(), cargoID, false)
					PublishTripStatusForCargoParticipants(h.stream, tr, cg, offer)
				}
			}
			resp.OKLang(c, "ok", gin.H{
				"cargo_id":        cargoID.String(),
				"offer_id":        offerID.String(),
				"trip_id":         tripID.String(),
				"driver_id":       carrierID.String(),
				"status":          "accepted",
				"trip_flow":       tripFlow,
				"agreed_price":    agreedPrice,
				"agreed_currency": agreedCurrency,
			})
			if h.stream != nil {
				recipientKind := tripnotif.RecipientDispatcher
				recipientID := userID
				if role == "dispatcher" {
					recipientKind = tripnotif.RecipientDriver
					recipientID = carrierID
				}
				h.stream.PublishNotification(recipientKind, recipientID, gin.H{
					"kind":       "cargo_offer",
					"event":      "cargo_offer_accepted",
					"offer_id":   offerID.String(),
					"cargo_id":   cargoID.String(),
					"driver_id":  carrierID.String(),
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				})
			}
			return
		}
	}
	if h.stream != nil {
		recipientKind := tripnotif.RecipientDispatcher
		recipientID := userID
		if role == "dispatcher" {
			recipientKind = tripnotif.RecipientDriver
			recipientID = carrierID
		}
		h.stream.PublishNotification(recipientKind, recipientID, gin.H{
			"kind":       "cargo_offer",
			"event":      "cargo_offer_accepted",
			"offer_id":   offerID.String(),
			"cargo_id":   cargoID.String(),
			"driver_id":  carrierID.String(),
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	resp.OKLang(c, "ok", gin.H{
		"cargo_id":        cargoID.String(),
		"offer_id":        offerID.String(),
		"status":          "accepted",
		"trip_flow":       tripFlow,
		"agreed_price":    agreedPrice,
		"agreed_currency": agreedCurrency,
	})
}

// DriverConfirmOffer POST /v1/driver/offers/:id/confirm — водитель подтверждает сделку, принятую менеджером.
func (h *CargoHandler) DriverConfirmOffer(c *gin.Context) {
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)

	offer, err := h.repo.GetOfferByID(c.Request.Context(), offerID)
	if err != nil || offer == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found")
		return
	}
	if offer.Status != cargo.OfferStatusWaitingDriverConfirm {
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_waiting_driver_confirm")
		return
	}
	if offer.CarrierID != driverID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
		return
	}
	tripFlow := offerTripFlowFromOffer(offer)

	tx, err := h.repo.BeginTx(c.Request.Context())
	if err != nil {
		h.logger.Error("driver confirm offer begin tx", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	defer tx.Rollback(c.Request.Context())

	cargoID, carrierID, err := h.repo.AcceptOfferTx(c.Request.Context(), tx, offerID)
	if err != nil {
		h.logger.Error("driver confirm offer accept tx", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_accept")
		return
	}

	agreedPrice := offer.Price
	agreedCurrency := strings.ToUpper(strings.TrimSpace(offer.Currency))

	if h.tripsRepo != nil {
		tripID, tripErr := h.tripsRepo.CreateTx(c.Request.Context(), tx, cargoID, offerID, carrierID, agreedPrice, agreedCurrency)
		if tripErr != nil {
			h.logger.Error("driver confirm offer trip create", zap.Error(tripErr))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_accept")
			return
		}

		if err := tx.Commit(c.Request.Context()); err != nil {
			h.logger.Error("driver confirm offer commit", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}

		if h.stream != nil {
			if tr, err := h.tripsRepo.GetByID(c.Request.Context(), tripID); err == nil && tr != nil {
				cg, _ := h.repo.GetByID(c.Request.Context(), cargoID, false)
				PublishTripStatusForCargoParticipants(h.stream, tr, cg, offer)
			}
			// Notify Cargo Manager and Driver Manager (same SSE channel for dispatchers)
			if cargoObj, _ := h.repo.GetByID(c.Request.Context(), cargoID, false); cargoObj != nil {
				acc := gin.H{
					"kind":       "cargo_offer",
					"event":      "cargo_offer_accepted",
					"offer_id":   offerID.String(),
					"cargo_id":   cargoID.String(),
					"driver_id":  carrierID.String(),
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				}
				var cmID *uuid.UUID
				if disp := tripNotifyDispatcherID(cargoObj); disp != nil {
					cmID = disp
					h.stream.PublishNotification(tripnotif.RecipientDispatcher, *disp, acc)
				}
				if dm := offerDriverManagerDispatcherID(offer); dm != nil && (cmID == nil || *dm != *cmID) {
					h.stream.PublishNotification(tripnotif.RecipientDispatcher, *dm, acc)
				}
			}
		}

		resp.OKLang(c, "ok", gin.H{
			"trip_id":   tripID.String(),
			"status":    "accepted",
			"trip_flow": tripFlow,
		})
		return
	}

	_ = tx.Commit(c.Request.Context())
	resp.OKLang(c, "ok", nil)
}

func offerTripFlowFromOffer(o *cargo.Offer) string {
	if o == nil {
		return "direct"
	}
	if strings.EqualFold(strings.TrimSpace(o.ProposedBy), cargo.OfferProposedByDriverManager) {
		return "via_driver_manager"
	}
	if o.NegotiationDispatcherID != nil && *o.NegotiationDispatcherID != uuid.Nil {
		return "via_driver_manager"
	}
	return "direct"
}

// offerDriverManagerDispatcherID returns the driver-manager dispatcher involved in negotiation (WAITING_DRIVER_CONFIRM / ratings).
func offerDriverManagerDispatcherID(o *cargo.Offer) *uuid.UUID {
	if o == nil {
		return nil
	}
	if o.NegotiationDispatcherID != nil && *o.NegotiationDispatcherID != uuid.Nil {
		return o.NegotiationDispatcherID
	}
	if strings.EqualFold(strings.TrimSpace(o.ProposedBy), cargo.OfferProposedByDriverManager) && o.ProposedByID != nil && *o.ProposedByID != uuid.Nil {
		return o.ProposedByID
	}
	return nil
}

func (h *CargoHandler) publishCargoOfferEndedNotifs(c *gin.Context, offer *cargo.Offer, cargoObj *cargo.Cargo, event string) {
	if h.stream == nil || offer == nil {
		return
	}
	if event == "" {
		event = "cargo_offer_rejected"
	}
	payload := gin.H{
		"kind":       "cargo_offer",
		"event":      event,
		"offer_id":   offer.ID.String(),
		"cargo_id":   offer.CargoID.String(),
		"driver_id":  offer.CarrierID.String(),
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	h.stream.PublishNotification(tripnotif.RecipientDriver, offer.CarrierID, payload)
	if disp := tripNotifyDispatcherID(cargoObj); disp != nil && *disp != uuid.Nil {
		h.stream.PublishNotification(tripnotif.RecipientDispatcher, *disp, payload)
	}
	if dm := offerDriverManagerDispatcherID(offer); dm != nil {
		if disp := tripNotifyDispatcherID(cargoObj); disp == nil || *dm != *disp {
			h.stream.PublishNotification(tripnotif.RecipientDispatcher, *dm, payload)
		}
	}
}

func isDBConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" || strings.HasPrefix(pgErr.Code, "23")
}

func isTransactionAborted(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "25P02"
}

// RejectOfferReq body for POST .../offers/:id/reject — reason is mandatory.
type RejectOfferReq struct {
	Reason string `json:"reason" binding:"required"`
}

var reasonMinWordRe = regexp.MustCompile(`\pL{3,}`)

func isValidRejectReason(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 3 {
		return false
	}
	return reasonMinWordRe.MatchString(trimmed)
}

// RejectOfferDispatcher POST .../offers/:id/reject — отклонить входящий оффер водителя или отозвать свой исходящий водителю (proposed_by=DISPATCHER) по своему грузу. Reason required.
func (h *CargoHandler) RejectOfferDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	offer, err := h.repo.GetOfferByID(c.Request.Context(), offerID)
	if err != nil || offer == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found")
		return
	}
	pb := strings.ToUpper(strings.TrimSpace(offer.ProposedBy))
	if pb == "" {
		pb = cargo.OfferProposedByDriver
	}
	if pb != cargo.OfferProposedByDriver && pb != cargo.OfferProposedByDispatcher && pb != cargo.OfferProposedByDriverManager {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), offer.CargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	ownsCargo := dispatcherOwnsCargoForNegotiation(cargoObj, dispatcherID, companyID)
	isNegotiator := offer.NegotiationDispatcherID != nil && *offer.NegotiationDispatcherID == dispatcherID
	isOfferAuthor := offer.ProposedByID != nil && *offer.ProposedByID == dispatcherID &&
		(pb == cargo.OfferProposedByDispatcher || pb == cargo.OfferProposedByDriverManager)
	canReject := ownsCargo || isOfferAuthor || (offer.Status == cargo.OfferStatusWaitingDriverConfirm && isNegotiator)
	if !canReject {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	if offer.Status != "PENDING" && offer.Status != cargo.OfferStatusWaitingDriverConfirm {
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_found_or_not_pending")
		return
	}
	var req RejectOfferReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_required")
		return
	}
	if !isValidRejectReason(req.Reason) {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_too_short")
		return
	}
	cancelOwn := false
	switch offer.Status {
	case "PENDING":
		if isOfferAuthor {
			cancelOwn = true
		}
	case cargo.OfferStatusWaitingDriverConfirm:
		if ownsCargo || isNegotiator {
			cancelOwn = true
		}
	}
	var endErr error
	if cancelOwn {
		endErr = h.repo.CancelOffer(c.Request.Context(), offerID, req.Reason)
	} else {
		endErr = h.repo.RejectOffer(c.Request.Context(), offerID, req.Reason)
	}
	if endErr != nil {
		if errors.Is(endErr, cargo.ErrRejectionReasonRequired) {
			resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_required")
			return
		}
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_found_or_not_pending")
		return
	}
	if cancelOwn {
		h.publishCargoOfferEndedNotifs(c, offer, cargoObj, "cargo_offer_canceled")
		resp.OKLang(c, "ok", gin.H{"status": "CANCELED"})
	} else {
		h.publishCargoOfferEndedNotifs(c, offer, cargoObj, "cargo_offer_rejected")
		resp.OKLang(c, "ok", gin.H{"status": "REJECTED"})
	}
}

// RejectOfferDriver POST /v1/driver/offers/:id/reject — отклонить входящий оффер диспетчера или отозвать свой исходящий (proposed_by=DRIVER); reason required.
func (h *CargoHandler) RejectOfferDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	offer, err := h.repo.GetOfferByID(c.Request.Context(), offerID)
	if err != nil || offer == nil {
		resp.ErrorLang(c, http.StatusNotFound, "offer_not_found")
		return
	}
	if offer.CarrierID != driverID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
		return
	}
	pb := strings.ToUpper(strings.TrimSpace(offer.ProposedBy))
	if pb == "" {
		pb = cargo.OfferProposedByDriver
	}
	switch offer.Status {
	case cargo.OfferStatusWaitingDriverConfirm:
		if pb != cargo.OfferProposedByDispatcher && pb != cargo.OfferProposedByDriverManager {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
			return
		}
	case "PENDING":
		if pb != cargo.OfferProposedByDispatcher && pb != cargo.OfferProposedByDriver {
			resp.ErrorLang(c, http.StatusForbidden, "not_your_offer")
			return
		}
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_found_or_not_pending")
		return
	}
	var req RejectOfferReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_required")
		return
	}
	if !isValidRejectReason(req.Reason) {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_too_short")
		return
	}
	cancelOwn := offer.Status == "PENDING" && pb == cargo.OfferProposedByDriver
	var endErr error
	if cancelOwn {
		endErr = h.repo.CancelOffer(c.Request.Context(), offerID, req.Reason)
	} else {
		endErr = h.repo.RejectOffer(c.Request.Context(), offerID, req.Reason)
	}
	if endErr != nil {
		if errors.Is(endErr, cargo.ErrRejectionReasonRequired) {
			resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_required")
			return
		}
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_found_or_not_pending")
		return
	}
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), offer.CargoID, false)
	if cancelOwn {
		h.publishCargoOfferEndedNotifs(c, offer, cargoObj, "cargo_offer_canceled")
		resp.OKLang(c, "ok", gin.H{"status": "CANCELED"})
	} else {
		h.publishCargoOfferEndedNotifs(c, offer, cargoObj, "cargo_offer_rejected")
		resp.OKLang(c, "ok", gin.H{"status": "REJECTED"})
	}
}

// GetDriverOfferInvitationStats GET /v1/driver/cargo-invitation-stats — counts for current driver (all cargos).
func (h *CargoHandler) GetDriverOfferInvitationStats(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	s, err := h.repo.GetDriverOfferInvitationStats(c.Request.Context(), driverID)
	if err != nil {
		h.logger.Error("driver offer invitation stats", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}
	resp.OKLang(c, "ok", gin.H{
		"invitations_received_total":   s.OutgoingTotal, // DISPATCHER → driver
		"invitations_received_pending": s.OutgoingPending,
		"invitations_sent_total":       s.IncomingTotal, // DRIVER → cargo
		"invitations_sent_pending":     s.IncomingPending,
	})
}

func dispatcherOwnsCargoForNegotiation(cargoObj *cargo.Cargo, dispatcherID uuid.UUID, companyID *uuid.UUID) bool {
	if cargoObj == nil {
		return false
	}
	if cargoObj.CreatedByType != nil && strings.EqualFold(*cargoObj.CreatedByType, "DISPATCHER") &&
		cargoObj.CreatedByID != nil && *cargoObj.CreatedByID == dispatcherID {
		return true
	}
	if cargoObj.CreatedByType != nil && strings.EqualFold(*cargoObj.CreatedByType, "COMPANY") &&
		cargoObj.CompanyID != nil && companyID != nil && *cargoObj.CompanyID == *companyID {
		return true
	}
	return false
}

// dispatcherCanAccessOfferForNegotiation: cargo owner/company OR driver manager who proposed (DRIVER_MANAGER) OR dispatcher recorded on offer (negotiation / accept chain).
func dispatcherCanAccessOfferForNegotiation(cargoObj *cargo.Cargo, offer *cargo.Offer, dispatcherID uuid.UUID, companyID *uuid.UUID) bool {
	if offer == nil {
		return false
	}
	if dispatcherOwnsCargoForNegotiation(cargoObj, dispatcherID, companyID) {
		return true
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

// ListCargoNegotiation GET /v1/dispatchers/cargo/:id/negotiation — офферы по грузу + привязанные рейсы (договорная цена на рейсе, кто предложил/статус).
func (h *CargoHandler) ListCargoNegotiation(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var companyID *uuid.UUID
	if cid, ok := c.Get(mw.CtxDispatcherCompanyID); ok {
		if u, ok2 := cid.(uuid.UUID); ok2 && u != uuid.Nil {
			companyID = &u
		}
	}
	cargoID, err := uuid.Parse(c.Param("id"))
	if err != nil || cargoID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), cargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if !dispatcherOwnsCargoForNegotiation(cargoObj, dispatcherID, companyID) {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	offers, err := h.repo.GetOffers(c.Request.Context(), cargoID)
	if err != nil {
		h.logger.Error("cargo negotiation list offers", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_cargo_offers")
		return
	}
	tripByOffer := make(map[uuid.UUID]*trips.Trip)
	if h.tripsRepo != nil {
		tripList, err := h.tripsRepo.ListByCargoID(c.Request.Context(), cargoID)
		if err != nil {
			h.logger.Error("cargo negotiation list trips", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
			return
		}
		for i := range tripList {
			t := &tripList[i]
			tripByOffer[t.OfferID] = t
		}
	}
	items := make([]gin.H, 0, len(offers))
	for i := range offers {
		o := &offers[i]
		item := gin.H{
			"offer_id":    o.ID.String(),
			"cargo_id":    o.CargoID.String(),
			"carrier_id":  o.CarrierID.String(),
			"status":      o.Status,
			"proposed_by": o.ProposedBy,
			"price":       o.Price,
			"currency":    o.Currency,
			"created_at":  o.CreatedAt,
		}
		if o.Comment != nil && strings.TrimSpace(*o.Comment) != "" {
			item["comment"] = *o.Comment
		}
		if o.RejectionReason != nil && strings.TrimSpace(*o.RejectionReason) != "" {
			item["rejection_reason"] = *o.RejectionReason
		}
		if o.ProposedBy == cargo.OfferProposedByDriver {
			item["waits_accept_from"] = "DISPATCHER"
		} else {
			item["waits_accept_from"] = "DRIVER"
		}
		if t := tripByOffer[o.ID]; t != nil {
			item["trip_id"] = t.ID.String()
			item["trip_status"] = t.Status
			item["agreed_price"] = t.AgreedPrice
			item["agreed_currency"] = t.AgreedCurrency
			if t.DriverID != nil {
				item["driver_id"] = t.DriverID.String()
			}
		}
		items = append(items, item)
	}
	out := gin.H{"items": items, "cargo_id": cargoID.String()}
	if pay, _ := h.repo.GetPayment(c.Request.Context(), cargoID); pay != nil {
		cp := gin.H{}
		if pay.TotalAmount != nil {
			cp["listing_total_amount"] = *pay.TotalAmount
		}
		if pay.TotalCurrency != nil {
			cp["listing_total_currency"] = *pay.TotalCurrency
		}
		out["cargo_listing_payment"] = cp
	}
	resp.OKLang(c, "ok", out)
}

// UpdateCargoReq for PUT /api/cargo/:id (all optional).
type UpdateCargoReq struct {
	Name                 *string          `json:"name"`
	Weight               *float64         `json:"weight"`
	Volume               *float64         `json:"volume"`
	Packaging            *string          `json:"packaging"`
	PackagingAmount      *int             `json:"packaging_amount"`
	Dimensions           *string          `json:"dimensions"`
	Photos               []string         `json:"photos"`
	WayPoints            []WayPointReq    `json:"way_points"`
	ReadyEnabled         *bool            `json:"ready_enabled"`
	ReadyAt              *string          `json:"ready_at"`
	Comment              *string          `json:"comment"`
	TruckType            *string          `json:"truck_type"`
	TempMin              *float64         `json:"temp_min"`
	TempMax              *float64         `json:"temp_max"`
	ADREnabled           *bool            `json:"adr_enabled"`
	ADRClass             *string          `json:"adr_class"`
	LoadingTypes         []string         `json:"loading_types"`
	UnloadingTypes       []string         `json:"unloading_types"`
	IsTwoDriversRequired *bool            `json:"is_two_drivers_required"`
	ShipmentType         *string          `json:"shipment_type"`
	BeltsCount           *int             `json:"belts_count"`
	Documents            *cargo.Documents `json:"documents"`
	ContactName          *string          `json:"contact_name"`
	ContactPhone         *string          `json:"contact_phone"`
	RoutePoints          []RoutePointReq  `json:"route_points"`
	Payment              *PaymentReq      `json:"payment"`
}

// trailerPlateToTruckType maps transport trailer code to cargo.truck_type (Reference / Cargo).
// This removes duplication: create cargo accepts only power_plate_type + trailer_plate_type.
func trailerPlateToTruckType(trailerPlate string) string {
	switch upperStr(trailerPlate) {
	case "REEFER":
		return "REFRIGERATOR"
	case "FLATBED", "LOWBED":
		return "FLATBED"
	case "TANKER":
		return "TANKER"
	case "TENTED", "BOX", "CONTAINER":
		return "TENT"
	default:
		return "OTHER"
	}
}

func validateCargoCreate(req CreateCargoReq) error {
	if req.PackagingAmount != nil && *req.PackagingAmount < 0 {
		return errors.New("packaging_amount must be >= 0")
	}
	if req.VehiclesAmount < 1 || req.VehiclesAmount > 100 {
		return errors.New("vehicles_amount must be between 1 and 100")
	}
	power := upperStr(req.PowerPlateType)
	switch power {
	case "TRUCK", "TRACTOR":
	default:
		return errors.New("power_plate_type must be from reference GET /v1/driver/transport-options → power_plate_types (TRUCK, TRACTOR)")
	}
	trailer := upperStr(req.TrailerPlateType)
	allowedTrailer := map[string]map[string]bool{
		"TRUCK": {
			"FLATBED": true, "TENTED": true, "BOX": true, "REEFER": true, "TANKER": true, "TIPPER": true, "CAR_CARRIER": true,
		},
		"TRACTOR": {
			"FLATBED": true, "TENTED": true, "BOX": true, "REEFER": true, "TANKER": true, "LOWBED": true, "CONTAINER": true,
		},
	}
	if !allowedTrailer[power][trailer] {
		return errors.New("trailer_plate_type must be from reference GET /v1/driver/transport-options → trailer_plate_types_by_power[" + power + "]")
	}
	derivedTruckType := trailerPlateToTruckType(trailer)
	hasLoad, hasUnload := false, false
	for _, rp := range req.RoutePoints {
		if strings.TrimSpace(rp.CountryCode) == "" {
			return errors.New("route_points[].country_code is required")
		}
		if !reference.IsAllowed(rp.Type, reference.AllowedRoutePointTypes()) {
			return errors.New("route_points[].type must be one of: load, unload, customs, transit")
		}
		if strings.ToUpper(strings.TrimSpace(rp.Type)) == "LOAD" {
			hasLoad = true
		}
		if strings.ToUpper(strings.TrimSpace(rp.Type)) == "UNLOAD" {
			hasUnload = true
		}
	}
	if !hasLoad || !hasUnload {
		return errors.New("at least one load and one unload point required")
	}
	if (req.TempMin != nil || req.TempMax != nil) && derivedTruckType != "REFRIGERATOR" {
		return errors.New("temp_min/temp_max require refrigerator trailer_plate_type")
	}
	if req.ADREnabled && (req.ADRClass == nil || *req.ADRClass == "") {
		return errors.New("adr_class required when adr_enabled is true")
	}
	if req.ReadyEnabled && (req.ReadyAt == nil || *req.ReadyAt == "") {
		return errors.New("ready_at required when ready_enabled is true")
	}
	if req.ShipmentType != nil && *req.ShipmentType != "" && !reference.IsAllowed(*req.ShipmentType, reference.AllowedShipmentTypes()) {
		return errors.New("shipment_type must be from reference GET /v1/reference/cargo → shipment_type")
	}
	for i, v := range req.LoadingTypes {
		if v != "" && !reference.IsAllowed(v, reference.AllowedLoadingTypes()) {
			return errors.New("loading_types[" + strconv.Itoa(i) + "] must be from reference GET /v1/reference/cargo → loading_type")
		}
	}
	for i, v := range req.UnloadingTypes {
		if v != "" && !reference.IsAllowed(v, reference.AllowedLoadingTypes()) {
			return errors.New("unloading_types[" + strconv.Itoa(i) + "] must be from reference GET /v1/reference/cargo → loading_type")
		}
	}
	for i, wp := range req.WayPoints {
		wt := upperStr(wp.Type)
		if wt != "" && wt != "TRANSIT" && wt != "CUSTOMS" {
			return errors.New("way_points[" + strconv.Itoa(i) + "].type must be TRANSIT or CUSTOMS")
		}
	}
	if req.Payment != nil {
		if !req.Payment.PriceRequest && req.Payment.TotalAmount == nil {
			return errors.New("total_amount or price_request required in payment")
		}
		if req.Payment.TotalCurrency != nil && *req.Payment.TotalCurrency != "" && !reference.IsAllowed(*req.Payment.TotalCurrency, reference.AllowedCurrencies()) {
			return errors.New("payment.total_currency must be from reference GET /v1/reference/cargo → currency")
		}
		if req.Payment.PrepaymentCurrency != nil && *req.Payment.PrepaymentCurrency != "" && !reference.IsAllowed(*req.Payment.PrepaymentCurrency, reference.AllowedCurrencies()) {
			return errors.New("payment.prepayment_currency must be from reference GET /v1/reference/cargo → currency")
		}
		if req.Payment.RemainingCurrency != nil && *req.Payment.RemainingCurrency != "" && !reference.IsAllowed(*req.Payment.RemainingCurrency, reference.AllowedCurrencies()) {
			return errors.New("payment.remaining_currency must be from reference GET /v1/reference/cargo → currency")
		}
		if req.Payment.PrepaymentType != nil && *req.Payment.PrepaymentType != "" && !reference.IsAllowed(*req.Payment.PrepaymentType, reference.AllowedPrepaymentTypes()) {
			return errors.New("payment.prepayment_type must be from reference GET /v1/reference/cargo → prepayment_type")
		}
		if req.Payment.RemainingType != nil && *req.Payment.RemainingType != "" && !reference.IsAllowed(*req.Payment.RemainingType, reference.AllowedRemainingTypes()) {
			return errors.New("payment.remaining_type must be from reference GET /v1/reference/cargo → remaining_type")
		}
		if req.Payment.PaymentNote != nil && len(strings.TrimSpace(*req.Payment.PaymentNote)) > 500 {
			return errors.New("payment.payment_note max length is 500")
		}
	}
	return nil
}

func validateCargoUpdate(req UpdateCargoReq) error {
	if req.PackagingAmount != nil && *req.PackagingAmount < 0 {
		return errors.New("packaging_amount must be >= 0")
	}
	if req.Weight != nil && *req.Weight <= 0 {
		return errors.New("weight must be > 0")
	}
	if req.TruckType != nil && *req.TruckType != "" && !reference.IsAllowed(*req.TruckType, reference.AllowedTruckTypes()) {
		return errors.New("truck_type must be from reference GET /v1/reference/cargo → truck_type")
	}
	if req.TempMin != nil || req.TempMax != nil {
		if req.TruckType == nil || strings.ToUpper(strings.TrimSpace(*req.TruckType)) != "REFRIGERATOR" {
			return errors.New("temp_min/temp_max require truck_type refrigerator")
		}
	}
	if req.ADREnabled != nil && *req.ADREnabled && (req.ADRClass == nil || *req.ADRClass == "") {
		return errors.New("adr_class required when adr_enabled is true")
	}
	if req.ReadyEnabled != nil && *req.ReadyEnabled && (req.ReadyAt == nil || *req.ReadyAt == "") {
		return errors.New("ready_at required when ready_enabled is true")
	}
	if req.ShipmentType != nil && *req.ShipmentType != "" && !reference.IsAllowed(*req.ShipmentType, reference.AllowedShipmentTypes()) {
		return errors.New("shipment_type must be from reference GET /v1/reference/cargo → shipment_type")
	}
	for i, v := range req.LoadingTypes {
		if v != "" && !reference.IsAllowed(v, reference.AllowedLoadingTypes()) {
			return errors.New("loading_types[" + strconv.Itoa(i) + "] must be from reference GET /v1/reference/cargo → loading_type")
		}
	}
	for i, v := range req.UnloadingTypes {
		if v != "" && !reference.IsAllowed(v, reference.AllowedLoadingTypes()) {
			return errors.New("unloading_types[" + strconv.Itoa(i) + "] must be from reference GET /v1/reference/cargo → loading_type")
		}
	}
	for i, wp := range req.WayPoints {
		wt := upperStr(wp.Type)
		if wt != "" && wt != "TRANSIT" && wt != "CUSTOMS" {
			return errors.New("way_points[" + strconv.Itoa(i) + "].type must be TRANSIT or CUSTOMS")
		}
	}
	for i, rp := range req.RoutePoints {
		if strings.TrimSpace(rp.CountryCode) == "" {
			return errors.New("route_points[" + strconv.Itoa(i) + "].country_code is required")
		}
		if rp.Type != "" && !reference.IsAllowed(rp.Type, reference.AllowedRoutePointTypes()) {
			return errors.New("route_points[" + strconv.Itoa(i) + "].type must be one of: load, unload, customs, transit")
		}
	}
	if req.Payment != nil {
		if req.Payment.TotalCurrency != nil && *req.Payment.TotalCurrency != "" && !reference.IsAllowed(*req.Payment.TotalCurrency, reference.AllowedCurrencies()) {
			return errors.New("payment.total_currency must be from reference GET /v1/reference/cargo → currency")
		}
		if req.Payment.PrepaymentCurrency != nil && *req.Payment.PrepaymentCurrency != "" && !reference.IsAllowed(*req.Payment.PrepaymentCurrency, reference.AllowedCurrencies()) {
			return errors.New("payment.prepayment_currency must be from reference GET /v1/reference/cargo → currency")
		}
		if req.Payment.RemainingCurrency != nil && *req.Payment.RemainingCurrency != "" && !reference.IsAllowed(*req.Payment.RemainingCurrency, reference.AllowedCurrencies()) {
			return errors.New("payment.remaining_currency must be from reference GET /v1/reference/cargo → currency")
		}
		if req.Payment.PrepaymentType != nil && *req.Payment.PrepaymentType != "" && !reference.IsAllowed(*req.Payment.PrepaymentType, reference.AllowedPrepaymentTypes()) {
			return errors.New("payment.prepayment_type must be from reference GET /v1/reference/cargo → prepayment_type")
		}
		if req.Payment.RemainingType != nil && *req.Payment.RemainingType != "" && !reference.IsAllowed(*req.Payment.RemainingType, reference.AllowedRemainingTypes()) {
			return errors.New("payment.remaining_type must be from reference GET /v1/reference/cargo → remaining_type")
		}
		if req.Payment.PaymentNote != nil && len(strings.TrimSpace(*req.Payment.PaymentNote)) > 500 {
			return errors.New("payment.payment_note max length is 500")
		}
	}
	if len(req.RoutePoints) > 0 {
		for i, rp := range req.RoutePoints {
			if rp.Date == nil || strings.TrimSpace(*rp.Date) == "" {
				return fmt.Errorf("route_points[%d].date is required (RFC3339 UTC)", i)
			}
			if _, err := parseRFC3339UTC(*rp.Date); err != nil {
				return fmt.Errorf("route_points[%d].date: %w", i, err)
			}
		}
	}
	return nil
}

// parseRFC3339UTC parses RFC3339 / RFC3339Nano and returns UTC instant.
func parseRFC3339UTC(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty")
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func buildRoutePointInputs(points []RoutePointReq) ([]cargo.RoutePointInput, error) {
	out := make([]cargo.RoutePointInput, 0, len(points))
	for i, rp := range points {
		if rp.Date == nil || strings.TrimSpace(*rp.Date) == "" {
			return nil, fmt.Errorf("route_points[%d].date is required (RFC3339 UTC, e.g. 2026-03-23T10:45:30Z)", i)
		}
		pt, err := parseRFC3339UTC(*rp.Date)
		if err != nil {
			return nil, fmt.Errorf("route_points[%d].date must be RFC3339: %w", i, err)
		}
		out = append(out, cargo.RoutePointInput{
			Type:         upperStr(rp.Type),
			CountryCode:  upperStr(rp.CountryCode),
			CityCode:     rp.CityCode,
			RegionCode:   rp.RegionCode,
			Address:      rp.Address,
			Orientir:     rp.Orientir,
			Lat:          rp.Lat,
			Lng:          rp.Lng,
			PlaceID:      rp.PlaceID,
			Comment:      rp.Comment,
			PointOrder:   rp.PointOrder,
			IsMainLoad:   rp.IsMainLoad,
			IsMainUnload: rp.IsMainUnload,
			PointAt:      &pt,
		})
	}
	return out, nil
}

func upperStr(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func strPtrUpper(s *string) *string {
	if s == nil || *s == "" {
		return s
	}
	u := upperStr(*s)
	return &u
}

func shipmentTypePtrUpper(s *string) *cargo.ShipmentType {
	if s == nil || *s == "" {
		return nil
	}
	u := cargo.ShipmentType(upperStr(*s))
	return &u
}

func toWayPointInputs(points []WayPointReq) []cargo.WayPoint {
	out := make([]cargo.WayPoint, 0, len(points))
	for _, wp := range points {
		out = append(out, cargo.WayPoint{
			Type:        upperStr(wp.Type),
			CountryCode: upperStr(wp.CountryCode),
			CityCode:    wp.CityCode,
			RegionCode:  wp.RegionCode,
			Address:     wp.Address,
			Orientir:    wp.Orientir,
			Lat:         wp.Lat,
			Lng:         wp.Lng,
			PlaceID:     wp.PlaceID,
			Comment:     wp.Comment,
		})
	}
	return out
}

func toCreateParams(req CreateCargoReq) cargo.CreateParams {
	loadingTypes := make([]string, 0, len(req.LoadingTypes))
	for _, v := range req.LoadingTypes {
		loadingTypes = append(loadingTypes, upperStr(v))
	}
	p := cargo.CreateParams{
		Name:                 req.Name,
		Weight:               req.Weight,
		Volume:               req.Volume,
		VehiclesAmount:       req.VehiclesAmount,
		Packaging:            req.Packaging,
		PackagingAmount:      req.PackagingAmount,
		Dimensions:           req.Dimensions,
		Photos:               req.Photos,
		WayPoints:            toWayPointInputs(req.WayPoints),
		ReadyEnabled:         req.ReadyEnabled,
		ReadyAt:              req.ReadyAt,
		Comment:              req.Comment,
		TruckType:            trailerPlateToTruckType(req.TrailerPlateType),
		PowerPlateType:       upperStr(req.PowerPlateType),
		TrailerPlateType:     upperStr(req.TrailerPlateType),
		TempMin:              req.TempMin,
		TempMax:              req.TempMax,
		ADREnabled:           req.ADREnabled,
		ADRClass:             strPtrUpper(req.ADRClass),
		LoadingTypes:         loadingTypes,
		UnloadingTypes:       toUpperSlice(req.UnloadingTypes),
		IsTwoDriversRequired: req.IsTwoDriversRequired,
		ShipmentType:         shipmentTypePtrUpper(req.ShipmentType),
		BeltsCount:           req.BeltsCount,
		Documents:            req.Documents,
		ContactName:          req.ContactName,
		ContactPhone:         req.ContactPhone,
		CargoTypeID:          req.CargoTypeID,
		Status:               cargo.StatusPendingModeration,
	}
	if req.Payment != nil {
		p.Payment = &cargo.PaymentInput{
			IsNegotiable:       req.Payment.IsNegotiable,
			PriceRequest:       req.Payment.PriceRequest,
			TotalAmount:        req.Payment.TotalAmount,
			TotalCurrency:      strPtrUpper(req.Payment.TotalCurrency),
			WithPrepayment:     req.Payment.WithPrepayment,
			PrepaymentAmount:   req.Payment.PrepaymentAmount,
			PrepaymentCurrency: strPtrUpper(req.Payment.PrepaymentCurrency),
			PrepaymentType:     strPtrUpper(req.Payment.PrepaymentType),
			RemainingAmount:    req.Payment.RemainingAmount,
			RemainingCurrency:  strPtrUpper(req.Payment.RemainingCurrency),
			RemainingType:      strPtrUpper(req.Payment.RemainingType),
			PaymentNote:        req.Payment.PaymentNote,
			PaymentTermsNote:   req.Payment.PaymentTermsNote,
		}
	}
	return p
}

func toUpdateParams(req UpdateCargoReq) cargo.UpdateParams {
	p := cargo.UpdateParams{}
	p.Name = req.Name
	p.Weight = req.Weight
	p.Volume = req.Volume
	p.Packaging = req.Packaging
	p.PackagingAmount = req.PackagingAmount
	p.Dimensions = req.Dimensions
	p.Photos = req.Photos
	p.WayPoints = toWayPointInputs(req.WayPoints)
	p.ReadyEnabled = req.ReadyEnabled
	p.ReadyAt = req.ReadyAt
	p.Comment = req.Comment
	if req.TruckType != nil {
		u := upperStr(*req.TruckType)
		p.TruckType = &u
	}
	p.TempMin = req.TempMin
	p.TempMax = req.TempMax
	p.ADREnabled = req.ADREnabled
	p.ADRClass = req.ADRClass
	if len(req.LoadingTypes) > 0 {
		p.LoadingTypes = toUpperSlice(req.LoadingTypes)
	}
	if len(req.UnloadingTypes) > 0 {
		p.UnloadingTypes = toUpperSlice(req.UnloadingTypes)
	}
	p.IsTwoDriversRequired = req.IsTwoDriversRequired
	p.ShipmentType = shipmentTypePtrUpper(req.ShipmentType)
	p.BeltsCount = req.BeltsCount
	p.Documents = req.Documents
	p.ContactName = req.ContactName
	p.ContactPhone = req.ContactPhone
	if req.Payment != nil {
		p.Payment = &cargo.PaymentInput{
			IsNegotiable: req.Payment.IsNegotiable, PriceRequest: req.Payment.PriceRequest,
			TotalAmount: req.Payment.TotalAmount, TotalCurrency: strPtrUpper(req.Payment.TotalCurrency),
			WithPrepayment:   req.Payment.WithPrepayment,
			PrepaymentAmount: req.Payment.PrepaymentAmount, PrepaymentCurrency: strPtrUpper(req.Payment.PrepaymentCurrency),
			PrepaymentType: strPtrUpper(req.Payment.PrepaymentType), RemainingAmount: req.Payment.RemainingAmount,
			RemainingCurrency: strPtrUpper(req.Payment.RemainingCurrency), RemainingType: strPtrUpper(req.Payment.RemainingType),
			PaymentNote: req.Payment.PaymentNote, PaymentTermsNote: req.Payment.PaymentTermsNote,
		}
	}
	return p
}

func toUpperSlice(items []string) []string {
	out := make([]string, 0, len(items))
	for _, v := range items {
		out = append(out, upperStr(v))
	}
	return out
}

func toCargoListItems(items []cargo.Cargo) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, c := range items {
		out = append(out, toCargoItem(&c))
	}
	return out
}

func toCargoItem(c *cargo.Cargo) gin.H {
	out := gin.H{
		"id": c.ID.String(), "name": c.Name, "weight": c.Weight, "volume": c.Volume,
		"vehicles_amount": c.VehiclesAmount,
		"vehicles_left":   c.VehiclesLeft,
		"packaging":       c.Packaging, "packaging_amount": c.PackagingAmount, "dimensions": c.Dimensions, "photos": c.PhotoURLs, "way_points": c.WayPoints,
		"ready_enabled": c.ReadyEnabled, "ready_at": c.ReadyAt, "comment": c.Comment,
		"truck_type": c.TruckType, "temp_min": c.TempMin, "temp_max": c.TempMax,
		"power_plate_type": c.PowerPlateType, "trailer_plate_type": c.TrailerPlateType,
		"adr_enabled": c.ADREnabled, "adr_class": c.ADRClass, "loading_types": c.LoadingTypes,
		"unloading_types": c.UnloadingTypes, "is_two_drivers_required": c.IsTwoDriversRequired,
		"shipment_type": c.ShipmentType, "belts_count": c.BeltsCount, "documents": c.Documents,
		"contact_name": c.ContactName, "contact_phone": c.ContactPhone, "status": c.Status,
		"created_at": c.CreatedAt, "updated_at": c.UpdatedAt,
	}
	if c.CreatedByType != nil {
		out["created_by_type"] = *c.CreatedByType
	} else {
		out["created_by_type"] = nil
	}
	if c.CreatedByID != nil {
		out["created_by_id"] = c.CreatedByID.String()
	} else {
		out["created_by_id"] = nil
	}
	if c.CompanyID != nil {
		out["company_id"] = c.CompanyID.String()
	} else {
		out["company_id"] = nil
	}
	if c.CargoTypeCode != nil {
		out["cargo_type"] = gin.H{
			"id":      c.CargoTypeID.String(),
			"code":    *c.CargoTypeCode,
			"name_ru": derefStr(c.CargoTypeNameRU),
			"name_uz": derefStr(c.CargoTypeNameUZ),
			"name_en": derefStr(c.CargoTypeNameEN),
			"name_tr": derefStr(c.CargoTypeNameTR),
			"name_zh": derefStr(c.CargoTypeNameZH),
		}
	} else {
		out["cargo_type"] = nil
	}
	if c.ModerationRejectionReason != nil {
		out["moderation_rejection_reason"] = *c.ModerationRejectionReason
	} else {
		out["moderation_rejection_reason"] = nil
	}
	return out
}

func toCargoDetail(c *cargo.Cargo, points []cargo.RoutePoint, pay *cargo.Payment, stats *cargo.OfferInvitationStats) gin.H {
	detail := toCargoItem(c)
	detail["route_points"] = toRoutePointsResp(points)
	detail["payment"] = toPaymentResp(pay)
	if stats != nil {
		detail["offer_invitation_stats"] = gin.H{
			"from_drivers_total":   stats.IncomingTotal,
			"from_drivers_pending": stats.IncomingPending,
			"to_drivers_total":     stats.OutgoingTotal,
			"to_drivers_pending":   stats.OutgoingPending,
		}
	}
	return detail
}

func toRoutePointsResp(p []cargo.RoutePoint) []gin.H {
	out := make([]gin.H, 0, len(p))
	for _, rp := range p {
		item := gin.H{
			"id": rp.ID.String(), "cargo_id": rp.CargoID.String(), "type": upperStr(rp.Type),
			"country_code": upperStr(rp.CountryCode), "city_code": rp.CityCode, "region_code": rp.RegionCode, "address": rp.Address, "orientir": rp.Orientir,
			"lat": rp.Lat, "lng": rp.Lng, "place_id": rp.PlaceID, "comment": rp.Comment,
			"point_order": rp.PointOrder, "is_main_load": rp.IsMainLoad, "is_main_unload": rp.IsMainUnload,
		}
		if rp.PointAt != nil {
			item["date"] = rp.PointAt.UTC().Format(time.RFC3339)
		} else {
			item["date"] = nil
		}
		out = append(out, item)
	}
	return out
}

func toPaymentResp(p *cargo.Payment) gin.H {
	if p == nil {
		return nil
	}
	return gin.H{
		"id": p.ID.String(), "cargo_id": p.CargoID.String(), "is_negotiable": p.IsNegotiable, "price_request": p.PriceRequest,
		"total_amount": p.TotalAmount, "total_currency": p.TotalCurrency,
		"with_prepayment":   p.WithPrepayment,
		"prepayment_amount": p.PrepaymentAmount, "prepayment_currency": p.PrepaymentCurrency, "prepayment_type": p.PrepaymentType,
		"remaining_amount": p.RemainingAmount, "remaining_currency": p.RemainingCurrency, "remaining_type": p.RemainingType,
		"payment_note": p.PaymentNote, "payment_terms_note": p.PaymentTermsNote,
	}
}

func toOfferList(offers []cargo.Offer) []gin.H {
	out := make([]gin.H, 0, len(offers))
	for _, o := range offers {
		pb := o.ProposedBy
		if pb == "" {
			pb = cargo.OfferProposedByDriver
		}
		item := gin.H{
			"id": o.ID.String(), "cargo_id": o.CargoID.String(), "carrier_id": o.CarrierID.String(),
			"proposed_by": pb,
			"price":       o.Price, "currency": o.Currency, "comment": o.Comment, "status": o.Status, "created_at": o.CreatedAt,
		}
		if o.RejectionReason != nil && *o.RejectionReason != "" {
			item["rejection_reason"] = *o.RejectionReason
		}
		out = append(out, item)
	}
	return out
}

func getIntQuery(c *gin.Context, key string, defaultVal int) int {
	v := c.Query(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultVal
	}
	return n
}

func strPtr(s string) *string { return &s }

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmptyStr(items ...*string) string {
	for _, it := range items {
		if it != nil && strings.TrimSpace(*it) != "" {
			return strings.TrimSpace(*it)
		}
	}
	return ""
}
