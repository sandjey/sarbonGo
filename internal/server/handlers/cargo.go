package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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
	"sarbonNew/internal/reference"
	"sarbonNew/internal/security"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/trips"
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
	jwtm      *security.JWTManager
	cfg       config.Config
}

func NewCargoHandler(logger *zap.Logger, repo *cargo.Repo, tripsRepo *trips.Repo, driversRepo *drivers.Repo, jwtm *security.JWTManager, cfg config.Config) *CargoHandler {
	return &CargoHandler{logger: logger, repo: repo, tripsRepo: tripsRepo, drivers: driversRepo, jwtm: jwtm, cfg: cfg}
}

// CreateCargoReq body for POST /api/cargo.
type CreateCargoReq struct {
	Name             *string        `json:"name"`
	Weight           float64        `json:"weight" binding:"required,gt=0"`
	Volume           float64        `json:"volume" binding:"required,gt=0"` // объём груза (м³)
	VehiclesAmount   int            `json:"vehicles_amount" binding:"required,gte=1,lte=100"`
	Packaging        *string        `json:"packaging"`
	Dimensions       *string        `json:"dimensions"`
	Photos           []string       `json:"photos"` // до 5: внешние URL и/или pending с POST /api/cargo/photos (url или UUID)
	ReadyEnabled     bool           `json:"ready_enabled"`
	ReadyAt          *string        `json:"ready_at"`
	LoadComment      *string        `json:"load_comment"`
	PowerPlateType   string         `json:"power_plate_type" binding:"required"`
	TrailerPlateType string         `json:"trailer_plate_type" binding:"required"`
	CapacityRequired float64        `json:"capacity_required" binding:"required,gt=0"`
	TempMin          *float64       `json:"temp_min"`
	TempMax          *float64       `json:"temp_max"`
	ADREnabled       bool           `json:"adr_enabled"`
	ADRClass         *string        `json:"adr_class"`
	LoadingTypes     []string       `json:"loading_types"`
	Requirements     []string       `json:"requirements"`
	ShipmentType     *string        `json:"shipment_type"`
	BeltsCount       *int           `json:"belts_count"`
	Documents        *cargo.Documents `json:"documents"`
	ContactName      *string        `json:"contact_name"`
	ContactPhone     *string        `json:"contact_phone"`
	CargoTypeID      *uuid.UUID     `json:"cargo_type_id"`
	RoutePoints      []RoutePointReq `json:"route_points" binding:"required,dive"`
	Payment          *PaymentReq    `json:"payment"`
	CompanyID        *uuid.UUID     `json:"company_id"`
}

type RoutePointReq struct {
	Type         string   `json:"type" binding:"required,oneof=LOAD UNLOAD CUSTOMS TRANSIT"`
	CityCode     string   `json:"city_code" binding:"required"`  // код города (TAS, SAM, DXB) — из справочника
	RegionCode   string   `json:"region_code"`                   // код региона/области (опционально)
	Address      string   `json:"address" binding:"required"`    // адрес (улица, дом)
	Orientir     string   `json:"orientir"`                     // ориентир для водителя
	Lat          float64  `json:"lat" binding:"required"`
	Lng          float64  `json:"lng" binding:"required"`
	PlaceID      *string  `json:"place_id"` // ID от карт для автокомплита
	Comment      *string  `json:"comment"`
	PointOrder   int      `json:"point_order" binding:"required"`
	IsMainLoad   bool     `json:"is_main_load"`
	IsMainUnload bool     `json:"is_main_unload"`
	// Date — плановая дата/время точки (RFC3339, хранится и отдаётся в UTC).
	Date *string `json:"date" binding:"required"`
}

