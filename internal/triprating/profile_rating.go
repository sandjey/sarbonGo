package triprating

import "math"

const (
	profileMinStars = 1.0
	profileMaxStars = 5.0
	// alphaCap limits how fast the score can move on each new rating (after the first).
	alphaCap = 0.35
)

// FoldTripRatingsToProfile folds chronological per-trip scores (1.0–5.0, steps 0.5) into one profile number.
// The first rating becomes the baseline; each later rating pulls the value toward the new
// score with a bounded step (EMA-like), so one low score does not instantly collapse a high average.
func FoldTripRatingsToProfile(stars []float64) float64 {
	if len(stars) == 0 {
		return 0
	}
	r := stars[0]
	for i := 1; i < len(stars); i++ {
		n := i + 1
		alpha := math.Min(alphaCap, 1.0/math.Sqrt(float64(n)))
		r += alpha * (stars[i] - r)
	}
	if r < profileMinStars {
		return profileMinStars
	}
	if r > profileMaxStars {
		return profileMaxStars
	}
	return r
}
