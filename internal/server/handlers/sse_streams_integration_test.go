package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"sarbonNew/internal/config"
	"sarbonNew/internal/security"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

func TestDriverRealtimeSSE_FCMAlignedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := userstream.NewHub()
	jwtm := security.NewJWTManager("unit-test-jwt-signing-key-32b!", time.Hour, time.Hour)
	driverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	tokens, _, err := jwtm.Issue("driver", driverID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	r := gin.New()
	sseH := NewSSEStreamsHandler(hub)
	v1 := r.Group("/v1")
	drv := v1.Group("/driver")
	drv.Use(mw.RequireBaseHeaders(cfg))
	drv.Use(mw.RequireDriverWithQueryToken(jwtm, nil))
	drv.GET("/sse/realtime", sseH.DriverRealtimeSSE)

	ts := httptest.NewServer(r)
	defer ts.Close()

	q := url.Values{}
	q.Set("token", tokens.AccessToken)
	q.Set("device_type", "web")
	q.Set("language", "ru")
	q.Set("client_token", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/driver/sse/realtime?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d %s", resp.StatusCode, string(b))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 512*1024)
	for sc.Scan() {
		if strings.Contains(sc.Text(), "sse connected") {
			break
		}
	}
	hub.PublishNotification(tripnotif.RecipientDriver, driverID, map[string]any{
		"kind": "trip_notification", "trip_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
	})
	var payload map[string]any
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if payload["stream_kind"] != "notifications" {
		t.Fatalf("stream_kind=%v", payload["stream_kind"])
	}
	if payload["recipient_kind"] != tripnotif.RecipientDriver {
		t.Fatalf("recipient_kind=%v", payload["recipient_kind"])
	}
	if payload["recipient_id"] != driverID.String() {
		t.Fatalf("recipient_id=%v", payload["recipient_id"])
	}
	if payload["kind"] != "trip_notification" {
		t.Fatalf("kind=%v", payload["kind"])
	}
	cancel()
}

func TestHubPublishNotification_splitsByKind(t *testing.T) {
	h := userstream.NewHub()
	did := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	tripCh, unsubT := h.SubscribeTripUserNotifications(tripnotif.RecipientDispatcher, did)
	defer unsubT()
	offerCh, unsubO := h.SubscribeCargoOfferNotifications(tripnotif.RecipientDispatcher, did)
	defer unsubO()
	connCh, unsubC := h.SubscribeConnectionNotifications(tripnotif.RecipientDispatcher, did)
	defer unsubC()

	h.PublishNotification(tripnotif.RecipientDispatcher, did, map[string]any{"kind": "trip_notification", "x": 1})
	select {
	case b := <-tripCh:
		if !strings.Contains(string(b), `"kind":"trip_notification"`) {
			t.Fatalf("trip: %s", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout trip")
	}
	select {
	case <-offerCh:
		t.Fatal("cargo_offer channel should not receive trip_notification")
	default:
	}

	h.PublishNotification(tripnotif.RecipientDispatcher, did, map[string]any{"kind": "cargo_offer", "offer_id": "x"})
	select {
	case b := <-offerCh:
		if !strings.Contains(string(b), "cargo_offer") {
			t.Fatalf("offer: %s", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout offer")
	}

	h.PublishNotification(tripnotif.RecipientDispatcher, did, map[string]any{"kind": "connection_offer", "token": "t"})
	select {
	case b := <-connCh:
		if !strings.Contains(string(b), "connection_offer") {
			t.Fatalf("conn: %s", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout conn")
	}
}
