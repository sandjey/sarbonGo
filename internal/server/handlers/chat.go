package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"sarbonNew/internal/adminanalytics"
	"sarbonNew/internal/chat"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// chatAttachmentPublicPath is the URL path clients use to download chat
// attachments (GET with X-User-Token). Prefer "media" over "files" so
// reverse proxies are less likely to intercept the segment `/files/` as static.
const chatAttachmentPublicPath = "/v1/chat/media"

type ChatHandler struct {
	logger      *zap.Logger
	repo        *chat.Repo
	presence    *chat.PresenceStore
	hub         *chat.Hub
	drivers     *drivers.Repo
	dispatchers *dispatchers.Repo
	analytics   *adminanalytics.Tracker
	// test hooks for file endpoint
	getAttachmentForUserFn func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error)
	getMediaFileByIDFn     func(ctx context.Context, id uuid.UUID) (*chat.MediaFile, error)
}

func NewChatHandler(logger *zap.Logger, repo *chat.Repo, presence *chat.PresenceStore, hub *chat.Hub, drv *drivers.Repo, disp *dispatchers.Repo, analytics *adminanalytics.Tracker) *ChatHandler {
	h := &ChatHandler{logger: logger, repo: repo, presence: presence, hub: hub, drivers: drv, dispatchers: disp, analytics: analytics}
	hub.SetOnTyping(func(conversationID, fromUserID uuid.UUID) (uuid.UUID, bool) {
		ctx := context.Background()
		conv, err := repo.GetConversation(ctx, conversationID, fromUserID)
		if err != nil || conv == nil {
			return uuid.Nil, false
		}
		return conv.PeerID(fromUserID), true
	})

	// When user opens a socket: flip presence on for their chat peers AND
	// drain undelivered messages addressed to them (ack back to every sender).
	hub.SetOnUserConnected(func(userID uuid.UUID) {
		ctx := context.Background()
		if acks, err := repo.MarkUndeliveredForUser(ctx, userID); err != nil {
			logger.Debug("chat: mark undelivered on connect", zap.Error(err), zap.String("user_id", userID.String()))
		} else {
			for _, a := range acks {
				hub.BroadcastMessageDelivered(a.SenderID, a.ConversationID, a.MessageIDs, a.DeliveredAt)
			}
		}
		peers, err := repo.ListConversationPeers(ctx, userID)
		if err != nil {
			logger.Debug("chat: list peers on connect", zap.Error(err))
			return
		}
		for _, peerID := range peers {
			hub.BroadcastPresence(peerID, userID, true, 0)
		}
	})

	// When user's last socket closes: push offline+last_seen to every peer.
	hub.SetOnUserDisconnected(func(userID uuid.UUID) {
		ctx := context.Background()
		var lastSeen int64
		if presence != nil {
			if ls, err := presence.LastSeen(ctx, userID); err == nil && ls > 0 {
				lastSeen = ls
			}
		}
		peers, err := repo.ListConversationPeers(ctx, userID)
		if err != nil {
			logger.Debug("chat: list peers on disconnect", zap.Error(err))
			return
		}
		for _, peerID := range peers {
			hub.BroadcastPresence(peerID, userID, false, lastSeen)
		}
	})

	return h
}

func (h *ChatHandler) getUserID(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(mw.CtxUserID)
	if !ok {
		return uuid.Nil, false
	}
	id, _ := v.(uuid.UUID)
	return id, id != uuid.Nil
}

func (h *ChatHandler) analyticsRoleForChat(c *gin.Context, userID uuid.UUID) string {
	if c == nil {
		return adminanalytics.RoleUnknown
	}
	if raw, ok := c.Get(mw.CtxUserRole); ok {
		if role, ok2 := raw.(string); ok2 {
			if strings.EqualFold(role, "dispatcher") {
				if mr, err := currentDispatcherManagerRole(c.Request.Context(), h.dispatchers, userID); err == nil && strings.TrimSpace(mr) != "" {
					return adminanalytics.NormalizeRole(mr)
				}
			}
			return adminanalytics.NormalizeRole(role)
		}
	}
	return adminanalytics.RoleUnknown
}

func (h *ChatHandler) getAttachmentForUser(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
	if h.getAttachmentForUserFn != nil {
		return h.getAttachmentForUserFn(ctx, attachmentID, userID)
	}
	return h.repo.GetAttachmentForUser(ctx, attachmentID, userID)
}

func (h *ChatHandler) getMediaFileByID(ctx context.Context, id uuid.UUID) (*chat.MediaFile, error) {
	if h.getMediaFileByIDFn != nil {
		return h.getMediaFileByIDFn(ctx, id)
	}
	return h.repo.GetMediaFileByID(ctx, id)
}

