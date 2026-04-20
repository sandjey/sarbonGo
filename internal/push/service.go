package push

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/api/option"

	"sarbonNew/internal/config"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/tripnotif"
)

type Service struct {
	logger    *zap.Logger
	fcm       *messaging.Client
	drv       *drivers.Repo
	disp      *dispatchers.Repo
	projectID string
}

func New(ctx context.Context, cfg config.Config, logger *zap.Logger, drv *drivers.Repo, disp *dispatchers.Repo) (*Service, error) {
	s := &Service{logger: logger, drv: drv, disp: disp}
	if !cfg.PushNotificationsEnabled {
		return s, nil
	}
	credFile := strings.TrimSpace(cfg.FirebaseCredentialsFile)
	projectID := strings.TrimSpace(cfg.FirebaseProjectID)
	if credFile == "" || projectID == "" {
		logger.Warn("push disabled: firebase config incomplete")
		return s, nil
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, option.WithCredentialsFile(credFile))
	if err != nil {
		return nil, err
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, err
	}
	s.fcm = client
	s.projectID = projectID
	logger.Info("firebase push enabled", zap.String("project_id", projectID))
	return s, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.fcm != nil
}

func (s *Service) sendToToken(ctx context.Context, token, title, body string, data map[string]string) {
	if _, err := s.sendToTokenErr(ctx, token, title, body, data); err != nil && s.logger != nil {
		s.logger.Warn("push send failed", zap.Error(err))
	}
}

// sendToTokenErr sends one FCM message. Returns FCM message ID on success (for logs / admin diagnostics).
// Android: high priority only — do not force notification channel_id here: if the app never created
// "sarbon_default", Android 8+ may drop the notification even when FCM returns success.
func (s *Service) sendToTokenErr(ctx context.Context, token, title, body string, data map[string]string) (fcmMessageID string, err error) {
	if s == nil || s.fcm == nil {
		return "", nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("push token is empty")
	}
	msg := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound: "default",
					Badge: badgeOne(),
				},
			},
		},
	}
	return s.fcm.Send(ctx, msg)
}

func (s *Service) resolveRecipientToken(ctx context.Context, recipientKind string, recipientID uuid.UUID) (string, string) {
	if recipientID == uuid.Nil {
		return "", "recipient_id_nil"
	}
	switch recipientKind {
	case tripnotif.RecipientDriver:
		if s.drv != nil {
			if tk, err := s.drv.GetPushToken(ctx, recipientID); err == nil && strings.TrimSpace(tk) != "" {
				return tk, "drivers.push_token"
			}
		}
		// Fallback: if role mapping is inconsistent in some environments.
		if s.disp != nil {
			if tk, err := s.disp.GetPushToken(ctx, recipientID); err == nil && strings.TrimSpace(tk) != "" {
				return tk, "freelance_dispatchers.push_token(fallback)"
			}
		}
	case tripnotif.RecipientDispatcher:
		if s.disp != nil {
			if tk, err := s.disp.GetPushToken(ctx, recipientID); err == nil && strings.TrimSpace(tk) != "" {
				return tk, "freelance_dispatchers.push_token"
			}
		}
		// Fallback: if role mapping is inconsistent in some environments.
		if s.drv != nil {
			if tk, err := s.drv.GetPushToken(ctx, recipientID); err == nil && strings.TrimSpace(tk) != "" {
				return tk, "drivers.push_token(fallback)"
			}
		}
	}
	return "", "token_not_found"
}

// SendTestToToken sends a test notification directly to the given FCM token.
// Returns FCM message ID on success (use in Firebase Console / support).
func (s *Service) SendTestToToken(ctx context.Context, token, title, body string, extra map[string]string) (fcmMessageID string, err error) {
	if !s.Enabled() {
		return "", fmt.Errorf("push notifications are disabled or firebase is not configured")
	}
	data := map[string]string{"source": "test"}
	for k, v := range extra {
		data[k] = v
	}
	return s.sendToTokenErr(ctx, token, title, body, data)
}

// RecipientTokenInfo summarises whether a driver/dispatcher has a usable FCM token in DB.
type RecipientTokenInfo struct {
	Kind        string
	ID          uuid.UUID
	TokenFound  bool
	TokenLen    int
	TokenPrefix string
	Source      string
}

// InspectRecipient returns token availability for a given (kind, id) without sending anything.
// Used by admin diagnostic endpoint to explain why system flow cannot push.
func (s *Service) InspectRecipient(ctx context.Context, recipientKind string, recipientID uuid.UUID) RecipientTokenInfo {
	info := RecipientTokenInfo{Kind: recipientKind, ID: recipientID}
	tk, source := s.resolveRecipientToken(ctx, recipientKind, recipientID)
	info.Source = source
	tk = strings.TrimSpace(tk)
	if tk == "" {
		return info
	}
	info.TokenFound = true
	info.TokenLen = len(tk)
	if len(tk) >= 16 {
		info.TokenPrefix = tk[:16]
	} else {
		info.TokenPrefix = tk
	}
	return info
}