type PaymentReq struct {
	IsNegotiable       bool     `json:"is_negotiable"`
	PriceRequest       bool     `json:"price_request"`
	TotalAmount        *float64 `json:"total_amount"`
	TotalCurrency      *string  `json:"total_currency"`
	WithPrepayment     bool     `json:"with_prepayment"`
	WithoutPrepayment  bool     `json:"without_prepayment"`
	PrepaymentAmount   *float64 `json:"prepayment_amount"`
	PrepaymentCurrency *string  `json:"prepayment_currency"`
	PrepaymentType     *string  `json:"prepayment_type"`
	RemainingAmount    *float64 `json:"remaining_amount"`
	RemainingCurrency  *string  `json:"remaining_currency"`
	RemainingType      *string  `json:"remaining_type"`
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
							"limit":  h.cfg.FreelanceDispatcherCargoLimit,
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
	resp.SuccessLang(c, http.StatusCreated, "created", toCargoDetail(obj, points, pay))
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
			resp.ErrorLang(c, http.StatusBadRequest, "file_too_large")
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
			resp.ErrorLang(c, http.StatusBadRequest, "file_too_large")
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
	f := cargo.ListFilter{
		Page:        getIntQuery(c, "page", 1),
		Limit:       getIntQuery(c, "limit", 20),
		Sort:        c.DefaultQuery("sort", "created_at:desc"),
		TruckType:   strings.TrimSpace(c.Query("truck_type")),
		CreatedFrom: strings.TrimSpace(c.Query("created_from")),
		CreatedTo:   strings.TrimSpace(c.Query("created_to")),
	}
	if v := c.Query("status"); v != "" {
		f.Status = strings.Split(v, ",")
		for i := range f.Status {
			f.Status[i] = strings.TrimSpace(strings.ToUpper(f.Status[i]))
		}
	}
	// When driver lists "searching" cargo, show only SEARCHING_ALL + SEARCHING_COMPANY (his company)
	if h.jwtm != nil && h.drivers != nil {
		if raw := strings.TrimSpace(c.GetHeader(mw.HeaderUserToken)); raw != "" {
			if userID, role, _, _, err := h.jwtm.ParseAccessWithSID(raw); err == nil && role == "driver" && userID != uuid.Nil {
				if drv, _ := h.drivers.FindByID(c.Request.Context(), userID); drv != nil && drv.CompanyID != nil && *drv.CompanyID != "" {
					if cid, err := uuid.Parse(*drv.CompanyID); err == nil {
						f.ForDriverCompanyID = &cid
					}
				}
			}
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
	result, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("cargo list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list")
		return
	}
	items := make([]gin.H, 0, len(result.Items))
	for i := range result.Items {
		points, _ := h.repo.GetRoutePoints(c.Request.Context(), result.Items[i].ID)
		pay, _ := h.repo.GetPayment(c.Request.Context(), result.Items[i].ID)
		items = append(items, toCargoDetail(&result.Items[i], points, pay))
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
	resp.OKLang(c, "ok", toCargoDetail(obj, points, pay))
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
		Status string `json:"status" binding:"required,oneof=CREATED PENDING_MODERATION SEARCHING_ALL SEARCHING_COMPANY REJECTED ASSIGNED IN_PROGRESS IN_TRANSIT DELIVERED COMPLETED CANCELLED"`
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
	offerID, err := h.repo.CreateOffer(c.Request.Context(), id, req.CarrierID, req.Price, req.Currency, req.Comment)
	if err != nil {
		h.logger.Error("cargo create offer", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed to create offer")
		return
	}
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"id": offerID.String()})
}

func (h *CargoHandler) ListOffers(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	offers, err := h.repo.GetOffers(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("cargo list offers", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed to list offers")
		return
	}
	resp.OKLang(c, "ok", gin.H{"items": toOfferList(offers)})
}

// ListMyCargoOffers lists driver offers (requests) grouped by bucket:
// sent (PENDING), accepted (ACCEPTED, not completed), completed (ACCEPTED + cargo COMPLETED), rejected (REJECTED).
// Endpoint: GET /v1/driver/cargo-offers?bucket=...
func (h *CargoHandler) ListMyCargoOffers(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)

	bucket := strings.ToLower(strings.TrimSpace(c.Query("bucket")))
	if bucket == "" {
		bucket = "sent"
	}
	switch bucket {
	case "sent", "accepted", "completed", "rejected":
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
		item := gin.H{
			"offer_id":          row.ID.String(),
			"cargo_id":          row.CargoID.String(),
			"status":            row.Status,
			"price":             row.Price,
			"currency":          row.Currency,
			"comment":           row.Comment,
			"created_at":        row.CreatedAt,
			"rejection_reason": row.RejectionReason,
			"cargo": gin.H{
				"id":                row.CargoID.String(),
				"name":              row.CargoName,
				"status":            row.CargoStatus,
				"weight":            row.CargoWeight,
				"volume":            row.CargoVolume,
				"truck_type":        row.CargoTruckType,
				"vehicles_left":     row.CargoVehiclesLeft,
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

	resp.OKLang(c, "cargo_offers_listed", gin.H{
		"items":  items,
		"total":  total,
		"page":   page,
		"limit":  limit,
		"bucket": bucket,
	})
}

func (h *CargoHandler) AcceptOffer(c *gin.Context) {
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid offer id")
		return
	}
	cargoID, carrierID, err := h.repo.AcceptOffer(c.Request.Context(), offerID)
	if err != nil {
		switch {
		case errors.Is(err, cargo.ErrOfferNotFoundOrNotPending):
			resp.ErrorLang(c, http.StatusNotFound, "offer_not_found_or_not_pending")
		case errors.Is(err, cargo.ErrCargoSlotsFull):
			resp.ErrorLang(c, http.StatusConflict, "cargo_slots_full")
		case errors.Is(err, cargo.ErrDriverBusy):
			resp.ErrorLang(c, http.StatusConflict, "driver_busy_with_another_cargo")
		case errors.Is(err, cargo.ErrCargoNotSearching):
			resp.ErrorLang(c, http.StatusConflict, "cargo_not_searching")
		default:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		}
		return
	}
	if h.tripsRepo != nil {
		tripID, _ := h.tripsRepo.Create(c.Request.Context(), cargoID, offerID)
		if tripID != uuid.Nil {
			_ = h.tripsRepo.AssignDriver(c.Request.Context(), tripID, carrierID)
			resp.OKLang(c, "ok", gin.H{"cargo_id": cargoID.String(), "offer_id": offerID.String(), "trip_id": tripID.String(), "driver_id": carrierID.String(), "status": "accepted"})
			return
		}
	}
	resp.OKLang(c, "ok", gin.H{"cargo_id": cargoID.String(), "offer_id": offerID.String(), "status": "accepted"})
}

// RejectOfferReq body for POST /v1/dispatchers/offers/:id/reject (reason optional).
type RejectOfferReq struct {
	Reason string `json:"reason"`
}

// RejectOfferDispatcher rejects an offer (dispatcher only; cargo must be created by this dispatcher). Reason optional.
func (h *CargoHandler) RejectOfferDispatcher(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
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
	cargoObj, _ := h.repo.GetByID(c.Request.Context(), offer.CargoID, false)
	if cargoObj == nil {
		resp.ErrorLang(c, http.StatusNotFound, "cargo_not_found")
		return
	}
	if cargoObj.CreatedByType == nil || *cargoObj.CreatedByType != "DISPATCHER" || cargoObj.CreatedByID == nil || *cargoObj.CreatedByID != dispatcherID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_cargo")
		return
	}
	var req RejectOfferReq
	_ = c.ShouldBindJSON(&req)
	if err := h.repo.RejectOffer(c.Request.Context(), offerID, req.Reason); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "offer_not_found_or_not_pending")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": "rejected"})
}

// UpdateCargoReq for PUT /api/cargo/:id (all optional).
type UpdateCargoReq struct {
	Name             *string          `json:"name"`
	Weight           *float64         `json:"weight"`
	Volume           *float64         `json:"volume"`
	Packaging        *string          `json:"packaging"`
	Dimensions       *string          `json:"dimensions"`
	Photos           []string         `json:"photos"`
	ReadyEnabled     *bool            `json:"ready_enabled"`
	ReadyAt          *string          `json:"ready_at"`
	LoadComment      *string          `json:"load_comment"`
	TruckType        *string          `json:"truck_type"`
	CapacityRequired *float64         `json:"capacity_required"`
	TempMin          *float64         `json:"temp_min"`
	TempMax          *float64         `json:"temp_max"`
	ADREnabled       *bool            `json:"adr_enabled"`
	ADRClass         *string          `json:"adr_class"`
	LoadingTypes     []string         `json:"loading_types"`
	Requirements     []string         `json:"requirements"`
	ShipmentType     *string          `json:"shipment_type"`
	BeltsCount       *int             `json:"belts_count"`
	Documents        *cargo.Documents `json:"documents"`
	ContactName      *string          `json:"contact_name"`
	ContactPhone     *string          `json:"contact_phone"`
	RoutePoints      []RoutePointReq  `json:"route_points"`
	Payment          *PaymentReq      `json:"payment"`
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
	}
	return nil
}

