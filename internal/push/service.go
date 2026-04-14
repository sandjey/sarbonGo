package push

import (
	"context"
	"encoding/json"
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
	logger *zap.Logger
	fcm    *messaging.Client
	drv    *drivers.Repo
	disp   *dispatchers.Repo
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
	logger.Info("firebase push enabled", zap.String("project_id", projectID))
	return s, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.fcm != nil
}

func (s *Service) sendToToken(ctx context.Context, token, title, body string, data map[string]string) {
	if s == nil || s.fcm == nil {
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	msg := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
	}
	if _, err := s.fcm.Send(ctx, msg); err != nil && s.logger != nil {
		s.logger.Warn("push send failed", zap.Error(err))
	}
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
	switch recipientKind {
	case tripnotif.RecipientDriver:
		if s.drv == nil {
			return
		}
		tk, _ := s.drv.GetPushToken(ctx, recipientID)
		s.sendToToken(ctx, tk, title, body, data)
	case tripnotif.RecipientDispatcher:
		if s.disp == nil {
			return
		}
		tk, _ := s.disp.GetPushToken(ctx, recipientID)
		s.sendToToken(ctx, tk, title, body, data)
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
			s.sendToToken(ctx, tk, title, body, data)
			return
		}
	}
	if s.disp != nil {
		if tk, _ := s.disp.GetPushToken(ctx, userID); strings.TrimSpace(tk) != "" {
			s.sendToToken(ctx, tk, title, body, data)
		}
	}
}
