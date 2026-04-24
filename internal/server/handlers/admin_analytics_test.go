package handlers

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestParseAdminAnalyticsWindowDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/v1/admin/dashboard", nil)
	c.Request = req

	got, ok := parseAdminAnalyticsWindow(c)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.TZ != "UTC" {
		t.Fatalf("expected default tz UTC, got %q", got.TZ)
	}
	if !got.From.Before(got.To) {
		t.Fatal("expected from < to")
	}
	if diff := got.To.Sub(got.From); diff < 29*24*time.Hour || diff > 31*24*time.Hour {
		t.Fatalf("expected default window about 30 days, got %v", diff)
	}
}

func TestParseAdminAnalyticsWindowInvalidTZ(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/v1/admin/dashboard?tz=Bad/Timezone", nil)
	c.Request = req

	_, ok := parseAdminAnalyticsWindow(c)
	if ok {
		t.Fatal("expected ok=false")
	}
	if w.Code != 400 {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestParseCSV(t *testing.T) {
	got := parseCSV(" cargo_count, offer_count ,, trip_count ")
	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}
	if got[0] != "cargo_count" || got[1] != "offer_count" || got[2] != "trip_count" {
		t.Fatalf("unexpected values: %#v", got)
	}
}
