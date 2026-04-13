package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/displaynames"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/store"
	"sarbonNew/internal/telegram"
	"sarbonNew/internal/userstream"
	"sarbonNew/internal/util"
)

const maxDriverPhotoSize = 10 * 1024 * 1024 // 10 MB
var allowedDriverPhotoTypes = map[string]bool{"image/jpeg": true, "image/png": true}

type ProfileHandler struct {
	logger      *zap.Logger
	drivers     *drivers.Repo
	displayName *displaynames.Checker
	phoneChange *store.PhoneChangeStore
	tg          *telegram.GatewayClient
	otpTTL      time.Duration
	otpLen      int
	stream      *userstream.Hub
}

func NewProfileHandler(logger *zap.Logger, driversRepo *drivers.Repo, displayName *displaynames.Checker, phoneChange *store.PhoneChangeStore, tg *telegram.GatewayClient, otpTTL time.Duration, otpLen int, stream *userstream.Hub) *ProfileHandler {
	return &ProfileHandler{logger: logger, drivers: driversRepo, displayName: displayName, phoneChange: phoneChange, tg: tg, otpTTL: otpTTL, otpLen: otpLen, stream: stream}
}

// GET /v1/driver/profile
func (h *ProfileHandler) Get(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	_ = h.drivers.TouchOnline(c.Request.Context(), driverID)
	d, err := h.drivers.FindByID(c.Request.Context(), driverID)
	if err != nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
		return
	}
	resp.OKLang(c, "ok", gin.H{"driver": groupedDriverProfile(d)})
}

func groupedDriverProfile(d *drivers.Driver) gin.H {
	if d == nil {
		return gin.H{}
	}
	return gin.H{
		"profile": gin.H{
			"id":                     d.ID,
			"phone":                  d.Phone,
			"name":                   d.Name,
			"has_photo":              d.HasPhoto,
			"work_status":            d.WorkStatus,
			"driver_type":            d.DriverType,
			"rating":                 d.Rating,
			"registration_step":      d.RegistrationStep,
			"registration_status":    d.RegistrationStatus,
			"account_status":         d.AccountStatus,
			"kyc_status":             d.KYCStatus,
			"driver_owner":           d.DriverOwner,
			"has_trips":             d.HasTrips,
			"freelancer_id":          d.FreelancerID,
			"company_id":             d.CompanyID,
			"driver_passport_series": d.DriverPassportSeries,
			"driver_passport_number": d.DriverPassportNumber,
			"driver_pinfl":           d.DriverPINFL,
			"driver_scan_status":     d.DriverScanStatus,
			"latitude":               d.Latitude,
			"longitude":              d.Longitude,
			"last_online_at":         d.LastOnlineAt,
			"created_at":             d.CreatedAt,
			"updated_at":             d.UpdatedAt,
		},
		"power_plate": gin.H{
			"type":        d.PowerPlateType,
			"number":      d.PowerPlateNumber,
			"tech_series": d.PowerTechSeries,
			"tech_number": d.PowerTechNumber,
			"owner_name":  d.PowerOwnerName,
			"scan_status": d.PowerScanStatus,
		},
		"trailer_plate": gin.H{
			"type":        d.TrailerPlateType,
			"number":      d.TrailerPlateNumber,
			"tech_series": d.TrailerTechSeries,
			"tech_number": d.TrailerTechNumber,
			"owner_name":  d.TrailerOwnerName,
			"scan_status": d.TrailerScanStatus,
		},
	}
}

type patchDriverReq struct {
	Name                 *string `json:"name,omitempty"`
	WorkStatus           *string `json:"work_status,omitempty"` // available|loaded|busy
	DriverPassportSeries *string `json:"driver_passport_series,omitempty"`
	DriverPassportNumber *string `json:"driver_passport_number,omitempty"`
	DriverPINFL          *string `json:"driver_pinfl,omitempty"`
}