// RecipientSendResult describes what SendByRecipient attempted and what happened.
type RecipientSendResult struct {
	TokenFound   bool
	TokenPrefix  string
	Source       string
	FCMMessageID string
}

// SendByRecipient sends a notification via the SAME code path used by the internal event flow
// (userstream → onPublish → SendByStreamRecipient). Use it from admin tools to reproduce the real
// system scenario for a given driver/dispatcher ID — not for bypass tests with a raw token.
func (s *Service) SendByRecipient(ctx context.Context, recipientKind string, recipientID uuid.UUID, title, body string, data map[string]string) (RecipientSendResult, error) {
	if !s.Enabled() {
		return RecipientSendResult{}, fmt.Errorf("push notifications are disabled or firebase is not configured")
	}
	if recipientID == uuid.Nil {
		return RecipientSendResult{}, fmt.Errorf("recipient_id is empty")
	}
	tk, source := s.resolveRecipientToken(ctx, recipientKind, recipientID)
	tk = strings.TrimSpace(tk)
	if tk == "" {
		return RecipientSendResult{Source: source}, fmt.Errorf("push token not found for %s %s (source=%s)", recipientKind, recipientID, source)
	}
	out := RecipientSendResult{TokenFound: true, Source: source}
	if len(tk) >= 16 {
		out.TokenPrefix = tk[:16]
	} else {
		out.TokenPrefix = tk
	}
	if data == nil {
		data = map[string]string{}
	}
	data["source"] = "admin.send_by_recipient"
	data["recipient_kind"] = recipientKind
	data["recipient_id"] = recipientID.String()

	msgID, err := s.sendToTokenErr(ctx, tk, title, body, data)
	if err != nil {
		return out, err
	}
	out.FCMMessageID = msgID
	return out, nil
}

// SaveRecipientToken upserts FCM token for the given driver/dispatcher. Used by admin tools so
// that a single successful test also populates DB and enables system flow (SendByStreamRecipient).
func (s *Service) SaveRecipientToken(ctx context.Context, recipientKind string, recipientID uuid.UUID, token string) error {
	if recipientID == uuid.Nil {
		return fmt.Errorf("recipient_id is empty")
	}
	token = strings.TrimSpace(token)
	if len(token) < 10 {
		return fmt.Errorf("token is too short")
	}
	switch recipientKind {
	case tripnotif.RecipientDriver:
		if s.drv == nil {
			return fmt.Errorf("drivers repo not configured")
		}
		return s.drv.UpdatePushToken(ctx, recipientID, token)
	case tripnotif.RecipientDispatcher:
		if s.disp == nil {
			return fmt.Errorf("dispatchers repo not configured")
		}
		return s.disp.UpdatePushToken(ctx, recipientID, token)
	default:
		return fmt.Errorf("unknown recipient_kind: %s", recipientKind)
	}
}

// ProjectID returns the Firebase project_id if configured (empty string if disabled).
func (s *Service) ProjectID() string {
	if s == nil || s.fcm == nil {
		return ""
	}
	// projectID is stored in the firebase.App; we expose it via config during construction.
	return s.projectID
}

func (s *Service) SendByStreamRecipient(ctx context.Context, streamKind, recipientKind string, recipientID uuid.UUID, payload []byte) {
	if !s.Enabled() || recipientID == uuid.Nil {
		return
	}
	title := "Sarbon"
	body := "New notification"
	data := map[string]string{
		"stream_kind":    streamKind,
		"recipient_kind": recipientKind,
		"recipient_id":   recipientID.String(),
	}
	var msg map[string]any
	if len(payload) > 0 && json.Unmarshal(payload, &msg) == nil {
		if v, ok := msg["event_kind"].(string); ok && strings.TrimSpace(v) != "" {
			body = v
			data["event_kind"] = v
		}
		if v, ok := msg["event"].(string); ok && strings.TrimSpace(v) != "" {
			body = v
			data["event"] = v
		}
		if v, ok := msg["kind"].(string); ok && strings.TrimSpace(v) != "" {
			data["kind"] = v
		}
	}

	// Business rule: the driver changes trip statuses themselves (IN_TRANSIT, DELIVERED,
	// COMPLETION_PENDING_MANAGER, CANCELLED). Pushing every hop to the same driver is pure noise.
	// Firebase для водителя по рейсам отправляем только финальный `COMPLETED` (ставится cargo manager).
	// Для диспетчера/cargo manager поведение не меняется — он получает все trip-события.
	// SSE и список /v1/driver/notifications не затрагиваются — в приложении водитель видит всю историю.
	if skip, reason := shouldSuppressTripPushForDriver(streamKind, recipientKind, data); skip {
		if s.logger != nil {
			s.logger.Info("push suppressed (trip event for driver)",
				zap.String("stream_kind", streamKind),
				zap.String("recipient_kind", recipientKind),
				zap.String("recipient_id", recipientID.String()),
				zap.String("payload_kind", firstNonEmpty(data["kind"], data["event_kind"], data["event"])),
				zap.String("event_kind", data["event_kind"]),
				zap.String("reason", reason),
			)
		}
		return
	}

	tk, source := s.resolveRecipientToken(ctx, recipientKind, recipientID)
	if strings.TrimSpace(tk) == "" {
		if s.logger != nil {
			s.logger.Warn("push skipped: token not found",
				zap.String("stream_kind", streamKind),
				zap.String("recipient_kind", recipientKind),
				zap.String("recipient_id", recipientID.String()),
				zap.String("token_source", source),
				zap.String("payload_kind", firstNonEmpty(data["kind"], data["event_kind"], data["event"])),
				zap.String("hint", "mobile app did not register FCM token via POST /v1/chat/push-token (driver/dispatcher) — or saved under a different user id"),
			)
		}
		return
	}
	if msgID, err := s.sendToTokenErr(ctx, tk, title, body, data); err != nil {
		if s.logger != nil {
			s.logger.Warn("push send failed (stream)",
				zap.Error(err),
				zap.String("stream_kind", streamKind),
				zap.String("recipient_kind", recipientKind),
				zap.String("recipient_id", recipientID.String()),
				zap.String("token_source", source),
			)
		}
	} else if s.logger != nil {
		s.logger.Info("push sent (stream)",
			zap.String("stream_kind", streamKind),
			zap.String("recipient_kind", recipientKind),
			zap.String("recipient_id", recipientID.String()),
			zap.String("token_source", source),
			zap.String("fcm_message_id", msgID),
		)
	}
}

