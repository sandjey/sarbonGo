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
	tk, source := s.resolveRecipientToken(ctx, recipientKind, recipientID)
	if strings.TrimSpace(tk) == "" {
		if s.logger != nil {
			s.logger.Warn("push skipped: token not found",
				zap.String("stream_kind", streamKind),
				zap.String("recipient_kind", recipientKind),
				zap.String("recipient_id", recipientID.String()),
				zap.String("token_source", source),
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
		s.logger.Warn("push skipped (chat): token not found", zap.String("user_id", userID.String()))
	}
}

func badgeOne() *int { v := 1; return &v }
