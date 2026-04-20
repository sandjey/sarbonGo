package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

type PushTokensHandler struct {
	logger *zap.Logger
	drv    *drivers.Repo
	disp   *dispatchers.Repo
}

func NewPushTokensHandler(logger *zap.Logger, drv *drivers.Repo, disp *dispatchers.Repo) *PushTokensHandler {
	return &PushTokensHandler{logger: logger, drv: drv, disp: disp}
}

type upsertPushTokenReq struct {
	PushToken string `json:"push_token" binding:"required"`
}

// Upsert registers or updates push token for current driver/dispatcher.
// POST /v1/chat/push-token
func (h *PushTokensHandler) Upsert(c *gin.Context) {
	userID, _ := c.Get(mw.CtxUserID)
	role, _ := c.Get(mw.CtxUserRole)
	uid, ok := userID.(uuid.UUID)
	if !ok || uid == uuid.Nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	roleStr, ok := role.(string)
	if !ok || strings.TrimSpace(roleStr) == "" {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	roleStr = strings.ToLower(strings.TrimSpace(roleStr))
	var req upsertPushTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	token := strings.TrimSpace(req.PushToken)
	if len(token) < 10 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	switch roleStr {
	case "driver":
		if h.drv == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if err := h.drv.UpdatePushToken(c.Request.Context(), uid, token); err != nil {
			h.logger.Error("update driver push token failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
	case "dispatcher":
		if h.disp == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if err := h.disp.UpdatePushToken(c.Request.Context(), uid, token); err != nil {
			h.logger.Error("update dispatcher push token failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	resp.OKLang(c, "updated", gin.H{"status": "ok"})
}

// Delete removes push token for current driver/dispatcher.
// DELETE /v1/chat/push-token
func (h *PushTokensHandler) Delete(c *gin.Context) {
	userID, _ := c.Get(mw.CtxUserID)
	role, _ := c.Get(mw.CtxUserRole)
	uid, ok := userID.(uuid.UUID)
	if !ok || uid == uuid.Nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	roleStr, ok := role.(string)
	if !ok || strings.TrimSpace(roleStr) == "" {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	roleStr = strings.ToLower(strings.TrimSpace(roleStr))
	switch roleStr {
	case "driver":
		if h.drv == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if err := h.drv.UpdatePushToken(c.Request.Context(), uid, ""); err != nil {
			h.logger.Error("delete driver push token failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
	case "dispatcher":
		if h.disp == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if err := h.disp.UpdatePushToken(c.Request.Context(), uid, ""); err != nil {
			h.logger.Error("delete dispatcher push token failed", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	resp.OKLang(c, "ok", gin.H{"status": "ok"})
}
