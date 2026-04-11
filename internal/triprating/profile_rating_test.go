package triprating

import (
	"math"
	"testing"
)

func TestFoldTripRatingsToProfile_empty(t *testing.T) {
	if v := FoldTripRatingsToProfile(nil); v != 0 {
		t.Fatalf("empty: got %v want 0", v)
	}
	if v := FoldTripRatingsToProfile([]float64{}); v != 0 {
		t.Fatalf("empty slice: got %v", v)
	}
}

func TestFoldTripRatingsToProfile_firstIsExact(t *testing.T) {
	if v := FoldTripRatingsToProfile([]float64{5}); math.Abs(v-5) > 1e-9 {
		t.Fatalf("single 5: got %v", v)
	}
}

func TestFoldTripRatingsToProfile_gradualDrop(t *testing.T) {
	// 5 then 2.5 should not become 2.5 immediately (user scenario).
	v := FoldTripRatingsToProfile([]float64{5, 2.5})
	if v <= 2.51 || v >= 4.99 {
		t.Fatalf("5 then 2.5: got %v, want between 2.5 and 5, not instant 2.5", v)
	}
	want := 5 + 0.35*(2.5-5)
	if math.Abs(v-want) > 1e-6 {
		t.Fatalf("5 then 2.5: got %v want %v", v, want)
	}
}

func TestFoldTripRatingsToProfile_clamp(t *testing.T) {
	v := FoldTripRatingsToProfile([]float64{1})
	if v != 1 {
		t.Fatalf("got %v", v)
	}
}

func TestValidateStars(t *testing.T) {
	for _, s := range []float64{1, 1.5, 3.5, 5, 4.5} {
		if err := ValidateStars(s); err != nil {
			t.Fatalf("%v: %v", s, err)
		}
	}
	for _, s := range []float64{0.5, 5.5, 3.33, 2.25} {
		if err := ValidateStars(s); err == nil {
			t.Fatalf("%v: expected error", s)
		}
	}
}
