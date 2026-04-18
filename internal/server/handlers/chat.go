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
	"go.uber.org/zap"

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

type ChatHandler struct {
	logger      *zap.Logger
	repo        *chat.Repo
	presence    *chat.PresenceStore
	hub         *chat.Hub
	drivers     *drivers.Repo
	dispatchers *dispatchers.Repo
}

func NewChatHandler(logger *zap.Logger, repo *chat.Repo, presence *chat.PresenceStore, hub *chat.Hub, drv *drivers.Repo, disp *dispatchers.Repo) *ChatHandler {
	h := &ChatHandler{logger: logger, repo: repo, presence: presence, hub: hub, drivers: drv, dispatchers: disp}
	hub.SetOnTyping(func(conversationID, fromUserID uuid.UUID) (uuid.UUID, bool) {
		ctx := context.Background()
		conv, err := repo.GetConversation(ctx, conversationID, fromUserID)
		if err != nil || conv == nil {
			return uuid.Nil, false
		}
		return conv.PeerID(fromUserID), true
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

func chatLastMessagePreview(msgType string, body *string) string {
	if body != nil {
		s := strings.TrimSpace(*body)
		if s != "" {
			return s
		}
	}
	switch strings.ToUpper(strings.TrimSpace(msgType)) {
	case "PHOTO":
		return "Photo"
	case "VOICE":
		return "Voice"
	case "VIDEO":
		return "Video"
	case "VIDEO_NOTE":
		return "Video message"
	case "LOCATION":
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
		"peer_id":   peerID,
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
// POST /v1/chat/conversations/:id/messages body: { "body": "text" }
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
	conv, err := h.repo.GetConversation(c.Request.Context(), convID, userID)
	if err != nil || conv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "conversation_not_found")
		return
	}
	msg, err := h.repo.CreateTextMessage(c.Request.Context(), convID, userID, req.Body)
	if err != nil {
		h.logger.Error("chat create message", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return
	}
	h.hub.BroadcastMessage(conv.UserAID, conv.UserBID, msg)
	resp.SuccessLang(c, http.StatusCreated, "ok", msg)
}

type mediaMsgPayload struct {
	AttachmentID string `json:"attachment_id"`
	URL          string `json:"url"`
	ThumbURL     string `json:"thumb_url,omitempty"`
	Mime         string `json:"mime"`
	SizeBytes    int64  `json:"size_bytes"`
	DurationMs   *int   `json:"duration_ms,omitempty"`
	Width        *int   `json:"width,omitempty"`
	Height       *int   `json:"height,omitempty"`
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

	msgType := strings.ToUpper(strings.TrimSpace(c.PostForm("type")))
	if msgType == "AUDIO" {
		msgType = "VOICE"
	}
	switch msgType {
	case "PHOTO", "VOICE", "VIDEO", "VIDEO_NOTE":
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
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := dst.Close(); err != nil {
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}

	contentType := fh.Header.Get("Content-Type")
	var outExt string
	outPath := ""
	thumbPath := ""

	var durationMs *int
	var width, height *int
	var mime string

	switch msgType {
	case "VOICE":
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
	case "VIDEO":
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
	case "VIDEO_NOTE":
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
	case "PHOTO":
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

	var thumbMediaID *uuid.UUID
	var thumbRel *string
	if thumbPath != "" {
		// Thumbnail is optional; we keep it per-upload (no dedup required), but store in legacy column and optionally media_files too.
		rel := thumbPath
		thumbRel = &rel
	}
	rel := storeRel
	attID, err := h.repo.CreateAttachment(c.Request.Context(), chat.Attachment{
		MessageID:      nil,
		ConversationID: convID,
		UploaderID:     userID,
		Kind:           msgType,
		Mime:           mime,
		SizeBytes:      sizeBytes,
		Path:           rel,
		ThumbPath:      thumbRel,
		Width:          width,
		Height:         height,
		DurationMs:     durationMs,
		MediaFileID:    &mediaID,
		ThumbMediaFileID: thumbMediaID,
	})
	if err != nil {
		h.logger.Error("chat create attachment", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return
	}

	payload := mediaMsgPayload{
		AttachmentID: attID.String(),
		URL:          "/v1/chat/files/" + attID.String(),
		Mime:         mime,
		SizeBytes:    sizeBytes,
		DurationMs:   durationMs,
		Width:        width,
		Height:       height,
	}
	if thumbRel != nil {
		payload.ThumbURL = "/v1/chat/files/" + attID.String() + "?thumb=1"
	}

	msg, err := h.repo.CreateMessage(c.Request.Context(), convID, userID, msgType, caption, payload)
	if err != nil {
		h.logger.Error("chat create media message", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_send_message")
		return
	}
	_ = h.repo.LinkAttachment(c.Request.Context(), attID, msg.ID)

	h.hub.BroadcastMessage(conv.UserAID, conv.UserBID, msg)
	resp.SuccessLang(c, http.StatusCreated, "ok", msg)
}

// GetFile serves a chat attachment file (or thumbnail) if requester is participant.
// GET /v1/chat/files/:id?thumb=1
func (h *ChatHandler) GetFile(c *gin.Context) {
	userID, ok := h.getUserID(c)
	if !ok {
		resp.ErrorLang(c, http.StatusUnauthorized, "user_not_identified")
		return
	}
	attID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_id")
		return
	}
	a, err := h.repo.GetAttachmentForUser(c.Request.Context(), attID, userID)
	if err != nil {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}
	path := a.Path
	etag := ""
	if a.MediaFileID != nil {
		if mf, err := h.repo.GetMediaFileByID(c.Request.Context(), *a.MediaFileID); err == nil && mf != nil {
			path = mf.Path
			etag = mf.ContentHash
		}
	}
	if c.Query("thumb") == "1" && a.ThumbPath != nil && *a.ThumbPath != "" {
		path = *a.ThumbPath
	}
	path = filepath.FromSlash(path)
	if _, err := os.Stat(path); err != nil {
		resp.ErrorLang(c, http.StatusNotFound, "photo_not_found")
		return
	}

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

func ffmpegVideoRemuxCopy(inPath, outPath string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", inPath, "-c", "copy", "-movflags", "+faststart", outPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg remux: %w: %s", err, string(out))
	}
	return nil
}

func ffmpegVoiceToOggOpus(inPath, outPath string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", inPath, "-vn", "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", "-compression_level", "10", "-ac", "1", "-ar", "48000", outPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, string(out))
	}
	return nil
}

func ffmpegVideoToMp4(inPath, outPath string, square bool) error {
	args := []string{"-y", "-i", inPath, "-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart"}
	if square {
		// Make a square "video note"-like output.
		// scale to min dim, then crop center to 480x480.
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

func ffmpegImageToJpeg(inPath, outPath string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", inPath, "-q:v", "3", outPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, string(out))
	}
	return nil
}

func ffmpegThumbnail(inPath, outPath string) error {
	cmd := exec.Command("ffmpeg", "-y", "-ss", "00:00:01", "-i", inPath, "-vframes", "1", "-q:v", "4", outPath)
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
