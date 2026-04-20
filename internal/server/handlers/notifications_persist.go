package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/tripnotif"
)

// NotificationPersister records non-trip events (cargo_offer, connection_offer, driver_update, chat message, call)
// into trip_user_notifications so mobile clients can paginate over a single "notifications" inbox that mirrors
// everything that was pushed via SSE / WebSocket / Firebase.
//
// Trip-status notifications are intentionally NOT persisted here because trip_notify_emit.go.notifInsertExtra
// already writes them with richer metadata (from_status, to_status, notification_id).
type NotificationPersister struct {
	logger *zap.Logger
	notif  *tripnotif.Repo
	drv    *drivers.Repo
	disp   *dispatchers.Repo
}

func NewNotificationPersister(logger *zap.Logger, notif *tripnotif.Repo, drv *drivers.Repo, disp *dispatchers.Repo) *NotificationPersister {
	return &NotificationPersister{logger: logger, notif: notif, drv: drv, disp: disp}
}

// PersistStream stores a payload published via userstream.Hub for a known recipient (driver or dispatcher).
func (p *NotificationPersister) PersistStream(ctx context.Context, streamKind, recipientKind string, recipientID uuid.UUID, payload []byte) {
	if p == nil || p.notif == nil || recipientID == uuid.Nil || len(payload) == 0 {
		return
	}

	eventType, eventKind, skip := classifyStreamPayload(streamKind, payload)
	if skip || eventType == "" {
		return
	}

	if _, err := p.notif.InsertGeneric(ctx, recipientKind, recipientID, eventType, eventKind, payload); err != nil {
		if p.logger != nil {
			p.logger.Warn("notification persist failed (stream)",
				zap.Error(err),
				zap.String("stream_kind", streamKind),
				zap.String("event_type", eventType),
				zap.String("event_kind", eventKind),
				zap.String("recipient_kind", recipientKind),
				zap.String("recipient_id", recipientID.String()),
			)
		}
	}
}

// PersistChat stores a chat-hub event (message, call.*) for the recipient user in its canonical inbox.
// We resolve recipient_kind by looking up the user in drivers / freelance_dispatchers tables.
func (p *NotificationPersister) PersistChat(ctx context.Context, userID uuid.UUID, payload []byte) {
	if p == nil || p.notif == nil || userID == uuid.Nil || len(payload) == 0 {
		return
	}

	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	t := strings.TrimSpace(env.Type)

	var eventType, eventKind string
	switch {
	case t == "message":
		eventType = tripnotif.TypeMessage
		eventKind = "message.new"
	case strings.HasPrefix(t, "call."):
		eventType = tripnotif.TypeCall
		eventKind = t
	default:
		return
	}

	recipientKind := p.resolveChatRecipientKind(ctx, userID)
	if recipientKind == "" {
		// Unknown user — do not persist to avoid polluting the inbox of an unrelated actor.
		return
	}

	if _, err := p.notif.InsertGeneric(ctx, recipientKind, userID, eventType, eventKind, payload); err != nil {
		if p.logger != nil {
			p.logger.Warn("notification persist failed (chat)",
				zap.Error(err),
				zap.String("event_type", eventType),
				zap.String("event_kind", eventKind),
				zap.String("recipient_kind", recipientKind),
				zap.String("user_id", userID.String()),
			)
		}
	}
}

func (p *NotificationPersister) resolveChatRecipientKind(ctx context.Context, userID uuid.UUID) string {
	if p.drv != nil {
		if _, err := p.drv.FindByID(ctx, userID); err == nil {
			return tripnotif.RecipientDriver
		} else if !errors.Is(err, drivers.ErrNotFound) && p.logger != nil {
			p.logger.Debug("notification persist: driver lookup error", zap.Error(err), zap.String("user_id", userID.String()))
		}
	}
	if p.disp != nil {
		if _, err := p.disp.FindByID(ctx, userID); err == nil {
			return tripnotif.RecipientDispatcher
		}
	}
	return ""
}

// classifyStreamPayload derives (event_type, event_kind, skip) from the hub stream + envelope.
// When skip == true the payload must not be persisted (either it is already stored elsewhere or not interesting).
func classifyStreamPayload(streamKind string, payload []byte) (eventType, eventKind string, skip bool) {
	switch streamKind {
	case "trip_status":
		// Trip-status stream duplicates trip_user_notifications inserts done by trip_notify_emit.
		return "", "", true
	case "driver_updates":
		eventKind = extractStringField(payload, "action", "event")
		return tripnotif.TypeDriverProfileEdit, eventKind, false
	case "notifications":
		kind := strings.TrimSpace(extractStringField(payload, "kind"))
		event := strings.TrimSpace(extractStringField(payload, "event", "event_kind", "action"))
		switch kind {
		case "trip_notification":
			// Already persisted by trip_notify_emit.notifInsertExtra (notification_id present in payload).
			return "", "", true
		case "cargo_offer":
			return tripnotif.TypeCargoOffer, event, false
		case "connection_offer":
			return tripnotif.TypeConnectionOffer, event, false
		case "driver_update":
			return tripnotif.TypeDriverProfileEdit, event, false
		case "":
			// Legacy payloads without kind: fall back to event categorisation.
			return "", event, false
		default:
			return kind, event, false
		}
	}
	return "", "", true
}

// extractStringField returns the first non-empty string value found under the given JSON keys at root level.
func extractStringField(payload []byte, keys ...string) string {
	if len(payload) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	return ""
}
