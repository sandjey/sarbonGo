package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/adminanalytics"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/displaynames"
	"sarbonNew/internal/security"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/store"
	"sarbonNew/internal/util"
)

type DispatcherRegistrationHandler struct {
	logger      *zap.Logger
	repo        *dispatchers.Repo
	displayName *displaynames.Checker
	sessions    *store.DispatcherSessionStore
	jwtm        *security.JWTManager
	refresh     *store.RefreshStore
	analytics   *adminanalytics.Tracker
}

func NewDispatcherRegistrationHandler(logger *zap.Logger, repo *dispatchers.Repo, displayName *displaynames.Checker, sessions *store.DispatcherSessionStore, jwtm *security.JWTManager, refresh *store.RefreshStore, analytics *adminanalytics.Tracker) *DispatcherRegistrationHandler {
	return &DispatcherRegistrationHandler{logger: logger, repo: repo, displayName: displayName, sessions: sessions, jwtm: jwtm, refresh: refresh, analytics: analytics}
}

type dispCompleteReq struct {
	SessionID      string `json:"session_id" binding:"required"`
	Name           string `json:"name" binding:"required"`
	Role           string `json:"role"` // CARGO_MANAGER | DRIVER_MANAGER (uppercase); обязателен только при создании новой записи
	Password       string `json:"password" binding:"required"`
	PassportSeries string `json:"passport_series" binding:"required"`
	PassportNumber string `json:"passport_number" binding:"required"`
	PINFL          string `json:"pinfl" binding:"required"`
}

func (h *DispatcherRegistrationHandler) Complete(c *gin.Context) {
	var req dispCompleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	if err := util.ValidatePassword(req.Password); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	phone, err := h.sessions.Consume(c.Request.Context(), strings.TrimSpace(req.SessionID))
	if err != nil {
		if errors.Is(err, store.ErrDispatcherSessionNotFound) {
			resp.ErrorLang(c, http.StatusUnauthorized, "session_expired_or_invalid")
			return
		}
		h.logger.Error("consume session failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	existing, err := h.repo.FindByPhone(c.Request.Context(), phone)
	if err == nil {
		id, _ := uuid.Parse(existing.ID)
		tokens, refreshClaims, err := h.jwtm.Issue("dispatcher", id)
		if err != nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "token_issue_failed")
			return
		}
		_ = h.refresh.Put(c.Request.Context(), refreshClaims.UserID, refreshClaims.JTI)
		_ = h.refresh.PutSession(c.Request.Context(), refreshClaims.UserID, refreshClaims.JTI)
		roleName := adminanalytics.NormalizeRole(derefString(existing.ManagerRole))
		h.analytics.SafeTrack(c, adminanalytics.EventInput{
			EventName:  adminanalytics.EventLoginSuccess,
			UserID:     &id,
			ActorID:    &id,
			Role:       roleName,
			EntityType: adminanalytics.EntityUser,
			EntityID:   &id,
			SessionID:  refreshClaims.JTI,
			Metadata:   map[string]any{"login_method": "registration_idempotent"},
		})
		h.analytics.SafeTrack(c, adminanalytics.EventInput{
			EventName:  adminanalytics.EventSessionStarted,
			UserID:     &id,
			ActorID:    &id,
			Role:       roleName,
			EntityType: adminanalytics.EntitySession,
			SessionID:  refreshClaims.JTI,
			Metadata:   map[string]any{"auth_method": "registration_idempotent"},
		})
		resp.OKLang(c, "login", gin.H{"status": "login", "tokens": tokens, "dispatcher": existing})
		return
	}
	if !errors.Is(err, dispatchers.ErrNotFound) {
		h.logger.Error("find by phone failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	pwHash, err := util.HashPassword(req.Password)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "password_hash_failed")
		return
	}
	name := strings.TrimSpace(req.Name)
	if errKey := validatePersonName(name); errKey != "" {
		resp.ErrorLang(c, http.StatusBadRequest, errKey)
		return
	}
	ps := strings.TrimSpace(req.PassportSeries)
	pn := strings.TrimSpace(req.PassportNumber)
	pinfl := strings.TrimSpace(req.PINFL)
	if errKey := validatePassportSeries(ps); errKey != "" {
		resp.ErrorLang(c, http.StatusBadRequest, errKey)
		return
	}
	if errKey := validatePassportNumber(pn); errKey != "" {
		resp.ErrorLang(c, http.StatusBadRequest, errKey)
		return
	}
	if errKey := validatePINFL(pinfl); errKey != "" {
		resp.ErrorLang(c, http.StatusBadRequest, errKey)
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "freelance_dispatcher_role_required")
		return
	}
	if errKey := validateFreelanceDispatcherRole(role); errKey != "" {
		resp.ErrorLang(c, http.StatusBadRequest, errKey)
		return
	}

	taken, err := h.displayName.IsTaken(c.Request.Context(), name, nil, nil)
	if err != nil {
		h.logger.Error("display name taken check failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if taken {
		resp.ErrorLang(c, http.StatusConflict, "display_name_taken")
		return
	}

	id, err := h.repo.Create(c.Request.Context(), dispatchers.CreateParams{
		Phone:          phone,
		Name:           name,
		PasswordHash:   pwHash,
		PassportSeries: ps,
		PassportNumber: pn,
		PINFL:          pinfl,
		ManagerRole:    role,
	})
	if err != nil {
		if errors.Is(err, dispatchers.ErrPhoneAlreadyRegistered) {
			resp.ErrorLang(c, http.StatusConflict, "phone_already_registered")
			return
		}
		h.logger.Error("dispatcher create failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	tokens, refreshClaims, err := h.jwtm.Issue("dispatcher", id)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "token_issue_failed")
		return
	}
	_ = h.refresh.Put(c.Request.Context(), refreshClaims.UserID, refreshClaims.JTI)
	_ = h.refresh.PutSession(c.Request.Context(), refreshClaims.UserID, refreshClaims.JTI)
	roleName := adminanalytics.NormalizeRole(role)
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventUserRegistered,
		UserID:     &id,
		ActorID:    &id,
		Role:       roleName,
		EntityType: adminanalytics.EntityUser,
		EntityID:   &id,
		Metadata:   map[string]any{"registration_source": "dispatcher"},
	})
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventLoginSuccess,
		UserID:     &id,
		ActorID:    &id,
		Role:       roleName,
		EntityType: adminanalytics.EntityUser,
		EntityID:   &id,
		SessionID:  refreshClaims.JTI,
		Metadata:   map[string]any{"login_method": "registration"},
	})
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventSessionStarted,
		UserID:     &id,
		ActorID:    &id,
		Role:       roleName,
		EntityType: adminanalytics.EntitySession,
		SessionID:  refreshClaims.JTI,
		Metadata:   map[string]any{"auth_method": "registration"},
	})

	disp, err := h.repo.FindByID(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("dispatcher reload after register failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": "registered", "tokens": tokens, "dispatcher": disp})
}
