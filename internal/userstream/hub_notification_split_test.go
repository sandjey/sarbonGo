package userstream

import (
	"testing"

	"github.com/google/uuid"
)

func TestNotificationKindFromJSON(t *testing.T) {
	b := []byte(`{"kind":"trip_notification","trip_id":"x"}`)
	if notificationKindFromJSON(b) != "trip_notification" {
		t.Fatal(notificationKindFromJSON(b))
	}
	if notificationKindFromJSON([]byte(`{}`)) != "" {
		t.Fatal("expected empty kind")
	}
}

func TestSubscribeNotificationsStillReceivesAllKinds(t *testing.T) {
	h := NewHub()
	id := uuid.New()
	ch, unsub := h.SubscribeNotifications("driver", id)
	defer unsub()
	h.PublishNotification("driver", id, map[string]any{"kind": "cargo_offer"})
	select {
	case msg := <-ch:
		if len(msg) == 0 {
			t.Fatal("empty")
		}
	default:
		t.Fatal("full inbox should receive cargo_offer")
	}
}
