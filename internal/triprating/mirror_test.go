package triprating

import (
	"reflect"
	"testing"
)

func TestTripMirrorTargets(t *testing.T) {
	tests := []struct {
		name                string
		raterKind           string
		rateeKind           string
		dispatcherRateeRole string
		want                []string
	}{
		{
			name:                "driver rates cargo manager",
			raterKind:           "driver",
			rateeKind:           "dispatcher",
			dispatcherRateeRole: dispatcherRateeRoleCargoManager,
			want:                []string{"rating_from_driver"},
		},
		{
			name:                "driver rates driver manager",
			raterKind:           "driver",
			rateeKind:           "dispatcher",
			dispatcherRateeRole: dispatcherRateeRoleDriverManager,
			want:                []string{"rating_driver_to_dm"},
		},
		{
			name:      "driver manager rates driver",
			raterKind: "driver_manager",
			rateeKind: "driver",
			want:      []string{"rating_dm_to_driver"},
		},
		{
			name:      "driver manager rates cargo manager",
			raterKind: "driver_manager",
			rateeKind: "dispatcher",
			want:      []string{"rating_dm_to_cm"},
		},
		{
			name:      "cargo manager rates driver manager",
			raterKind: "dispatcher",
			rateeKind: "driver_manager",
			want:      []string{"rating_cm_to_dm"},
		},
		{
			name:      "cargo manager rates driver",
			raterKind: "dispatcher",
			rateeKind: "driver",
			want:      []string{"rating_from_dispatcher"},
		},
		{
			name:      "unknown mapping",
			raterKind: "driver",
			rateeKind: "driver",
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tripMirrorTargets(tc.raterKind, tc.rateeKind, tc.dispatcherRateeRole)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tripMirrorTargets(%q, %q, %q) = %#v, want %#v", tc.raterKind, tc.rateeKind, tc.dispatcherRateeRole, got, tc.want)
			}
		})
	}
}
