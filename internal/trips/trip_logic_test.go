package trips

import "testing"

func TestNextStatus(t *testing.T) {
	cases := []struct {
		current  string
		expected string
	}{
		{StatusPendingDriver, StatusAssigned},
		{StatusAssigned, StatusLoading},
		{StatusLoading, StatusEnRoute},
		{StatusEnRoute, StatusUnloading},
		{StatusUnloading, StatusCompleted},
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
