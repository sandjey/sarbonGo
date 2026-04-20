package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/config"
	"sarbonNew/internal/push"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/tripnotif"
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
			"push_notifications_enabled":         h.cfg.PushNotificationsEnabled,
			"firebase_project_id_configured":     strings.TrimSpace(h.cfg.FirebaseProjectID) != "",
			"firebase_project_id":                strings.TrimSpace(h.cfg.FirebaseProjectID),
			"firebase_credentials_file_raw":      credRaw,
			"firebase_credentials_path_resolved": resolved,
			"firebase_credentials_file_found":    credFound,
			"push_service_initialized":           h.pushSvc != nil,
		},
	})
}

type sendPushReq struct {
	PushToken string            `json:"push_token" binding:"required"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Data      map[string]string `json:"data"`
	// Optional: if set together, the token will also be persisted for this user
	// (drivers.push_token or freelance_dispatchers.push_token) so the regular
	// system flow (cargo offers, chat, …) starts delivering push for them.
	RecipientKind string `json:"recipient_kind,omitempty"`
	RecipientID   string `json:"recipient_id,omitempty"`
}

// SendTest godoc
// POST /v1/admin/push/send
func (h *PushAdminHandler) SendTest(c *gin.Context) {
	if h.pushSvc == nil {
		h.logger.Warn("admin push send: push service nil (push.New failed at startup — see logs push init failed)")
		resp.ErrorWithDataLang(c, http.StatusServiceUnavailable, "push_not_enabled", gin.H{
			"reason":  "push_service_nil",
			"hint":    "Check server logs for push init failed; fix FIREBASE_CREDENTIALS_FILE / JSON and restart.",
			"enabled": false,
			"same_as": "GET /v1/admin/push/status would show enabled=false",
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

	tokenSaved := false
	var saveErr string
	if strings.TrimSpace(req.RecipientKind) != "" && strings.TrimSpace(req.RecipientID) != "" {
		kind := strings.ToLower(strings.TrimSpace(req.RecipientKind))
		if kind != tripnotif.RecipientDriver && kind != tripnotif.RecipientDispatcher {
			saveErr = "recipient_kind must be 'driver' or 'dispatcher'"
		} else if rid, err := uuid.Parse(strings.TrimSpace(req.RecipientID)); err != nil || rid == uuid.Nil {
			saveErr = "recipient_id must be a non-zero UUID"
		} else if err := h.pushSvc.SaveRecipientToken(c.Request.Context(), kind, rid, req.PushToken); err != nil {
			saveErr = err.Error()
			h.logger.Warn("admin push test save token failed", zap.Error(err), zap.String("kind", kind), zap.String("id", rid.String()))
		} else {
			tokenSaved = true
			h.logger.Info("admin push test saved token for recipient", zap.String("kind", kind), zap.String("id", rid.String()))
		}
	}

	resp.OKLang(c, "ok", gin.H{
		"sent":             true,
		"token":            req.PushToken,
		"fcm_message_id":   msgID,
		"firebase_project": strings.TrimSpace(h.cfg.FirebaseProjectID),
		"token_saved":      tokenSaved,
		"token_save_error": saveErr,
	})
}

// RecipientStatus godoc
// GET /v1/admin/push/recipient-status?kind=driver|dispatcher&id=<uuid>
// Explains why system flow might not push to a given user: shows if push_token is present in DB.
func (h *PushAdminHandler) RecipientStatus(c *gin.Context) {
	if h.pushSvc == nil || !h.pushSvc.Enabled() {
		resp.ErrorWithDataLang(c, http.StatusServiceUnavailable, "push_not_enabled", gin.H{"enabled": false})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(c.Query("kind")))
	if kind != tripnotif.RecipientDriver && kind != tripnotif.RecipientDispatcher {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	rid, err := uuid.Parse(strings.TrimSpace(c.Query("id")))
	if err != nil || rid == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	info := h.pushSvc.InspectRecipient(c.Request.Context(), kind, rid)
	resp.OKLang(c, "ok", gin.H{
		"kind":         info.Kind,
		"id":           info.ID.String(),
		"token_found":  info.TokenFound,
		"token_length": info.TokenLen,
		"token_prefix": info.TokenPrefix,
		"source":       info.Source,
	})
}

type sendByRecipientReq struct {
	Kind  string            `json:"kind" binding:"required"`
	ID    string            `json:"id" binding:"required"`
	Title string            `json:"title"`
	Body  string            `json:"body"`
	Data  map[string]string `json:"data"`
}

// SendByRecipient godoc
// POST /v1/admin/push/send-by-recipient
// Sends a push using the SAME internal flow the backend uses for real events (resolves token
// from drivers/freelance_dispatchers by id). If the endpoint returns token_found=false, the
// corresponding mobile app did not register its FCM token via POST /v1/chat/push-token.
func (h *PushAdminHandler) SendByRecipient(c *gin.Context) {
	if h.pushSvc == nil || !h.pushSvc.Enabled() {
		resp.ErrorWithDataLang(c, http.StatusServiceUnavailable, "push_not_enabled", gin.H{"enabled": false})
		return
	}
	var req sendByRecipientReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != tripnotif.RecipientDriver && kind != tripnotif.RecipientDispatcher {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	rid, err := uuid.Parse(strings.TrimSpace(req.ID))
	if err != nil || rid == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Sarbon"
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "System push (admin trigger)"
	}
	res, err := h.pushSvc.SendByRecipient(c.Request.Context(), kind, rid, title, body, req.Data)
	if err != nil {
		h.logger.Warn("admin send-by-recipient failed", zap.Error(err), zap.String("kind", kind), zap.String("id", rid.String()), zap.String("source", res.Source))
		resp.ErrorWithData(c, http.StatusBadGateway, "firebase error: "+err.Error(), gin.H{
			"kind":         kind,
			"id":           rid.String(),
			"token_found":  res.TokenFound,
			"token_prefix": res.TokenPrefix,
			"source":       res.Source,
		})
		return
	}
	h.logger.Info("admin send-by-recipient sent",
		zap.String("kind", kind),
		zap.String("id", rid.String()),
		zap.String("source", res.Source),
		zap.String("fcm_message_id", res.FCMMessageID),
	)
	resp.OKLang(c, "ok", gin.H{
		"sent":             true,
		"kind":             kind,
		"id":               rid.String(),
		"token_found":      res.TokenFound,
		"token_prefix":     res.TokenPrefix,
		"source":           res.Source,
		"fcm_message_id":   res.FCMMessageID,
		"firebase_project": strings.TrimSpace(h.cfg.FirebaseProjectID),
	})
}