// currentUserIDForChat resolves user id from chat JWT or driver/dispatcher scoped routes.
func (h *ChatHandler) currentUserIDForChat(c *gin.Context) (uuid.UUID, bool) {
	if id, ok := h.getUserID(c); ok {
		return id, true
	}
	if v, ok := c.Get(mw.CtxDriverID); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	if v, ok := c.Get(mw.CtxDispatcherID); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

// Chat media size caps per message type. Keep in sync with docs/openapi.yaml.
// Limits apply to the incoming raw upload — final stored object may be smaller
// after ffmpeg reencoding (video/voice).
const (
	chatMaxPhotoBytes = 15 << 20  // 15 MiB
	chatMaxVideoBytes = 200 << 20 // 200 MiB
	chatMaxVoiceBytes = 50 << 20  // 50 MiB

	// Telegram-style duration cap: voice messages, regular video and video
	// notes are all hard-limited to 60 seconds. Avoids multi-minute clips
	// chewing CPU on ffmpeg and gigabytes of disk.
	chatMaxMediaDurationMs = 60_000
)

// chatMediaLimitForType returns the byte cap for the given message type.
func chatMediaLimitForType(msgType string) int64 {
	switch chat.NormalizeMessageType(msgType) {
	case chat.MsgTypeImg:
		return chatMaxPhotoBytes
	case chat.MsgTypeVideo, chat.MsgTypeVideoNote:
		return chatMaxVideoBytes
	case chat.MsgTypeAudio:
		return chatMaxVoiceBytes
	default:
		return chatMaxPhotoBytes
	}
}

// chatMediaDurationCapMs returns the max allowed duration for a media message
// type, or 0 if the type has no duration (photo).
func chatMediaDurationCapMs(msgType string) int {
	switch chat.NormalizeMessageType(msgType) {
	case chat.MsgTypeAudio, chat.MsgTypeVideo, chat.MsgTypeVideoNote:
		return chatMaxMediaDurationMs
	}
	return 0
}

// markDeliveredIfPeerOnline stamps delivered_at = now() on the just-created
// message if the recipient has a live WS session, and sends message_delivered
// back to the sender so their UI flips to "double-check". Mutates msg in place.
func (h *ChatHandler) markDeliveredIfPeerOnline(ctx context.Context, conv *chat.Conversation, msg *chat.Message) {
	if conv == nil || msg == nil {
		return
	}
	peerID := conv.PeerID(msg.SenderID)
	if peerID == uuid.Nil {
		return
	}
	online := h.hub != nil && h.hub.IsOnline(peerID)
	if !online && h.presence != nil {
		ok, err := h.presence.IsOnline(ctx, peerID)
		if err == nil && ok {
			online = true
		}
	}
	if !online {
		return
	}
	ts, err := h.repo.MarkMessageDelivered(ctx, msg.ID)
	if err != nil || ts == nil {
		return
	}
	msg.DeliveredAt = ts
	h.hub.BroadcastMessageDelivered(msg.SenderID, msg.ConversationID, []uuid.UUID{msg.ID}, *ts)
}

func chatLastMessagePreview(msgType string, body *string) string {
	if body != nil {
		s := strings.TrimSpace(*body)
		if s != "" {
			return s
		}
	}
	switch chat.NormalizeMessageType(msgType) {
	case chat.MsgTypeImg:
		return "Photo"
	case chat.MsgTypeAudio:
		return "Voice"
	case chat.MsgTypeVideo:
		return "Video"
	case chat.MsgTypeVideoNote:
		return "Video message"
	case chat.MsgTypeLocation:
		return "Location"
	default:
		return ""
	}
}

// ListConversations returns Telegram-like rows for the current user (peer profile, last message, unread).
// GET /v1/chat/conversations
func (h *ChatHandler) ListConversations(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	limit := getIntQuery(c, "limit", 50)
	list, err := h.repo.ListConversationsEnriched(c.Request.Context(), userID, limit)
	if err != nil {
		h.logger.Error("chat list conversations", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_conversations")
		return
	}
	for i := range list {
		if list[i].LastMessageType != nil {
			s := chat.CanonicalMessageTypeForAPI(*list[i].LastMessageType)
			list[i].LastMessageType = &s
		}
		list[i].LastMessagePreview = chatLastMessagePreview(
			strings.TrimSpace(chatStrPtr(list[i].LastMessageType)),
			list[i].LastMessageBody,
		)
		if list[i].PeerHasPhoto {
			u := "/v1/chat/users/" + list[i].PeerID.String() + "/photo"
			list[i].PeerPhotoURL = &u
		}
	}
	resp.OKLang(c, "ok", gin.H{"conversations": list})
}

func chatStrPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// UserFinder searches drivers and freelance dispatchers by phone prefix (typeahead).
// GET /v1/chat/user-finder | /v1/driver/user-finder | /v1/dispatchers/user-finder
func (h *ChatHandler) UserFinder(c *gin.Context) {
	userID, ok := h.currentUserIDForChat(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	if h.drivers == nil || h.dispatchers == nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	phone := strings.TrimSpace(c.Query("phone"))
	if phone == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "phone_required")
		return
	}
	limit := getIntQuery(c, "limit", 20)
	if limit < 1 || limit > 50 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload")
		return
	}
	half := limit / 2
	if half < 1 {
		half = 1
	}
	ctx := c.Request.Context()
	drvList, err := h.drivers.HintByPhonePrefix(ctx, phone, half+limit)
	if err != nil {
		h.logger.Error("user finder drivers", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_user_finder")
		return
	}
	dispList, err := h.dispatchers.HintByPhonePrefix(ctx, phone, half+limit)
	if err != nil {
		h.logger.Error("user finder dispatchers", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_user_finder")
		return
	}
	seen := map[uuid.UUID]struct{}{userID: {}}
	items := make([]gin.H, 0, limit)
	for _, d := range drvList {
		if d == nil {
			continue
		}
		drvID, err := uuid.Parse(d.ID)
		if err != nil {
			continue
		}
		if drvID == userID {
			continue
		}
		if _, ok := seen[drvID]; ok {
			continue
		}
		seen[drvID] = struct{}{}
		name := ""
		if d.Name != nil {
			name = strings.TrimSpace(*d.Name)
		}
		if name == "" {
			name = d.Phone
		}
		row := gin.H{
			"role":      "driver",
			"id":        drvID,
			"phone":     d.Phone,
			"name":      name,
			"has_photo": d.HasPhoto,
		}
		if d.HasPhoto {
			row["photo_url"] = "/v1/chat/users/" + drvID.String() + "/photo"
		}
		items = append(items, row)
		if len(items) >= limit {
			break
		}
	}
	for _, d := range dispList {
		if len(items) >= limit {
			break
		}
		dispID, err := uuid.Parse(d.ID)
		if err != nil {
			continue
		}
		if dispID == userID {
			continue
		}
		if _, ok := seen[dispID]; ok {
			continue
		}
		seen[dispID] = struct{}{}
		name := ""
		if d.Name != nil {
			name = strings.TrimSpace(*d.Name)
		}
		if name == "" {
			name = d.Phone
		}
		row := gin.H{
			"role":      "dispatcher",
			"id":        dispID,
			"phone":     d.Phone,
			"name":      name,
			"has_photo": d.HasPhoto,
		}
		if d.HasPhoto {
			row["photo_url"] = "/v1/chat/users/" + dispID.String() + "/photo"
		}
		items = append(items, row)
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

// MarkConversationRead marks all messages in the conversation as read for the current user.
// POST /v1/chat/conversations/:id/read
func (h *ChatHandler) MarkConversationRead(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	convID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	if err := h.repo.MarkConversationRead(c.Request.Context(), convID, userID); err != nil {
		if errors.Is(err, chat.ErrNotFound) {
			resp.ErrorLang(c, http.StatusNotFound, "conversation_not_found")
			return
		}
		h.logger.Error("chat mark read", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_mark_read")
		return
	}
	if conv, err := h.repo.GetConversation(c.Request.Context(), convID, userID); err == nil && conv != nil {
		h.hub.SendConversationReadEvent(conv.PeerID(userID), convID, userID)
	}
	resp.OKLang(c, "ok", gin.H{"read": true})
}

// GetPeerPhoto serves peer avatar for chat UI (authenticated driver/dispatcher).
// GET /v1/chat/users/:id/photo
func (h *ChatHandler) GetPeerPhoto(c *gin.Context) {
	_, ok := h.currentUserIDForChat(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	if h.drivers == nil || h.dispatchers == nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	peerID, err := uuid.Parse(c.Param("id"))
	if err != nil || peerID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_user_id")
		return
	}
	ctx := c.Request.Context()
	data, ct, err := h.drivers.GetPhoto(ctx, peerID)
	if err == nil && len(data) > 0 {
		c.Data(http.StatusOK, ct, data)
		return
	}
	if err != nil && !errors.Is(err, drivers.ErrNotFound) {
		h.logger.Error("chat peer photo driver", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	data, ct, err = h.dispatchers.GetPhoto(ctx, peerID)
	if err == nil && len(data) > 0 {
		c.Data(http.StatusOK, ct, data)
		return
	}
	if err != nil && !errors.Is(err, dispatchers.ErrNotFound) {
		h.logger.Error("chat peer photo dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
}

// GetOrCreateConversation gets or creates a conversation with peer_id.
// POST /v1/chat/conversations body: { "peer_id": "uuid" }
func (h *ChatHandler) GetOrCreateConversation(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	var req struct {
		PeerID string `json:"peer_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	peerID, err := uuid.Parse(req.PeerID)
	if err != nil || peerID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_peer_id")
		return
	}
	conv, err := h.repo.GetOrCreateConversation(c.Request.Context(), userID, peerID)
	if err != nil {
		if err == chat.ErrSameUser {
			resp.ErrorLang(c, http.StatusBadRequest, "cannot_chat_with_yourself")
			return
		}
		h.logger.Error("chat get or create conversation", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_conversation")
		return
	}
	peerID = conv.PeerID(userID)
	resp.OKLang(c, "ok", gin.H{
		"id":         conv.ID,
		"peer_id":    peerID,
		"created_at": conv.CreatedAt,
	})
}

// ListMessages returns messages for a conversation (paginated by cursor).
// GET /v1/chat/conversations/:id/messages?limit=20&cursor=uuid
func (h *ChatHandler) ListMessages(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	convIDStr := c.Param("id")
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	var cursor *uuid.UUID
	if c := c.Query("cursor"); c != "" {
		u, err := uuid.Parse(c)
		if err == nil {
			cursor = &u
		}
	}
	limit := getIntQuery(c, "limit", 50)
	list, err := h.repo.ListMessages(c.Request.Context(), convID, userID, cursor, limit)
	if err != nil {
		h.logger.Error("chat list messages", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_messages")
		return
	}
	for i := range list {
		list[i].Type = chat.CanonicalMessageTypeForAPI(list[i].Type)
	}
	mark := strings.TrimSpace(c.Query("mark_read"))
	if mark == "1" || strings.EqualFold(mark, "true") {
		if err := h.repo.MarkConversationRead(c.Request.Context(), convID, userID); err != nil && !errors.Is(err, chat.ErrNotFound) {
			h.logger.Error("chat mark read on list", zap.Error(err))
		} else if err == nil {
			if conv, e := h.repo.GetConversation(c.Request.Context(), convID, userID); e == nil && conv != nil {
				h.hub.SendConversationReadEvent(conv.PeerID(userID), convID, userID)
			}
		}
	}
	resp.OKLang(c, "ok", gin.H{"messages": list})
}

// SendMessage creates a message and broadcasts via WebSocket.
// POST /v1/chat/conversations/:id/messages
//
//   - Content-Type: application/json
//       • Text: `{ "body": "..." }` or `{ "type": "text", "body": "..." }`
//       • Reuse cached media (no separate /media-ref required): `{ "type": "img"|"audio"|"video"|"video_note", "sha256": "<64 hex>", "body": "<optional caption>" }`
//   - Content-Type: multipart/form-data — same as POST .../messages/media (fields type, body?, file)
func (h *ChatHandler) SendMessage(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	convIDStr := c.Param("id")
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	conv, err := h.repo.GetConversation(c.Request.Context(), convID, userID)
	if err != nil || conv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "conversation_not_found")
		return
	}

	ct := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if strings.HasPrefix(ct, "multipart/form-data") {
		h.SendMediaMessage(c)
		return
	}

	var req struct {
		Type   string `json:"type"`
		Body   string `json:"body"`
		SHA256 string `json:"sha256"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	msgKind := chat.NormalizeMessageType(req.Type)
	if msgKind == "" {
		msgKind = chat.MsgTypeText
	}
	if msgKind == chat.MsgTypeText && strings.TrimSpace(req.SHA256) != "" {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	switch msgKind {
	case chat.MsgTypeText:
		body := strings.TrimSpace(req.Body)
		if body == "" {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		if len(body) > 64*1024 {
			resp.ErrorLang(c, http.StatusBadRequest, "message_too_long")
			return
		}
		msg, err := h.repo.CreateTextMessage(c.Request.Context(), convID, userID, body)
		if err != nil {
			h.logger.Error("chat create message", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
			return
		}
		msg.Type = chat.CanonicalMessageTypeForAPI(msg.Type)
		h.markDeliveredIfPeerOnline(c.Request.Context(), conv, msg)
		h.hub.BroadcastMessage(conv.UserAID, conv.UserBID, msg)
		roleName := h.analyticsRoleForChat(c, userID)
		h.analytics.SafeTrack(c, adminanalytics.EventInput{
			EventName:  adminanalytics.EventChatMessageSent,
			UserID:     &userID,
			ActorID:    &userID,
			Role:       roleName,
			EntityType: adminanalytics.EntityChatMessage,
			EntityID:   &msg.ID,
			Metadata: map[string]any{
				"conversation_id": convID.String(),
				"message_type":    msg.Type,
			},
		})
		resp.SuccessLang(c, http.StatusCreated, "ok", msg)
	case chat.MsgTypeImg, chat.MsgTypeAudio, chat.MsgTypeVideo, chat.MsgTypeVideoNote:
		sha := strings.ToLower(strings.TrimSpace(req.SHA256))
		if len(sha) != 64 {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
		var caption *string
		if v := strings.TrimSpace(req.Body); v != "" {
			if len(v) > 64*1024 {
				resp.ErrorLang(c, http.StatusBadRequest, "message_too_long")
				return
			}
			caption = &v
		}
		mapping, mErr := h.repo.GetSourceMapping(c.Request.Context(), sha)
		if mErr != nil {
			h.logger.Error("chat send message media-ref", zap.Error(mErr))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if mapping == nil || !chat.MessageKindsEqual(mapping.Kind, msgKind) {
			resp.ErrorLang(c, http.StatusNotFound, "chat_media_not_cached")
			return
		}
		if sent := h.sendFromSourceMapping(c, conv, userID, convID, msgKind, caption, mapping); !sent {
			resp.ErrorLang(c, http.StatusNotFound, "chat_media_not_cached")
		}
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
	}
}

type mediaLinks struct {
	Media string `json:"media"`
	Thumb string `json:"thumb,omitempty"`
}

// mediaMsgPayload is stored in chat_messages.payload. Links duplicate url /
// thumb_url so clients can render media from a single JSON object without
// extra "attachment resolve" APIs.
type mediaMsgPayload struct {
	AttachmentID string      `json:"attachment_id"`
	Link         string      `json:"link"`
	URL          string      `json:"url"`
	ThumbURL     string      `json:"thumb_url,omitempty"`
	Links        *mediaLinks `json:"links,omitempty"`
	Mime         string      `json:"mime"`
	SizeBytes    int64       `json:"size_bytes"`
	DurationMs   *int        `json:"duration_ms,omitempty"`
	Width        *int        `json:"width,omitempty"`
	Height       *int        `json:"height,omitempty"`
}

// SendMediaMessage uploads media (photo/voice/video/video_note), creates message and broadcasts.
// POST /v1/chat/conversations/:id/messages/media (multipart/form-data)
func (h *ChatHandler) SendMediaMessage(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	convID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	conv, err := h.repo.GetConversation(c.Request.Context(), convID, userID)
	if err != nil || conv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "conversation_not_found")
		return
	}

	if err := c.Request.ParseMultipartForm(128 << 20); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	msgType := chat.NormalizeMessageType(c.PostForm("type"))
	switch msgType {
	case chat.MsgTypeImg, chat.MsgTypeAudio, chat.MsgTypeVideo, chat.MsgTypeVideoNote:
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}

	var caption *string
	if v := strings.TrimSpace(c.PostForm("body")); v != "" {
		if len(v) > 64*1024 {
			resp.ErrorLang(c, http.StatusBadRequest, "message_too_long")
			return
		}
		caption = &v
	}

	fh, ok := chatPickMultipartFile(c)
	if !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "chat_media_file_required")
		return
	}

	// Enforce per-type size caps BEFORE we spool the upload to disk / ffmpeg.
	if limit := chatMediaLimitForType(msgType); fh.Size > limit {
		resp.ErrorLang(c, http.StatusRequestEntityTooLarge, "chat_media_too_large")
		return
	}

	// Soft-check the declared MIME/extension matches the message type; ffmpeg
	// will still reject broken files, but a quick reject here gives a clean 400
	// and avoids burning CPU on obviously wrong uploads (e.g. mp3 as VIDEO).
	if !chatMediaTypeLooksRight(msgType, fh.Filename, fh.Header.Get("Content-Type")) {
		resp.ErrorLang(c, http.StatusBadRequest, "chat_media_type_not_supported")
		return
	}

	storageRoot := strings.TrimSpace(os.Getenv("CHAT_STORAGE_DIR"))
	if storageRoot == "" {
		storageRoot = "storage"
	}
	baseDir := filepath.Join(storageRoot, "chat", convID.String())
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		h.logger.Error("chat mkdir", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	inPath := filepath.Join(baseDir, "upload_"+uuid.New().String()+"_"+filepath.Base(fh.Filename))
	src, err := fh.Open()
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "cannot_read_file")
		return
	}
	defer src.Close()
	dst, err := os.Create(inPath)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	defer func() { _ = os.Remove(inPath) }()
	// Hash the ORIGINAL bytes (pre-ffmpeg) while we stream them to disk.
	// This source hash is the cache key for chat_source_hashes: if the same
	// file was processed before (possibly by someone else), we skip the whole
	// ffmpeg pipeline and reuse the existing media_file.
	sourceHasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, sourceHasher), src); err != nil {
		_ = dst.Close()
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := dst.Close(); err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	sourceHash := hex.EncodeToString(sourceHasher.Sum(nil))

	// Fast-path: identical bytes were processed before → reuse.
	// Kind must match (same source reused as a different message type is not
	// a valid cache hit, e.g. someone sending the same mp4 as VIDEO_NOTE vs
	// VIDEO would produce different processed outputs).
	if mapping, mErr := h.repo.GetSourceMapping(c.Request.Context(), sourceHash); mErr == nil && mapping != nil && chat.MessageKindsEqual(mapping.Kind, msgType) {
		if sent := h.sendFromSourceMapping(c, conv, userID, convID, msgType, caption, mapping); sent {
			return
		}
		// mapping stale (media_files row gone) → fall through to full pipeline.
	}

	contentType := fh.Header.Get("Content-Type")
	var outExt string
	outPath := ""
	thumbPath := ""

	var durationMs *int
	var width, height *int
	var mime string

	switch msgType {
	case chat.MsgTypeAudio:
		if chatMediaLikelyMP3(fh.Filename, contentType) {
			outExt = ".mp3"
			outPath = filepath.Join(baseDir, msgType+"_"+uuid.New().String()+outExt)
			if err := chatCopyFile(inPath, outPath); err != nil {
				h.logger.Error("chat voice mp3 copy", zap.Error(err))
				resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
				return
			}
			mime = "audio/mpeg"
		} else {
			outExt = ".ogg"
			outPath = filepath.Join(baseDir, msgType+"_"+uuid.New().String()+outExt)
			if err := ffmpegVoiceToOggOpus(inPath, outPath); err != nil {
				h.logger.Error("ffmpeg voice", zap.Error(err))
				resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
				return
			}
			mime = "audio/ogg"
		}
		if ms, _ := ffprobeDurationMs(outPath); ms != nil {
			durationMs = ms
		}
	case chat.MsgTypeVideo:
		outExt = ".mp4"
		outPath = filepath.Join(baseDir, msgType+"_"+uuid.New().String()+outExt)
		if chatMediaLikelyMP4(fh.Filename, contentType) {
			if err := chatCopyFile(inPath, outPath); err != nil {
				h.logger.Error("chat video mp4 copy", zap.Error(err))
				resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
				return
			}
		} else if err := ffmpegVideoRemuxCopy(inPath, outPath); err != nil {
			if err := ffmpegVideoToMp4(inPath, outPath, false); err != nil {
				h.logger.Error("ffmpeg video", zap.Error(err))
				resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
				return
			}
		}
		mime = "video/mp4"
		if ms, _ := ffprobeDurationMs(outPath); ms != nil {
			durationMs = ms
		}
		thumbPath = filepath.Join(baseDir, "thumb_"+uuid.New().String()+".jpg")
		_ = ffmpegThumbnail(outPath, thumbPath)
	case chat.MsgTypeVideoNote:
		outExt = ".mp4"
		outPath = filepath.Join(baseDir, msgType+"_"+uuid.New().String()+outExt)
		if err := ffmpegVideoToMp4(inPath, outPath, true); err != nil {
			h.logger.Error("ffmpeg video_note", zap.Error(err))
			resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
			return
		}
		mime = "video/mp4"
		if ms, _ := ffprobeDurationMs(outPath); ms != nil {
			durationMs = ms
		}
		thumbPath = filepath.Join(baseDir, "thumb_"+uuid.New().String()+".jpg")
		_ = ffmpegThumbnail(outPath, thumbPath)
	case chat.MsgTypeImg:
		outExt = ".jpg"
		outPath = filepath.Join(baseDir, msgType+"_"+uuid.New().String()+outExt)
		if err := ffmpegImageToJpeg(inPath, outPath); err != nil {
			if chatMediaLikelyRasterImage(fh.Filename, contentType) {
				if err := chatCopyFile(inPath, outPath); err != nil {
					h.logger.Error("chat photo copy fallback", zap.Error(err))
					resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
					return
				}
				if strings.ToLower(filepath.Ext(fh.Filename)) == ".png" || strings.Contains(strings.ToLower(contentType), "png") {
					mime = "image/png"
				} else {
					mime = "image/jpeg"
				}
			} else {
				h.logger.Error("ffmpeg photo", zap.Error(err))
				resp.ErrorLang(c, http.StatusBadRequest, "chat_media_processing_failed")
				return
			}
		} else {
			mime = "image/jpeg"
		}
	}

	// Enforce Telegram-style duration cap. ffprobe already filled durationMs
	// (or left it nil for unreadable files, which we treat as 0 — no cap).
	if cap := chatMediaDurationCapMs(msgType); cap > 0 && durationMs != nil && *durationMs > cap {
		_ = os.Remove(outPath)
		if thumbPath != "" {
			_ = os.Remove(thumbPath)
		}
		resp.ErrorLang(c, http.StatusBadRequest, "chat_media_too_long")
		return
	}

	stat, err := os.Stat(outPath)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	hashHex, err := sha256FileHex(outPath)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	// Store deduplicated file path: storage/chat/media/<prefix>/<hash>.<ext>
	mediaDir := filepath.Join(storageRoot, "chat", "media", hashHex[:2])
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	finalRelPath := filepath.Join(storageRoot, "chat", "media", hashHex[:2], hashHex+outExt)
	finalPath := finalRelPath

	mediaID, inserted, err := h.repo.UpsertMediaFile(c.Request.Context(), chat.MediaFile{
		ContentHash: hashHex,
		Kind:        msgType,
		Mime:        mime,
		SizeBytes:   stat.Size(),
		Path:        finalRelPath,
	})
	if err != nil {
		h.logger.Error("chat upsert media file", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return
	}
	storeRel := finalRelPath
	sizeBytes := stat.Size()
	if inserted {
		if err := os.Rename(outPath, finalPath); err != nil {
			h.logger.Error("chat media rename", zap.Error(err))
			_ = os.Remove(outPath)
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
	} else {
		_ = os.Remove(outPath)
		if mf, e := h.repo.GetMediaFileByID(c.Request.Context(), mediaID); e == nil && mf != nil {
			if mf.Path != "" {
				storeRel = mf.Path
			}
			sizeBytes = mf.SizeBytes
			if mf.Mime != "" {
				mime = mf.Mime
			}
		}
	}

	// Thumbnail is optional (only generated for VIDEO/VIDEO_NOTE). When present
	// we dedup it via media_files so the same video sent 10 times shares ONE
	// preview file on disk, not ten. Legacy ThumbPath stays as a fallback if
	// hashing/upsert fails, so the UI never loses a working preview.
	var thumbMediaID *uuid.UUID
	var thumbRel *string
	if thumbPath != "" {
		if info, e := os.Stat(thumbPath); e == nil && info.Size() > 0 {
			if thash, e := sha256FileHex(thumbPath); e == nil {
				thumbDir := filepath.Join(storageRoot, "chat", "media", thash[:2])
				if err := os.MkdirAll(thumbDir, 0o755); err == nil {
					thumbFinalPath := filepath.Join(thumbDir, thash+".jpg")
					tid, inserted, err := h.repo.UpsertMediaFile(c.Request.Context(), chat.MediaFile{
						ContentHash: thash,
						Kind:        "THUMB",
						Mime:        "image/jpeg",
						SizeBytes:   info.Size(),
						Path:        thumbFinalPath,
					})
					if err == nil {
						if inserted {
							if renErr := os.Rename(thumbPath, thumbFinalPath); renErr != nil {
								h.logger.Debug("chat thumb rename", zap.Error(renErr))
								_ = os.Remove(thumbPath)
							}
						} else {
							_ = os.Remove(thumbPath)
							if mf, e := h.repo.GetMediaFileByID(c.Request.Context(), tid); e == nil && mf != nil && mf.Path != "" {
								thumbFinalPath = mf.Path
							}
						}
						thumbMediaID = &tid
						thumbRel = &thumbFinalPath
					}
				}
			}
		}
		if thumbRel == nil {
			rel := thumbPath
			thumbRel = &rel
		}
	}
	rel := storeRel
	attID, err := h.repo.CreateAttachment(c.Request.Context(), chat.Attachment{
		MessageID:        nil,
		ConversationID:   convID,
		UploaderID:       userID,
		Kind:             msgType,
		Mime:             mime,
		SizeBytes:        sizeBytes,
		Path:             rel,
		ThumbPath:        thumbRel,
		Width:            width,
		Height:           height,
		DurationMs:       durationMs,
		MediaFileID:      &mediaID,
		ThumbMediaFileID: thumbMediaID,
	})
	if err != nil {
		h.logger.Error("chat create attachment", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return
	}

	baseURL := chatAttachmentPublicPath + "/" + attID.String()
	payload := mediaMsgPayload{
		AttachmentID: attID.String(),
		Link:         baseURL,
		URL:          baseURL,
		Mime:         mime,
		SizeBytes:    sizeBytes,
		DurationMs:   durationMs,
		Width:        width,
		Height:       height,
	}
	if thumbRel != nil {
		payload.ThumbURL = baseURL + "?thumb=1"
		payload.Links = &mediaLinks{Media: baseURL, Thumb: payload.ThumbURL}
	} else {
		payload.Links = &mediaLinks{Media: baseURL}
	}

	msg, err := h.repo.CreateMessage(c.Request.Context(), convID, userID, msgType, caption, payload)
	if err != nil {
		h.logger.Error("chat create media message", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return
	}
	msg.Type = chat.CanonicalMessageTypeForAPI(msg.Type)
	_ = h.repo.LinkAttachment(c.Request.Context(), attID, msg.ID)

	// Cache (source_hash → processed media_file) so the next upload of the
	// same source bytes hits the fast-path above. Best-effort — failure here
	// never breaks the send, it just means we'll re-process next time.
	if err := h.repo.PutSourceMapping(c.Request.Context(), chat.SourceMapping{
		SourceHash:       sourceHash,
		MediaFileID:      mediaID,
		ThumbMediaFileID: thumbMediaID,
		Kind:             msgType,
		Mime:             mime,
		SizeBytes:        sizeBytes,
		DurationMs:       durationMs,
		Width:            width,
		Height:           height,
	}); err != nil {
		h.logger.Debug("chat put source mapping", zap.Error(err))
	}

	h.markDeliveredIfPeerOnline(c.Request.Context(), conv, msg)
	h.hub.BroadcastMessage(conv.UserAID, conv.UserBID, msg)
	roleName := h.analyticsRoleForChat(c, userID)
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventChatMessageSent,
		UserID:     &userID,
		ActorID:    &userID,
		Role:       roleName,
		EntityType: adminanalytics.EntityChatMessage,
		EntityID:   &msg.ID,
		Metadata: map[string]any{
			"conversation_id": convID.String(),
			"message_type":    msg.Type,
		},
	})
	resp.SuccessLang(c, http.StatusCreated, "ok", msg)
}

// sendFromSourceMapping builds an attachment + message from an existing
// (source_hash → media_file) cache entry, skipping upload-body handling and
// ffmpeg entirely. Used both by the SendMediaMessage fast-path (when the
// uploaded bytes turn out to be known) and by the /messages/media-ref
// endpoint (client explicitly reuses an already-known source hash).
//
// Returns true if the message was sent (success or client-visible error
// response written). Returns false only when the mapping looks stale (the
// referenced media_file was deleted) so the caller can fall through to the
// full upload path without reporting a failure yet.
func (h *ChatHandler) sendFromSourceMapping(c *gin.Context, conv *chat.Conversation, userID, convID uuid.UUID, msgType string, caption *string, mapping *chat.SourceMapping) bool {
	ctx := c.Request.Context()

	mf, err := h.repo.GetMediaFileByID(ctx, mapping.MediaFileID)
	if err != nil || mf == nil {
		return false
	}
	mime := mf.Mime
	if mime == "" {
		mime = mapping.Mime
	}
	sizeBytes := mf.SizeBytes
	if sizeBytes == 0 {
		sizeBytes = mapping.SizeBytes
	}

	var thumbRel *string
	if mapping.ThumbMediaFileID != nil {
		if tmf, tErr := h.repo.GetMediaFileByID(ctx, *mapping.ThumbMediaFileID); tErr == nil && tmf != nil && tmf.Path != "" {
			rel := tmf.Path
			thumbRel = &rel
		}
	}

	mediaID := mapping.MediaFileID
	rel := mf.Path
	attID, err := h.repo.CreateAttachment(ctx, chat.Attachment{
		MessageID:        nil,
		ConversationID:   convID,
		UploaderID:       userID,
		Kind:             msgType,
		Mime:             mime,
		SizeBytes:        sizeBytes,
		Path:             rel,
		ThumbPath:        thumbRel,
		Width:            mapping.Width,
		Height:           mapping.Height,
		DurationMs:       mapping.DurationMs,
		MediaFileID:      &mediaID,
		ThumbMediaFileID: mapping.ThumbMediaFileID,
	})
	if err != nil {
		h.logger.Error("chat create attachment (cached)", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return true
	}

	baseURL := chatAttachmentPublicPath + "/" + attID.String()
	payload := mediaMsgPayload{
		AttachmentID: attID.String(),
		Link:         baseURL,
		URL:          baseURL,
		Mime:         mime,
		SizeBytes:    sizeBytes,
		DurationMs:   mapping.DurationMs,
		Width:        mapping.Width,
		Height:       mapping.Height,
	}
	if thumbRel != nil {
		payload.ThumbURL = baseURL + "?thumb=1"
		payload.Links = &mediaLinks{Media: baseURL, Thumb: payload.ThumbURL}
	} else {
		payload.Links = &mediaLinks{Media: baseURL}
	}

	msg, err := h.repo.CreateMessage(ctx, convID, userID, msgType, caption, payload)
	if err != nil {
		h.logger.Error("chat create media message (cached)", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return true
	}
	msg.Type = chat.CanonicalMessageTypeForAPI(msg.Type)
	_ = h.repo.LinkAttachment(ctx, attID, msg.ID)

	h.markDeliveredIfPeerOnline(ctx, conv, msg)
	h.hub.BroadcastMessage(conv.UserAID, conv.UserBID, msg)
	roleName := h.analyticsRoleForChat(c, userID)
	h.analytics.SafeTrack(c, adminanalytics.EventInput{
		EventName:  adminanalytics.EventChatMessageSent,
		UserID:     &userID,
		ActorID:    &userID,
		Role:       roleName,
		EntityType: adminanalytics.EntityChatMessage,
		EntityID:   &msg.ID,
		Metadata: map[string]any{
			"conversation_id": convID.String(),
			"message_type":    msg.Type,
		},
	})
	resp.SuccessLang(c, http.StatusCreated, "ok", msg)
	return true
}

// ProbeFile looks up a previously processed source by its SHA-256. Lets the
// client avoid uploading the body at all when the server already has it.
//
// POST /v1/chat/files/probe { "sha256": "<hex>" }
//   200 { exists: true,  mime, size_bytes, kind, duration_ms?, width?, height?, has_thumb }
//   200 { exists: false }
func (h *ChatHandler) ProbeFile(c *gin.Context) {
	if _, ok := h.getUserID(c); !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	var body struct {
		SHA256 string `json:"sha256"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	body.SHA256 = strings.ToLower(strings.TrimSpace(body.SHA256))
	if len(body.SHA256) != 64 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	mapping, err := h.repo.GetSourceMapping(c.Request.Context(), body.SHA256)
	if err != nil {
		h.logger.Error("chat probe", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if mapping == nil {
		resp.SuccessLang(c, http.StatusOK, "ok", gin.H{"exists": false})
		return
	}
	// Confirm the referenced media_file is still on disk / in DB; otherwise
	// tell the client it doesn't exist so they upload normally.
	mf, err := h.repo.GetMediaFileByID(c.Request.Context(), mapping.MediaFileID)
	if err != nil || mf == nil {
		resp.SuccessLang(c, http.StatusOK, "ok", gin.H{"exists": false})
		return
	}
	out := gin.H{
		"exists":     true,
		"kind":       chat.CanonicalMessageTypeForAPI(mapping.Kind),
		"mime":       mapping.Mime,
		"size_bytes": mapping.SizeBytes,
		"has_thumb":  mapping.ThumbMediaFileID != nil,
	}
	if mapping.DurationMs != nil {
		out["duration_ms"] = *mapping.DurationMs
	}
	if mapping.Width != nil {
		out["width"] = *mapping.Width
	}
	if mapping.Height != nil {
		out["height"] = *mapping.Height
	}
	resp.SuccessLang(c, http.StatusOK, "ok", out)
}

// SendMediaRef sends a media message by reusing a previously processed source,
// identified by its SHA-256. No body upload, no ffmpeg. Pairs with ProbeFile:
// the client probes first, then either uploads (if miss) or calls media-ref
// (if hit). This is the Telegram-style "send by file_id" flow.
//
// POST /v1/chat/conversations/:id/messages/media-ref
//   body: { type: "PHOTO|VOICE|VIDEO|VIDEO_NOTE", sha256, body? }
func (h *ChatHandler) SendMediaRef(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	convID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	conv, err := h.repo.GetConversation(c.Request.Context(), convID, userID)
	if err != nil || conv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "conversation_not_found")
		return
	}

	var body struct {
		Type   string  `json:"type"`
		SHA256 string  `json:"sha256"`
		Body   *string `json:"body,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	msgType := chat.NormalizeMessageType(body.Type)
	switch msgType {
	case chat.MsgTypeImg, chat.MsgTypeAudio, chat.MsgTypeVideo, chat.MsgTypeVideoNote:
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	sha := strings.ToLower(strings.TrimSpace(body.SHA256))
	if len(sha) != 64 {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	var caption *string
	if body.Body != nil {
		v := strings.TrimSpace(*body.Body)
		if v != "" {
			if len(v) > 64*1024 {
				resp.ErrorLang(c, http.StatusBadRequest, "message_too_long")
				return
			}
			caption = &v
		}
	}
	mapping, err := h.repo.GetSourceMapping(c.Request.Context(), sha)
	if err != nil {
		h.logger.Error("chat media-ref lookup", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if mapping == nil || !chat.MessageKindsEqual(mapping.Kind, msgType) {
		resp.ErrorLang(c, http.StatusNotFound, "chat_media_not_cached")
		return
	}
	if sent := h.sendFromSourceMapping(c, conv, userID, convID, msgType, caption, mapping); !sent {
		// mapping stale → media_file deleted under us; tell client to upload.
		resp.ErrorLang(c, http.StatusNotFound, "chat_media_not_cached")
	}
}

// GetFile serves a chat attachment file (or thumbnail) if requester is participant.
// GET /v1/chat/media/:id?thumb=1 (canonical) — GET /v1/chat/files/:id (legacy alias)
func (h *ChatHandler) GetFile(c *gin.Context) {
	userID, ok := h.currentUserIDForChat(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	attID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	wantThumb := c.Query("thumb") == "1"
	h.logger.Info("chat file request",
		zap.String("attachment_id", attID.String()),
		zap.String("user_id", userID.String()),
		zap.Bool("thumb", wantThumb),
	)
	a, err := h.getAttachmentForUser(c.Request.Context(), attID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.logger.Info("chat file not found in db",
				zap.String("attachment_id", attID.String()),
				zap.String("user_id", userID.String()),
				zap.Bool("thumb", wantThumb),
			)
		} else {
			h.logger.Error("chat file db lookup failed",
				zap.String("attachment_id", attID.String()),
				zap.String("user_id", userID.String()),
				zap.Bool("thumb", wantThumb),
				zap.Error(err),
			)
		}
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "file not found",
			"file_id": attID.String(),
		})
		return
	}
	h.logger.Info("chat file db hit",
		zap.String("attachment_id", attID.String()),
		zap.String("conversation_id", a.ConversationID.String()),
		zap.String("uploader_id", a.UploaderID.String()),
		zap.String("path", a.Path),
		zap.String("mime", a.Mime),
		zap.Bool("thumb_requested", wantThumb),
		zap.Bool("thumb_path_present", a.ThumbPath != nil && strings.TrimSpace(*a.ThumbPath) != ""),
		zap.Bool("thumb_media_file_id_present", a.ThumbMediaFileID != nil),
	)
	// Resolve the actual file on disk + its content ETag. Preference order:
	//   thumb=1 → deduplicated thumb (media_files) → legacy thumb path
	//   otherwise → deduplicated main file (media_files) → legacy attachment path
	path := a.Path
	etag := ""
	if wantThumb {
		if a.ThumbMediaFileID != nil {
			if mf, err := h.getMediaFileByID(c.Request.Context(), *a.ThumbMediaFileID); err == nil && mf != nil {
				path = mf.Path
				etag = mf.ContentHash
				h.logger.Info("chat file resolved thumb via media_files",
					zap.String("attachment_id", attID.String()),
					zap.String("media_file_id", mf.ID.String()),
					zap.String("resolved_path", path),
				)
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				h.logger.Error("chat file thumb media lookup failed",
					zap.String("attachment_id", attID.String()),
					zap.String("thumb_media_file_id", a.ThumbMediaFileID.String()),
					zap.Error(err),
				)
			}
		} else if a.ThumbPath != nil && *a.ThumbPath != "" {
			path = *a.ThumbPath
			h.logger.Info("chat file resolved thumb via legacy path",
				zap.String("attachment_id", attID.String()),
				zap.String("resolved_path", path),
			)
		} else {
			h.logger.Info("chat file thumbnail missing",
				zap.String("attachment_id", attID.String()),
				zap.String("user_id", userID.String()),
			)
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "thumbnail not found",
				"file_id": attID.String(),
			})
			return
		}
	} else if a.MediaFileID != nil {
		if mf, err := h.getMediaFileByID(c.Request.Context(), *a.MediaFileID); err == nil && mf != nil {
			path = mf.Path
			etag = mf.ContentHash
			h.logger.Info("chat file resolved main via media_files",
				zap.String("attachment_id", attID.String()),
				zap.String("media_file_id", mf.ID.String()),
				zap.String("resolved_path", path),
			)
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("chat file main media lookup failed",
				zap.String("attachment_id", attID.String()),
				zap.String("media_file_id", a.MediaFileID.String()),
				zap.Error(err),
			)
		}
	}
	h.logger.Info("chat file path before resolve",
		zap.String("attachment_id", attID.String()),
		zap.String("path_raw", path),
	)
	path = chat.ResolveStoredMediaPath(path)
	info, err := os.Stat(path)
	if err != nil {
		h.logger.Warn("chat file missing on disk",
			zap.String("attachment_id", attID.String()),
			zap.String("db_path", a.Path),
			zap.String("resolved", path),
			zap.Bool("thumb", wantThumb),
			zap.Error(err),
		)
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "file not found on disk",
			"file_id": attID.String(),
		})
		return
	}
	h.logger.Info("chat file stat ok",
		zap.String("attachment_id", attID.String()),
		zap.String("resolved_path", path),
		zap.Int64("size_bytes", info.Size()),
		zap.Bool("thumb", wantThumb),
	)

	// Caching headers (immutable for stored media).
	if etag != "" {
		c.Header("ETag", `"`+etag+`"`)
		if inm := strings.TrimSpace(c.GetHeader("If-None-Match")); inm != "" && inm == `"`+etag+`"` {
			c.Status(http.StatusNotModified)
			return
		}
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")

	// Prefer Nginx X-Accel-Redirect for efficient range/caching.
	if strings.TrimSpace(os.Getenv("CHAT_USE_X_ACCEL")) == "1" {
		prefix := strings.TrimSpace(os.Getenv("CHAT_X_ACCEL_PREFIX"))
		if prefix == "" {
			prefix = "/_protected"
		}
		// Important: Nginx should map prefix + "/" + path to filesystem via alias.
		c.Header("X-Accel-Redirect", prefix+"/"+strings.TrimLeft(path, "/"))
		c.Status(http.StatusOK)
		return
	}

	// Fallback: serve from Go (slower than Nginx for large video).
	if strings.TrimSpace(a.Mime) != "" {
		c.Header("Content-Type", strings.TrimSpace(a.Mime))
	}
	c.File(path)
}

func chatPickMultipartFile(c *gin.Context) (*multipart.FileHeader, bool) {
	for _, k := range []string{"file", "upload", "media", "photo", "video", "voice"} {
		fh, err := c.FormFile(k)
		if err == nil && fh != nil && fh.Size > 0 {
			return fh, true
		}
	}
	return nil, false
}

func chatCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// chatMediaTypeLooksRight is a soft pre-flight check: rejects obviously wrong
// uploads (e.g. image declared as VIDEO). Not a security boundary — ffmpeg is.
func chatMediaTypeLooksRight(msgType, filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	switch chat.NormalizeMessageType(msgType) {
	case chat.MsgTypeImg:
		return chatMediaLikelyRasterImage(filename, contentType)
	case chat.MsgTypeVideo, chat.MsgTypeVideoNote:
		switch ext {
		case ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".avi", ".3gp":
			return true
		}
		return strings.HasPrefix(ct, "video/")
	case chat.MsgTypeAudio:
		switch ext {
		case ".mp3", ".ogg", ".oga", ".opus", ".m4a", ".aac", ".wav", ".webm", ".amr":
			return true
		}
		return strings.HasPrefix(ct, "audio/")
	}
	return false
}

func chatMediaLikelyMP3(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	return ext == ".mp3" || strings.Contains(ct, "audio/mpeg") || strings.Contains(ct, "mp3")
}

func chatMediaLikelyMP4(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	return ext == ".mp4" || ext == ".m4v" || strings.Contains(ct, "video/mp4")
}

func chatMediaLikelyRasterImage(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".heic", ".heif":
		return true
	default:
		return strings.Contains(ct, "image/jpeg") || strings.Contains(ct, "image/png") ||
			strings.Contains(ct, "image/webp") || strings.Contains(ct, "image/gif")
	}
}

// Shared flags that make ffmpeg output byte-deterministic for a given input:
// no unix timestamps, no encoder/muxer metadata, bit-exact codec paths. This
// matters because we dedup media by SHA-256 of the OUTPUT — without these
// flags the same mp3/photo sent twice would produce two different hashes
// and defeat the cache.
var ffmpegBitExactFlags = []string{
	"-fflags", "+bitexact",
	"-flags:v", "+bitexact",
	"-flags:a", "+bitexact",
	"-map_metadata", "-1",
	"-metadata", "encoder=",
}

func ffmpegVideoRemuxCopy(inPath, outPath string) error {
	args := []string{"-y", "-i", inPath}
	args = append(args, ffmpegBitExactFlags...)
	args = append(args, "-c", "copy", "-movflags", "+faststart+empty_moov", outPath)
	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg remux: %w: %s", err, string(out))
	}
	return nil
}

func ffmpegVoiceToOggOpus(inPath, outPath string) error {
	args := []string{"-y", "-i", inPath}
	args = append(args, ffmpegBitExactFlags...)
	args = append(args,
		"-vn",
		"-c:a", "libopus",
		"-b:a", "32k",
		"-vbr", "on",
		"-compression_level", "10",
		"-ac", "1",
		"-ar", "48000",
		outPath,
	)
	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, string(out))
	}
	return nil
}

// ffmpegVideoToMp4 re-encodes the input to H.264/AAC MP4 with broad player
// compatibility (iOS Safari, older Android): yuv420p, main profile, level 3.1,
// faststart for progressive playback. `square=true` produces a 480x480
// Telegram-style "video note" (кружок).
func ffmpegVideoToMp4(inPath, outPath string, square bool) error {
	args := []string{"-y", "-i", inPath}
	args = append(args, ffmpegBitExactFlags...)
	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "28",
		"-profile:v", "main",
		"-level", "3.1",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "96k",
		"-ac", "2",
		"-ar", "44100",
		"-movflags", "+faststart",
	)
	if square {
		args = append(args, "-vf", "scale='if(gt(iw,ih),-2,480)':'if(gt(iw,ih),480,-2)',crop=480:480")
	} else {
		args = append(args, "-vf", "scale='min(1280,iw)':-2")
	}
	args = append(args, outPath)
	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, string(out))
	}
	return nil
}

// ffmpegImageToJpeg: Telegram-style photo compression. Caps long side to 1920,
// quality 4 (~85%), JPEG-ready chroma subsampling. Typical 4000x3000 photo
// shrinks from ~3-5 MB to ~350-700 KB without visible loss on a phone screen.
func ffmpegImageToJpeg(inPath, outPath string) error {
	args := []string{"-y", "-i", inPath}
	args = append(args, ffmpegBitExactFlags...)
	args = append(args,
		"-vf", "scale='if(gt(iw,1920),1920,iw)':-2",
		"-q:v", "4",
		"-pix_fmt", "yuvj420p",
		outPath,
	)
	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, string(out))
	}
	return nil
}

// ffmpegThumbnail extracts the first frame of a video as a small JPEG preview
// (long side 320px). Uses first frame (not t=1s) so it also works on clips
// shorter than a second.
func ffmpegThumbnail(inPath, outPath string) error {
	args := []string{"-y", "-i", inPath}
	args = append(args, ffmpegBitExactFlags...)
	args = append(args,
		"-vframes", "1",
		"-vf", "scale='if(gt(iw,ih),320,-2)':'if(gt(iw,ih),-2,320)'",
		"-q:v", "5",
		"-pix_fmt", "yuvj420p",
		outPath,
	)
	cmd := exec.Command("ffmpeg", args...)
	_, _ = cmd.CombinedOutput()
	return nil
}

func ffprobeDurationMs(path string) (*int, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	ms := int(f * 1000)
	return &ms, nil
}

func sha256FileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// EditMessage updates a message.
// PATCH /v1/chat/messages/:id body: { "body": "new text" }
func (h *ChatHandler) EditMessage(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	msgIDStr := c.Param("id")
	msgID, err := uuid.Parse(msgIDStr)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_message_id")
		return
	}
	var req struct {
		Body string `json:"body" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if len(req.Body) > 64*1024 {
		resp.ErrorLang(c, http.StatusBadRequest, "message_too_long")
		return
	}
	msg, err := h.repo.UpdateMessage(c.Request.Context(), msgID, userID, req.Body)
	if err != nil {
		resp.ErrorLang(c, http.StatusNotFound, "message_not_found")
		return
	}
	msg.Type = chat.CanonicalMessageTypeForAPI(msg.Type)
	if conv, err := h.repo.GetConversation(c.Request.Context(), msg.ConversationID, userID); err == nil && conv != nil {
		h.hub.BroadcastMessageUpdated(conv.UserAID, conv.UserBID, msg)
	}
	resp.OKLang(c, "ok", msg)
}

// DeleteMessage soft-deletes a message.
// DELETE /v1/chat/messages/:id
func (h *ChatHandler) DeleteMessage(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	msgIDStr := c.Param("id")
	msgID, err := uuid.Parse(msgIDStr)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_message_id")
		return
	}
	pre, err := h.repo.GetMessageByID(c.Request.Context(), msgID, userID)
	if err != nil {
		resp.ErrorLang(c, http.StatusNotFound, "message_not_found")
		return
	}
	conv, err := h.repo.GetConversation(c.Request.Context(), pre.ConversationID, userID)
	if err != nil || conv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "message_not_found")
		return
	}
	if err := h.repo.DeleteMessage(c.Request.Context(), msgID, userID); err != nil {
		if err == chat.ErrNotFound {
			resp.ErrorLang(c, http.StatusNotFound, "message_not_found")
			return
		}
		h.logger.Error("chat delete message", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_delete_message")
		return
	}
	h.hub.BroadcastMessageDeleted(conv.UserAID, conv.UserBID, pre.ConversationID, pre.ID)
	resp.OKLang(c, "ok", gin.H{"deleted": true})
}

// GetPresence returns online/last_seen (and optionally typing) for a user.
// GET /v1/chat/presence/:user_id?conversation_id=uuid
func (h *ChatHandler) GetPresence(c *gin.Context) {
	userIDStr := c.Param("user_id")
	targetID, err := uuid.Parse(userIDStr)
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_user_id")
		return
	}
	var convID *uuid.UUID
	if c := c.Query("conversation_id"); c != "" {
		u, err := uuid.Parse(c)
		if err == nil {
			convID = &u
		}
	}
	pres, err := h.presence.GetPresence(c.Request.Context(), targetID, convID)
	if err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_get_presence")
		return
	}
	resp.OKLang(c, "ok", pres)
}

// ServeWS upgrades connection to WebSocket and runs the client (read/write pumps).
// GET /v1/chat/ws?token=JWT
func (h *ChatHandler) ServeWS(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Debug("chat ws upgrade failed", zap.Error(err))
		return
	}
	client := h.hub.Register(userID, conn)
	go client.WritePump()
	client.ReadPump()
}
