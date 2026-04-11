package triprating

import (
	"math"
	"testing"
)

func TestFoldTripRatingsToProfile_empty(t *testing.T) {
	if v := FoldTripRatingsToProfile(nil); v != 0 {
		t.Fatalf("empty: got %v want 0", v)
	}
	if v := FoldTripRatingsToProfile([]int{}); v != 0 {
		t.Fatalf("empty slice: got %v want 0", v)
	}
}

func TestFoldTripRatingsToProfile_firstIsExact(t *testing.T) {
	if v := FoldTripRatingsToProfile([]int{10}); math.Abs(v-10) > 1e-9 {
		t.Fatalf("single 10: got %v", v)
	}
}

func TestFoldTripRatingsToProfile_gradualDrop(t *testing.T) {
	// 10 then 5 should not become 5 immediately (user scenario).
	v := FoldTripRatingsToProfile([]int{10, 5})
	if v <= 5.01 || v >= 9.99 {
		t.Fatalf("10 then 5: got %v, want between 5 and 10, not instant 5", v)
	}
	// Expected: 10 + 0.35*(5-10) = 8.25
	want := 8.25
	if math.Abs(v-want) > 1e-6 {
		t.Fatalf("10 then 5: got %v want %v", v, want)
	}
}

func TestFoldTripRatingsToProfile_clamp(t *testing.T) {
	v := FoldTripRatingsToProfile([]int{1})
	if v != 1 {
		t.Fatalf("got %v", v)
	}
}