func validateCargoUpdate(req UpdateCargoReq) error {
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
	for i, rp := range req.RoutePoints {
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

func toCreateParams(req CreateCargoReq) cargo.CreateParams {
	loadingTypes := make([]string, 0, len(req.LoadingTypes))
	for _, v := range req.LoadingTypes {
		loadingTypes = append(loadingTypes, upperStr(v))
	}
	p := cargo.CreateParams{
		Name:             req.Name,
		Weight:           req.Weight,
		Volume:           req.Volume,
		VehiclesAmount:   req.VehiclesAmount,
		Packaging:        req.Packaging,
		Dimensions:       req.Dimensions,
		Photos:           req.Photos,
		ReadyEnabled:     req.ReadyEnabled,
		ReadyAt:          req.ReadyAt,
		LoadComment:      req.LoadComment,
		TruckType:        trailerPlateToTruckType(req.TrailerPlateType),
		PowerPlateType:   upperStr(req.PowerPlateType),
		TrailerPlateType: upperStr(req.TrailerPlateType),
		CapacityRequired: req.CapacityRequired,
		TempMin:          req.TempMin,
		TempMax:          req.TempMax,
		ADREnabled:       req.ADREnabled,
		ADRClass:         strPtrUpper(req.ADRClass),
		LoadingTypes:     loadingTypes,
		Requirements:     req.Requirements,
		ShipmentType:     shipmentTypePtrUpper(req.ShipmentType),
		BeltsCount:       req.BeltsCount,
		Documents:        req.Documents,
		ContactName:      req.ContactName,
		ContactPhone:     req.ContactPhone,
		CargoTypeID:      req.CargoTypeID,
		Status:           cargo.StatusPendingModeration,
	}
	if req.Payment != nil {
		p.Payment = &cargo.PaymentInput{
			IsNegotiable:       req.Payment.IsNegotiable,
			PriceRequest:       req.Payment.PriceRequest,
			TotalAmount:        req.Payment.TotalAmount,
			TotalCurrency:      strPtrUpper(req.Payment.TotalCurrency),
			WithPrepayment:     req.Payment.WithPrepayment,
			WithoutPrepayment:  req.Payment.WithoutPrepayment,
			PrepaymentAmount:   req.Payment.PrepaymentAmount,
			PrepaymentCurrency: strPtrUpper(req.Payment.PrepaymentCurrency),
			PrepaymentType:     strPtrUpper(req.Payment.PrepaymentType),
			RemainingAmount:    req.Payment.RemainingAmount,
			RemainingCurrency:  strPtrUpper(req.Payment.RemainingCurrency),
			RemainingType:      strPtrUpper(req.Payment.RemainingType),
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
	p.Dimensions = req.Dimensions
	p.Photos = req.Photos
	p.ReadyEnabled = req.ReadyEnabled
	p.ReadyAt = req.ReadyAt
	p.LoadComment = req.LoadComment
	if req.TruckType != nil {
		u := upperStr(*req.TruckType)
		p.TruckType = &u
	}
	p.CapacityRequired = req.CapacityRequired
	p.TempMin = req.TempMin
	p.TempMax = req.TempMax
	p.ADREnabled = req.ADREnabled
	p.ADRClass = req.ADRClass
	if len(req.LoadingTypes) > 0 {
		loadingTypes := make([]string, 0, len(req.LoadingTypes))
		for _, v := range req.LoadingTypes {
			loadingTypes = append(loadingTypes, upperStr(v))
		}
		p.LoadingTypes = loadingTypes
	}
	p.Requirements = req.Requirements
	p.ShipmentType = shipmentTypePtrUpper(req.ShipmentType)
	p.BeltsCount = req.BeltsCount
	p.Documents = req.Documents
	p.ContactName = req.ContactName
	p.ContactPhone = req.ContactPhone
	if req.Payment != nil {
		p.Payment = &cargo.PaymentInput{
			IsNegotiable: req.Payment.IsNegotiable, PriceRequest: req.Payment.PriceRequest,
			TotalAmount: req.Payment.TotalAmount, TotalCurrency: strPtrUpper(req.Payment.TotalCurrency),
			WithPrepayment: req.Payment.WithPrepayment, WithoutPrepayment: req.Payment.WithoutPrepayment,
			PrepaymentAmount: req.Payment.PrepaymentAmount, PrepaymentCurrency: strPtrUpper(req.Payment.PrepaymentCurrency),
			PrepaymentType: strPtrUpper(req.Payment.PrepaymentType), RemainingAmount: req.Payment.RemainingAmount,
			RemainingCurrency: strPtrUpper(req.Payment.RemainingCurrency), RemainingType: strPtrUpper(req.Payment.RemainingType),
		}
	}
	return p
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
		"vehicles_left": c.VehiclesLeft,
		"capacity_required": c.CapacityRequired,
		"packaging": c.Packaging, "dimensions": c.Dimensions, "photos": c.PhotoURLs,
		"ready_enabled": c.ReadyEnabled, "ready_at": c.ReadyAt, "load_comment": c.LoadComment,
		"truck_type": c.TruckType, "temp_min": c.TempMin, "temp_max": c.TempMax,
		"power_plate_type": c.PowerPlateType, "trailer_plate_type": c.TrailerPlateType,
		"adr_enabled": c.ADREnabled, "adr_class": c.ADRClass, "loading_types": c.LoadingTypes, "requirements": c.Requirements,
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

func toCargoDetail(c *cargo.Cargo, points []cargo.RoutePoint, pay *cargo.Payment) gin.H {
	detail := toCargoItem(c)
	detail["route_points"] = toRoutePointsResp(points)
	detail["payment"] = toPaymentResp(pay)
	return detail
}

func toRoutePointsResp(p []cargo.RoutePoint) []gin.H {
	out := make([]gin.H, 0, len(p))
	for _, rp := range p {
		item := gin.H{
			"id": rp.ID.String(), "cargo_id": rp.CargoID.String(), "type": upperStr(rp.Type),
			"city_code": rp.CityCode, "region_code": rp.RegionCode, "address": rp.Address, "orientir": rp.Orientir,
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
		"with_prepayment": p.WithPrepayment, "without_prepayment": p.WithoutPrepayment,
		"prepayment_amount": p.PrepaymentAmount, "prepayment_currency": p.PrepaymentCurrency, "prepayment_type": p.PrepaymentType,
		"remaining_amount": p.RemainingAmount, "remaining_currency": p.RemainingCurrency, "remaining_type": p.RemainingType,
	}
}

func toOfferList(offers []cargo.Offer) []gin.H {
	out := make([]gin.H, 0, len(offers))
	for _, o := range offers {
		out = append(out, gin.H{
			"id": o.ID.String(), "cargo_id": o.CargoID.String(), "carrier_id": o.CarrierID.String(),
			"price": o.Price, "currency": o.Currency, "comment": o.Comment, "status": o.Status, "created_at": o.CreatedAt,
		})
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