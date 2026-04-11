package trips

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNextStatus(t *testing.T) {
	cases := []struct {
		current  string
		expected string
	}{
		{StatusInProgress, StatusInTransit},
		{StatusInTransit, StatusDelivered},
		{StatusDelivered, StatusCompleted},
		{StatusCompleted, ""},
		{StatusCancelled, ""},
		{"UNKNOWN", ""},
	}
	for _, tc := range cases {
		got := NextStatus(tc.current)
		if got != tc.expected {
			t.Errorf("NextStatus(%q) = %q, want %q", tc.current, got, tc.expected)
		}
	}
}

func TestCompletionPending(t *testing.T) {
	p := StatusCompleted
	now := time.Now()
	trip := &Trip{Status: StatusDelivered, PendingConfirmTo: &p, DriverConfirmedAt: &now}
	if !CompletionPending(trip) {
		t.Fatal("expected completion pending")
	}
	if CompletionPending(&Trip{Status: StatusDelivered}) {
		t.Fatal("unexpected pending without flags")
	}
	if CompletionPending(&Trip{Status: StatusInProgress, ID: uuid.New()}) {
		t.Fatal("unexpected pending for in progress")
	}
}

func TestAllowedTransitionsContainNext(t *testing.T) {
	for cur, allowed := range allowedTransitions {
		next := NextStatus(cur)
		if next == "" {
			continue
		}
		ok := false
		for _, s := range allowed {
			if s == next {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("NextStatus(%q)=%q not in allowedTransitions[%q]=%v", cur, next, cur, allowed)
		}
	}
}
