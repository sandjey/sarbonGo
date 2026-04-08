package cargo

import "testing"

func TestTripStatusConsumesVehiclesLeft(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"IN_TRANSIT", true},
		{"DELIVERED", true},
		{"COMPLETED", true},
		{"IN_PROGRESS", false},
		{"LOADING", true},
		{"EN_ROUTE", true},
		{"UNLOADING", true},
		{"PENDING_DRIVER", false},
		{"CANCELLED", false},
	}
	for _, tc := range cases {
		if got := tripStatusConsumesVehiclesLeft(tc.status); got != tc.want {
			t.Errorf("tripStatusConsumesVehiclesLeft(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
