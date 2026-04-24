package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"sarbonNew/internal/adminanalytics"
	"sarbonNew/internal/admins"
	"sarbonNew/internal/security"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/store"
	"sarbonNew/internal/util"
)

type AdminAuthHandler struct {
	logger    *zap.Logger
	repo      *admins.Repo
	jwtm      *security.JWTManager
	refresh   *store.RefreshStore
	analytics *adminanalytics.Tracker
}

func NewAdminAuthHandler(logger *zap.Logger, repo *admins.Repo, jwtm *security.JWTManager, refresh *store.RefreshStore, analytics *adminanalytics.Tracker) *AdminAuthHandler {
	return &AdminAuthHandler{logger: logger, repo: repo, jwtm: jwtm, refresh: refresh, analytics: analytics}
}

type adminLoginPasswordReq struct {
	Login    string `json:"login" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AdminAuthHandler) LoginPassword(c *gin.Context) {
	var req adminLoginPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	login := strings.TrimSpace(req.Login)
	pw := strings.TrimSpace(req.Password)
	if login == "" || pw == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}

	a, err := h.repo.FindByLogin(c.Request.Context(), login)
	if err != nil {
		if errors.Is(err, admins.ErrNotFound) {
			h.analytics.SafeTrack(c, adminanalytics.EventInput{
				EventName:  adminanalytics.EventLoginFailed,
				EventTime:  time.Now().UTC(),
				Role:       adminanalytics.RoleAdmin,
				EntityType: adminanalytics.EntityUser,
				Metadata:   map[string]any{"login": login, "login_method": "password"},
			})
			resp.ErrorLang(c, http.StatusUnauthorized, "invalid_login_or_password")
			return
		}
		h.logger.Error("admin findByLogin failed", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if strings.ToLower(strings.TrimSpace(a.Status)) != "active" {
		adminID := a.ID
		h.analytics.SafeTrack(c, adminanalytics.EventInput{
			EventName:  adminanalytics.EventLoginFailed,
			EventTime:  time.Now().UTC(),
			UserID:     &adminID,
			ActorID:    &adminID,
			Role:       adminanalytics.RoleAdmin,
			EntityType: adminanalytics.EntityUser,
			EntityID:   &adminID,
			Metadata:   map[string]any{"reason": "inactive"},
		})
		resp.ErrorLang(c, http.StatusUnauthorized, "admin_inactive")
		return
	}
	if !util.ComparePassword(a.Password, pw) {
		adminID := a.ID
		h.analytics.SafeTrack(c, adminanalytics.EventInput{
			EventName:  adminanalytics.EventLoginFailed,
			EventTime:  time.Now().UTC(),
			UserID:     &adminID,
			ActorID:    &adminID,
			Role:       adminanalytics.RoleAdmin,
			EntityType: adminanalytics.EntityUser,
			EntityID:   &adminID,
			Metadata:   map[string]any{"login_method": "password"},
		})
		resp.ErrorLang(c, http.StatusUnauthorized, "invalid_login_or_password")
		return
	}

	tokens, refreshClaims, err := h.jwtm.Issue("admin", a.ID)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "token_issue_failed")
		return
	}
	_ = h.refresh.Put(c.Request.Context(), refreshClaims.UserID, refreshClaims.JTI)
	_ = h.refresh.PutSession(c.Request.Context(), refreshClaims.UserID, refreshClaims.JTI)
	adminID := a.ID
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventLoginSuccess,
		UserID:     &adminID,
		ActorID:    &adminID,
		Role:       adminanalytics.RoleAdmin,
		EntityType: adminanalytics.EntityUser,
		EntityID:   &adminID,
		SessionID:  refreshClaims.JTI,
		Metadata:   map[string]any{"login_method": "password", "admin_type": a.Type},
	})
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventSessionStarted,
		UserID:     &adminID,
		ActorID:    &adminID,
		Role:       adminanalytics.RoleAdmin,
		EntityType: adminanalytics.EntitySession,
		SessionID:  refreshClaims.JTI,
		Metadata:   map[string]any{"auth_method": "password", "admin_type": a.Type},
	})

	resp.OKLang(c, "ok", gin.H{
		"tokens": tokens,
		"admin": gin.H{
			"id":     a.ID,
			"login":  a.Login,
			"name":   a.Name,
			"status": a.Status,
			"type":   a.Type,
		},
	})
}
