package cargo

import "testing"

func TestTripStatusConsumesVehiclesLeft(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"LOADING", true},
		{"EN_ROUTE", true},
		{"UNLOADING", true},
		{"PENDING_DRIVER", false},
		{"ASSIGNED", false},
		{"COMPLETED", false},
		{"CANCELLED", false},
	}
	for _, tc := range cases {
		if got := tripStatusConsumesVehiclesLeft(tc.status); got != tc.want {
			t.Errorf("tripStatusConsumesVehiclesLeft(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
