package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/adminanalytics"
	"sarbonNew/internal/calls"
	"sarbonNew/internal/chat"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

// CallsHandler provides REST API for voice calls (session/state). Media is via WebRTC.
type CallsHandler struct {
	logger         *zap.Logger
	calls          *calls.Repo
	chatRepo       *chat.Repo
	hub            *chat.Hub
	limiter        *calls.CreateLimiter
	ringingTimeout time.Duration
	analytics      *adminanalytics.Tracker
}

func NewCallsHandler(logger *zap.Logger, callsRepo *calls.Repo, chatRepo *chat.Repo, hub *chat.Hub, limiter *calls.CreateLimiter, ringingTimeout time.Duration, analytics *adminanalytics.Tracker) *CallsHandler {
	return &CallsHandler{logger: logger, calls: callsRepo, chatRepo: chatRepo, hub: hub, limiter: limiter, ringingTimeout: ringingTimeout, analytics: analytics}
}

func (h *CallsHandler) userID(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(mw.CtxUserID)
	if !ok {
		return uuid.Nil, false
	}
	id, _ := v.(uuid.UUID)
	return id, id != uuid.Nil
}

func (h *CallsHandler) analyticsRole(c *gin.Context, userID uuid.UUID) string {
	if raw, ok := c.Get(mw.CtxUserRole); ok {
		if role, ok2 := raw.(string); ok2 {
			return adminanalytics.NormalizeRole(role)
		}
	}
	return adminanalytics.RoleUnknown
}

// ListMyCalls GET /v1/calls
func (h *CallsHandler) ListMyCalls(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	limit := getIntQuery(c, "limit", 50)
	list, err := h.calls.ListForUser(c.Request.Context(), calls.ListParams{UserID: userID, Limit: limit})
	if err != nil {
		h.logger.Error("calls list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_calls")
		return
	}
	if list == nil {
		list = []calls.Call{}
	}
	resp.OKLang(c, "ok", gin.H{"calls": list})
}