func (s *Service) SendByChatUser(ctx context.Context, userID uuid.UUID, payload []byte) {
	if !s.Enabled() || userID == uuid.Nil {
		return
	}
	title := "Sarbon Chat"
	body := "New event"
	data := map[string]string{
		"user_id": userID.String(),
	}
	var env struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}
	if len(payload) > 0 && json.Unmarshal(payload, &env) == nil {
		data["type"] = env.Type
		switch {
		case env.Type == "message":
			body = "New message"
		case strings.HasPrefix(env.Type, "call."):
			body = env.Type
		default:
			return
		}
	}
	// Try driver first, then dispatcher.
	if s.drv != nil {
		if tk, _ := s.drv.GetPushToken(ctx, userID); strings.TrimSpace(tk) != "" {
			if msgID, err := s.sendToTokenErr(ctx, tk, title, body, data); err != nil {
				if s.logger != nil {
					s.logger.Warn("push send failed (chat driver)", zap.Error(err), zap.String("user_id", userID.String()))
				}
			} else if s.logger != nil {
				s.logger.Info("push sent (chat driver)", zap.String("user_id", userID.String()), zap.String("fcm_message_id", msgID))
			}
			return
		}
	}
	if s.disp != nil {
		if tk, _ := s.disp.GetPushToken(ctx, userID); strings.TrimSpace(tk) != "" {
			if msgID, err := s.sendToTokenErr(ctx, tk, title, body, data); err != nil {
				if s.logger != nil {
					s.logger.Warn("push send failed (chat dispatcher)", zap.Error(err), zap.String("user_id", userID.String()))
				}
			} else if s.logger != nil {
				s.logger.Info("push sent (chat dispatcher)", zap.String("user_id", userID.String()), zap.String("fcm_message_id", msgID))
			}
			return
		}
	}
	if s.logger != nil {
		s.logger.Warn("push skipped (chat): token not found",
			zap.String("user_id", userID.String()),
			zap.String("type", data["type"]),
			zap.String("hint", "mobile app did not register FCM token via POST /v1/chat/push-token for this user"),
		)
	}
}

func badgeOne() *int { v := 1; return &v }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// shouldSuppressTripPushForDriver decides whether to silently skip a Firebase push to a driver
// for trip-related events. The driver is the one who performs IN_TRANSIT / DELIVERED / cancels /
// asks for completion, so pushing those back to their own device is noise. The only trip push
// the driver actually needs is the final COMPLETED (set by the cargo manager).
//
// Rules (driver only):
//   - streamKind == "trip_status"                          → suppress (purely UI snapshot stream).
//   - streamKind == "notifications" && kind == "trip_notification" && event_kind != COMPLETED
//                                                          → suppress.
//
// Non-trip events (cargo_offer, connection_offer, driver_profile_edit, etc.) and dispatcher
// recipients are NOT affected.
func shouldSuppressTripPushForDriver(streamKind, recipientKind string, data map[string]string) (bool, string) {
	if recipientKind != tripnotif.RecipientDriver {
		return false, ""
	}
	if streamKind == "trip_status" {
		return true, "driver_trip_status_self_managed"
	}
	if streamKind == "notifications" && strings.TrimSpace(data["kind"]) == "trip_notification" {
		evk := strings.ToUpper(strings.TrimSpace(data["event_kind"]))
		if evk != tripnotif.EventCompleted {
			return true, "driver_trip_event_non_final"
		}
	}
	return false, ""
}
