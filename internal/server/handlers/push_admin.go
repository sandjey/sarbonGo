package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"sarbonNew/internal/config"
	"sarbonNew/internal/push"
	"sarbonNew/internal/server/resp"
)

// PushAdminHandler exposes Firebase push diagnostic and test-send endpoints for admin.
type PushAdminHandler struct {
	logger  *zap.Logger
	pushSvc *push.Service
	cfg     config.Config
}

func NewPushAdminHandler(logger *zap.Logger, pushSvc *push.Service, cfg config.Config) *PushAdminHandler {
	return &PushAdminHandler{logger: logger, pushSvc: pushSvc, cfg: cfg}
}

func resolveCredentialsPath(raw string) (resolved string, found bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	p := raw
	if !filepath.IsAbs(p) {
		if wd, err := os.Getwd(); err == nil {
			p = filepath.Clean(filepath.Join(wd, p))
		}
	} else {
		p = filepath.Clean(p)
	}
	_, err := os.Stat(p)
	return p, err == nil
}

// Status godoc
// GET /v1/admin/push/status
func (h *PushAdminHandler) Status(c *gin.Context) {
	enabled := h.pushSvc != nil && h.pushSvc.Enabled()
	projectID := ""
	if h.pushSvc != nil {
		projectID = h.pushSvc.ProjectID()
	}
	credRaw := strings.TrimSpace(h.cfg.FirebaseCredentialsFile)
	resolved, credFound := resolveCredentialsPath(credRaw)
	resp.OKLang(c, "ok", gin.H{
		"enabled":    enabled,
		"project_id": projectID,
		"config": gin.H{
			"push_notifications_enabled":       h.cfg.PushNotificationsEnabled,
			"firebase_project_id_configured":   strings.TrimSpace(h.cfg.FirebaseProjectID) != "",
			"firebase_project_id":              strings.TrimSpace(h.cfg.FirebaseProjectID),
			"firebase_credentials_file_raw":  credRaw,
			"firebase_credentials_path_resolved": resolved,
			"firebase_credentials_file_found": credFound,
			"push_service_initialized":         h.pushSvc != nil,
		},
	})
}

type sendPushReq struct {
	PushToken string            `json:"push_token" binding:"required"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Data      map[string]string `json:"data"`
}

// SendTest godoc
// POST /v1/admin/push/send
func (h *PushAdminHandler) SendTest(c *gin.Context) {
	if h.pushSvc == nil {
		h.logger.Warn("admin push send: push service nil (push.New failed at startup — see logs push init failed)")
		resp.ErrorWithDataLang(c, http.StatusServiceUnavailable, "push_not_enabled", gin.H{
			"reason":   "push_service_nil",
			"hint":     "Check server logs for push init failed; fix FIREBASE_CREDENTIALS_FILE / JSON and restart.",
			"enabled":  false,
			"same_as":  "GET /v1/admin/push/status would show enabled=false",
		})
		return
	}
	if !h.pushSvc.Enabled() {
		h.logger.Warn("admin push send: FCM client not initialized (PUSH_NOTIFICATIONS_ENABLED or Firebase env incomplete)")
		resp.ErrorWithDataLang(c, http.StatusServiceUnavailable, "push_not_enabled", gin.H{
			"reason":     "fcm_client_nil",
			"hint":       "Set PUSH_NOTIFICATIONS_ENABLED=true, FIREBASE_PROJECT_ID, FIREBASE_CREDENTIALS_FILE (absolute path); restart.",
			"enabled":    false,
			"project_id": h.pushSvc.ProjectID(),
		})
		return
	}
	var req sendPushReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	req.PushToken = strings.TrimSpace(req.PushToken)
	if len(req.PushToken) < 10 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Sarbon Test"
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "Test push notification from admin"
	}
	msgID, err := h.pushSvc.SendTestToToken(c.Request.Context(), req.PushToken, title, body, req.Data)
	if err != nil {
		h.logger.Warn("admin push test failed", zap.Error(err))
		resp.ErrorWithData(c, http.StatusBadGateway, "firebase error: "+err.Error(), nil)
		return
	}
	prefixLen := 24
	if len(req.PushToken) < prefixLen {
		prefixLen = len(req.PushToken)
	}
	h.logger.Info("admin push test sent", zap.String("fcm_message_id", msgID), zap.String("token_prefix", req.PushToken[:prefixLen]))
	resp.OKLang(c, "ok", gin.H{
		"sent":             true,
		"token":            req.PushToken,
		"fcm_message_id":   msgID,
		"firebase_project": strings.TrimSpace(h.cfg.FirebaseProjectID),
	})
}