// CreateCall POST /v1/calls body: { peer_id, conversation_id?, client_request_id? }
func (h *CallsHandler) CreateCall(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	// Recover stale stuck calls first (app killed/disconnected without End).
	h.recoverUserStaleCalls(c.Request.Context(), userID)
	if h.limiter != nil {
		allowed, _, _, _ := h.limiter.Allow(c.Request.Context(), userID)
		if !allowed {
			resp.ErrorLang(c, http.StatusTooManyRequests, "rate_limited")
			return
		}
	}
	if busy, err := h.calls.HasOngoing(c.Request.Context(), userID); err == nil && busy {
		resp.ErrorLang(c, http.StatusConflict, "call_user_busy")
		return
	}
	var req struct {
		PeerID          string  `json:"peer_id" binding:"required"`
		ConversationID  *string `json:"conversation_id"`
		ClientRequestID *string `json:"client_request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	peerID, err := uuid.Parse(strings.TrimSpace(req.PeerID))
	if err != nil || peerID == uuid.Nil || peerID == userID {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_peer_id")
		return
	}
	// Recover stale states for peer too to reduce false "peer busy".
	h.recoverUserStaleCalls(c.Request.Context(), peerID)
	if busy, err := h.calls.HasOngoing(c.Request.Context(), peerID); err == nil && busy {
		resp.ErrorLang(c, http.StatusConflict, "call_peer_busy")
		return
	}

	var convID *uuid.UUID
	if req.ConversationID != nil && strings.TrimSpace(*req.ConversationID) != "" {
		u, err := uuid.Parse(strings.TrimSpace(*req.ConversationID))
		if err != nil || u == uuid.Nil {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_conversation_id")
			return
		}
		// validate access
		conv, err := h.chatRepo.GetConversation(c.Request.Context(), u, userID)
		if err != nil || conv == nil {
			resp.ErrorLang(c, http.StatusNotFound, "conversation_not_found")
			return
		}
		if conv.PeerID(userID) != peerID {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_peer_id")
			return
		}
		convID = &u
	} else {
		conv, err := h.chatRepo.GetOrCreateConversation(c.Request.Context(), userID, peerID)
		if err != nil || conv == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_conversation")
			return
		}
		convID = &conv.ID
	}

	crid := strings.TrimSpace(ptrStr(req.ClientRequestID))
	var cridPtr *string
	if crid != "" {
		cridPtr = &crid
	}
	call, err := h.calls.CreateRinging(c.Request.Context(), calls.CreateParams{
		ConversationID:  convID,
		CallerID:        userID,
		CalleeID:        peerID,
		ClientRequestID: cridPtr,
	})
	if err != nil {
		h.logger.Error("calls create", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_call")
		return
	}

	// Notify callee via chat WS (best-effort). Clients can also poll REST.
	if h.hub != nil {
		payload := map[string]any{
			"type": "call.invite",
			"data": map[string]any{
				"call": call,
			},
		}
		raw, _ := jsonMarshal(payload)
		h.hub.SendToUser(peerID, raw)
	}
	roleName := h.analyticsRole(c, userID)
	convIDStr := ""
	if call.ConversationID != nil {
		convIDStr = call.ConversationID.String()
	}
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventCallStarted,
		UserID:     &userID,
		ActorID:    &userID,
		Role:       roleName,
		EntityType: adminanalytics.EntityCall,
		EntityID:   &call.ID,
		Metadata: map[string]any{
			"callee_id":       peerID.String(),
			"conversation_id": convIDStr,
		},
	})
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"call": call})
}

func (h *CallsHandler) recoverUserStaleCalls(ctx context.Context, userID uuid.UUID) {
	list, err := h.calls.ListOngoingForUser(ctx, userID)
	if err != nil || len(list) == 0 {
		return
	}
	timeout := h.ringingTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	now := time.Now()
	for _, c := range list {
		peerID := c.CallerID
		if peerID == userID {
			peerID = c.CalleeID
		}
		switch c.Status {
		case calls.StatusRinging:
			if now.Sub(c.CreatedAt) > timeout {
				_, _ = h.calls.MissIfRingingSystem(ctx, c.ID, "recovered_timeout")
			}
		case calls.StatusActive:
			// If peer is offline, treat call as stale and auto-end.
			if h.hub != nil && !h.hub.IsOnline(peerID) {
				_, _ = h.calls.EndIfActiveSystem(ctx, c.ID, "peer_offline_recovered")
			}
		}
	}
}

// GetCall GET /v1/calls/:id
func (h *CallsHandler) GetCall(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	callID, err := uuid.Parse(c.Param("id"))
	if err != nil || callID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	call, err := h.calls.GetForUser(c.Request.Context(), callID, userID)
	if err != nil {
		if err == calls.ErrNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "call_not_found")
			return
		}
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.OKLang(c, "ok", gin.H{"call": call})
}

// AcceptCall POST /v1/calls/:id/accept
func (h *CallsHandler) AcceptCall(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	callID, err := uuid.Parse(c.Param("id"))
	if err != nil || callID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	call, err := h.calls.Accept(c.Request.Context(), callID, userID)
	if err != nil {
		switch err {
		case calls.ErrNotFound:
			resp.ErrorLang(c, http.StatusNotFound, "call_not_found")
		case calls.ErrInvalidState:
			resp.ErrorLang(c, http.StatusConflict, "call_invalid_state")
		default:
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	notifyPeer(h.hub, call, "call.accepted")
	resp.OKLang(c, "ok", gin.H{"call": call})
}

// DeclineCall POST /v1/calls/:id/decline body: { reason? }
func (h *CallsHandler) DeclineCall(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	callID, err := uuid.Parse(c.Param("id"))
	if err != nil || callID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	call, err := h.calls.Decline(c.Request.Context(), callID, userID, strings.TrimSpace(req.Reason))
	if err != nil {
		switch err {
		case calls.ErrNotFound:
			resp.ErrorLang(c, http.StatusNotFound, "call_not_found")
		case calls.ErrInvalidState:
			resp.ErrorLang(c, http.StatusConflict, "call_invalid_state")
		default:
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	notifyPeer(h.hub, call, "call.declined")
	roleName := h.analyticsRole(c, userID)
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventCallEnded,
		UserID:     &userID,
		ActorID:    &userID,
		Role:       roleName,
		EntityType: adminanalytics.EntityCall,
		EntityID:   &call.ID,
		Metadata:   map[string]any{"status": call.Status},
	})
	resp.OKLang(c, "ok", gin.H{"call": call})
}

// CancelCall POST /v1/calls/:id/cancel (caller only, before accept)
func (h *CallsHandler) CancelCall(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	callID, err := uuid.Parse(c.Param("id"))
	if err != nil || callID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	call, err := h.calls.Cancel(c.Request.Context(), callID, userID)
	if err != nil {
		switch err {
		case calls.ErrNotFound:
			resp.ErrorLang(c, http.StatusNotFound, "call_not_found")
		case calls.ErrInvalidState:
			resp.ErrorLang(c, http.StatusConflict, "call_invalid_state")
		default:
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	notifyPeer(h.hub, call, "call.cancelled")
	roleName := h.analyticsRole(c, userID)
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventCallEnded,
		UserID:     &userID,
		ActorID:    &userID,
		Role:       roleName,
		EntityType: adminanalytics.EntityCall,
		EntityID:   &call.ID,
		Metadata:   map[string]any{"status": call.Status},
	})
	resp.OKLang(c, "ok", gin.H{"call": call})
}

// EndCall POST /v1/calls/:id/end body: { reason? } (ACTIVE only)
func (h *CallsHandler) EndCall(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	callID, err := uuid.Parse(c.Param("id"))
	if err != nil || callID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	call, err := h.calls.End(c.Request.Context(), callID, userID, strings.TrimSpace(req.Reason))
	if err != nil {
		switch err {
		case calls.ErrNotFound:
			resp.ErrorLang(c, http.StatusNotFound, "call_not_found")
		case calls.ErrInvalidState:
			resp.ErrorLang(c, http.StatusConflict, "call_invalid_state")
		default:
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	notifyPeer(h.hub, call, "call.ended")
	roleName := h.analyticsRole(c, userID)
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventCallEnded,
		UserID:     &userID,
		ActorID:    &userID,
		Role:       roleName,
		EntityType: adminanalytics.EntityCall,
		EntityID:   &call.ID,
		Metadata:   map[string]any{"status": call.Status},
	})
	resp.OKLang(c, "ok", gin.H{"call": call})
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// jsonMarshal isolates json import usage to avoid repeating in handlers.
func jsonMarshal(v any) ([]byte, error) {
	// local inline import pattern avoided; we declare here for clarity.
	return json.Marshal(v)
}

func notifyPeer(hub *chat.Hub, call *calls.Call, eventType string) {
	if hub == nil || call == nil {
		return
	}
	if call.CallerID == call.CalleeID {
		return
	}
	// Default: send to caller; if caller is current updater in practice, clients still can ignore duplicates.
	// We don't have actor_id here; for correctness, send to both participants.
	payload := map[string]any{
		"type": eventType,
		"data": map[string]any{"call": call},
	}
	raw, _ := jsonMarshal(payload)
	hub.SendToUser(call.CallerID, raw)
	hub.SendToUser(call.CalleeID, raw)
}