// PATCH /v1/driver/profile/driver
func (h *ProfileHandler) PatchDriver(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req patchDriverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		if errKey := validatePersonName(v); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
		taken, err := h.displayName.IsTaken(c.Request.Context(), v, &driverID, nil)
		if err != nil {
			h.logger.Error("display name taken check failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if taken {
			resp.ErrorLang(c, http.StatusConflict, "display_name_taken")
			return
		}
		req.Name = &v
	}
	if req.WorkStatus != nil {
		v := strings.ToLower(strings.TrimSpace(*req.WorkStatus))
		switch v {
		case "available", "loaded", "busy":
			req.WorkStatus = &v
		default:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_work_status")
			return
		}
	}
	if req.DriverPassportSeries != nil {
		v := strings.TrimSpace(*req.DriverPassportSeries)
		if errKey := validatePassportSeries(v); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
		req.DriverPassportSeries = &v
	}
	if req.DriverPassportNumber != nil {
		v := strings.TrimSpace(*req.DriverPassportNumber)
		if errKey := validatePassportNumber(v); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
		req.DriverPassportNumber = &v
	}
	if req.DriverPINFL != nil {
		v := strings.TrimSpace(*req.DriverPINFL)
		if errKey := validatePINFL(v); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
		req.DriverPINFL = &v
	}

	if err := h.drivers.UpdateDriverEditable(c.Request.Context(), driverID, drivers.UpdateDriverEditable{
		Name: req.Name, WorkStatus: req.WorkStatus,
		DriverPassportSeries: req.DriverPassportSeries,
		DriverPassportNumber: req.DriverPassportNumber,
		DriverPINFL:          req.DriverPINFL,
	}); err != nil {
		h.logger.Error("update driver profile failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	d, err := h.drivers.FindByID(c.Request.Context(), driverID)
	if err != nil {
		h.logger.Error("driver reload after patch failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	changed := make([]string, 0, 5)
	if req.Name != nil {
		changed = append(changed, "name")
	}
	if req.WorkStatus != nil {
		changed = append(changed, "work_status")
	}
	if req.DriverPassportSeries != nil {
		changed = append(changed, "driver_passport_series")
	}
	if req.DriverPassportNumber != nil {
		changed = append(changed, "driver_passport_number")
	}
	if req.DriverPINFL != nil {
		changed = append(changed, "driver_pinfl")
	}
	publishDriverUpdateToManager(h.stream, h.logger, d, "driver", driverID.String(), "driver.profile.patch", changed)
	resp.OKLang(c, "updated", gin.H{"event": "updated", "driver": d})
}

type heartbeatReq struct {
	Latitude  float64 `json:"latitude" binding:"required"`
	Longitude float64 `json:"longitude" binding:"required"`
}

// PUT /v1/driver/profile/heartbeat — только latitude и longitude; last_online_at всегда обновляется на сервере автоматически.
func (h *ProfileHandler) Heartbeat(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req heartbeatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	if errKey := validateLatLng(req.Latitude, req.Longitude); errKey != "" {
		resp.ErrorLang(c, http.StatusBadRequest, errKey)
		return
	}
	if err := h.drivers.UpdateHeartbeat(c.Request.Context(), driverID, req.Latitude, req.Longitude, time.Now().UTC()); err != nil {
		h.logger.Error("heartbeat update failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	d, err := h.drivers.FindByID(c.Request.Context(), driverID)
	if err != nil {
		h.logger.Error("driver reload after heartbeat failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "heartbeat", gin.H{"event": "heartbeat", "driver": d})
}

type phoneChangeRequestReq struct {
	NewPhone string `json:"new_phone" binding:"required"`
}

// POST /v1/driver/profile/phone-change/request
func (h *ProfileHandler) PhoneChangeRequest(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req phoneChangeRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	newPhone, err := util.ValidateUzPhoneStrict(req.NewPhone)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	// Check uniqueness
	if _, err := h.drivers.FindByPhone(c.Request.Context(), newPhone); err == nil {
		resp.ErrorLang(c, http.StatusConflict, "phone_already_registered")
		return
	}

	ttlSec := int(h.otpTTL.Seconds())
	code, _, err := SendOTP(c.Request.Context(), h.tg, newPhone, ttlSec, h.otpLen)
	if err != nil {
		if WriteOTPSendError(c, err, h.logger, "telegram sendVerificationMessage failed") {
			return
		}
		resp.ErrorLang(c, http.StatusInternalServerError, "otp_generation_failed")
		return
	}
	sessionID, err := h.phoneChange.Create(c.Request.Context(), driverID, newPhone, code)
	if err != nil {
		h.logger.Error("phone change session create failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "otp_sent", gin.H{"event": "otp_sent", "session_id": sessionID, "ttl_seconds": ttlSec})
}

type phoneChangeVerifyReq struct {
	SessionID string `json:"session_id" binding:"required"`
	OTP       string `json:"otp" binding:"required"`
}

// POST /v1/driver/profile/phone-change/verify
func (h *ProfileHandler) PhoneChangeVerify(c *gin.Context) {
	var req phoneChangeVerifyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	otp := strings.TrimSpace(req.OTP)
	if otp == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "otp_required")
		return
	}

	rec, err := h.phoneChange.Verify(c.Request.Context(), sessionID, otp)
	if err != nil {
		switch err {
		case store.ErrPhoneChangeOTPExpired:
			resp.ErrorLang(c, http.StatusUnauthorized, "session_expired_or_invalid")
		case store.ErrPhoneChangeOTPInvalid:
			resp.ErrorLang(c, http.StatusUnauthorized, "otp_invalid")
		case store.ErrPhoneChangeMaxAttempts:
			resp.ErrorLang(c, http.StatusTooManyRequests, "otp_max_attempts_exceeded")
		default:
			h.logger.Error("phone change verify failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		}
		return
	}

	if err := h.drivers.UpdatePhone(c.Request.Context(), rec.DriverID, rec.NewPhone); err != nil {
		if err == drivers.ErrPhoneAlreadyRegistered {
			resp.ErrorLang(c, http.StatusConflict, "phone_already_registered")
			return
		}
		h.logger.Error("phone update failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	d, _ := h.drivers.FindByID(c.Request.Context(), rec.DriverID)
	resp.OKLang(c, "phone_updated", gin.H{"event": "phone_updated", "driver": d})
}

type patchPowerReq struct {
	PowerPlateNumber *string `json:"power_plate_number,omitempty"`
	PowerTechSeries  *string `json:"power_tech_series,omitempty"`
	PowerTechNumber  *string `json:"power_tech_number,omitempty"`
	PowerOwnerName   *string `json:"power_owner_name,omitempty"`
	PowerScanStatus  *bool   `json:"power_scan_status,omitempty"`
}

// PATCH /v1/driver/profile/power
func (h *ProfileHandler) PatchPower(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req patchPowerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}

	trimPtr := func(p **string) {
		if *p == nil {
			return
		}
		v := strings.TrimSpace(**p)
		if v == "" {
			*p = nil
			return
		}
		*p = &v
	}
	trimPtr(&req.PowerPlateNumber)
	trimPtr(&req.PowerTechSeries)
	trimPtr(&req.PowerTechNumber)
	trimPtr(&req.PowerOwnerName)

	if err := h.drivers.UpdatePowerProfile(c.Request.Context(), driverID, drivers.UpdatePowerProfile{
		PowerPlateNumber: req.PowerPlateNumber,
		PowerTechSeries:  req.PowerTechSeries,
		PowerTechNumber:  req.PowerTechNumber,
		PowerOwnerName:   req.PowerOwnerName,
		PowerScanStatus:  req.PowerScanStatus,
	}); err != nil {
		h.logger.Error("update power profile failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	d, _ := h.drivers.FindByID(c.Request.Context(), driverID)
	publishDriverUpdateToManager(h.stream, h.logger, d, "driver", driverID.String(), "driver.profile.power.patch", []string{
		"power_plate_number", "power_tech_series", "power_tech_number", "power_owner_name", "power_scan_status",
	})
	resp.OKLang(c, "updated", gin.H{"event": "updated", "driver": d})
}

type patchTrailerReq struct {
	TrailerPlateNumber *string `json:"trailer_plate_number,omitempty"`
	TrailerTechSeries  *string `json:"trailer_tech_series,omitempty"`
	TrailerTechNumber  *string `json:"trailer_tech_number,omitempty"`
	TrailerOwnerName   *string `json:"trailer_owner_name,omitempty"`
	TrailerScanStatus  *bool   `json:"trailer_scan_status,omitempty"`
}

// PATCH /v1/driver/profile/trailer
func (h *ProfileHandler) PatchTrailer(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req patchTrailerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}

	trimPtr := func(p **string) {
		if *p == nil {
			return
		}
		v := strings.TrimSpace(**p)
		if v == "" {
			*p = nil
			return
		}
		*p = &v
	}
	trimPtr(&req.TrailerPlateNumber)
	trimPtr(&req.TrailerTechSeries)
	trimPtr(&req.TrailerTechNumber)
	trimPtr(&req.TrailerOwnerName)

	if err := h.drivers.UpdateTrailerProfile(c.Request.Context(), driverID, drivers.UpdateTrailerProfile{
		TrailerPlateNumber: req.TrailerPlateNumber,
		TrailerTechSeries:  req.TrailerTechSeries,
		TrailerTechNumber:  req.TrailerTechNumber,
		TrailerOwnerName:   req.TrailerOwnerName,
		TrailerScanStatus:  req.TrailerScanStatus,
	}); err != nil {
		h.logger.Error("update trailer profile failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	d, _ := h.drivers.FindByID(c.Request.Context(), driverID)
	publishDriverUpdateToManager(h.stream, h.logger, d, "driver", driverID.String(), "driver.profile.trailer.patch", []string{
		"trailer_plate_number", "trailer_tech_series", "trailer_tech_number", "trailer_owner_name", "trailer_scan_status",
	})
	resp.OKLang(c, "updated", gin.H{"event": "updated", "driver": d})
}

// DELETE /v1/driver/profile
func (h *ProfileHandler) Delete(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	if err := h.drivers.DeleteAndArchive(c.Request.Context(), driverID); err != nil {
		if err == drivers.ErrDeleteNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
			return
		}
		h.logger.Error("delete profile failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": "ok"})
}

// UploadPhoto — POST multipart/form-data с полем "photo". Фото необязательно при регистрации; можно добавить/обновить когда угодно.
func (h *ProfileHandler) UploadPhoto(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	file, err := c.FormFile("photo")
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "photo_file_required")
		return
	}
	if file.Size > maxDriverPhotoSize {
		resp.ErrorWithDataLang(c, http.StatusBadRequest, "file_too_large", gin.H{
			"max_size_mb":    10,
			"max_size_bytes": maxDriverPhotoSize,
		})
		return
	}
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	if !allowedDriverPhotoTypes[contentType] {
		resp.ErrorLang(c, http.StatusBadRequest, "allowed_image_types")
		return
	}
	f, err := file.Open()
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "cannot_read_file")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "cannot_read_file")
		return
	}
	if err := h.drivers.UpdatePhoto(c.Request.Context(), driverID, data, contentType); err != nil {
		h.logger.Error("driver photo update failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "photo_uploaded", gin.H{"status": "ok", "event": "photo_uploaded"})
}

// GetPhoto — GET фото водителя (бинарный ответ с Content-Type). 404 если фото нет.
func (h *ProfileHandler) GetPhoto(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	data, contentType, err := h.drivers.GetPhoto(c.Request.Context(), driverID)
	if err != nil {
		if errors.Is(err, drivers.ErrNotFound) {
			resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
			return
		}
		h.logger.Error("driver get photo failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	etag := weakETagBytes(data)
	if inm := strings.TrimSpace(c.GetHeader("If-None-Match")); inm != "" && inm == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, contentType, data)
}

// DeletePhoto — DELETE фото водителя. Можно удалить когда угодно.
func (h *ProfileHandler) DeletePhoto(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	if err := h.drivers.DeletePhoto(c.Request.Context(), driverID); err != nil {
		h.logger.Error("driver delete photo failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "photo_deleted", gin.H{"status": "ok", "event": "photo_deleted"})
}
