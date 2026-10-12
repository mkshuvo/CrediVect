package governance

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// Baseline is the training-time histogram of calibrated probability of default
// written by engine/train.py. Drift is measured against it, not against a
// hand-typed number.
type Baseline struct {
	FeatureName   string    `json:"feature_name"`
	NSamples      int64     `json:"n_samples"`
	BinEdges      []float64 `json:"bin_edges"`
	ExpectedShare []float64 `json:"expected_share"`
}

func LoadBaseline(path string) (*Baseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read PSI baseline %s: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("parse PSI baseline %s: %w", path, err)
	}
	if len(b.BinEdges) < 2 || len(b.ExpectedShare) != len(b.BinEdges)-1 {
		return nil, fmt.Errorf("PSI baseline %s is malformed", path)
	}
	sum := 0.0
	for _, s := range b.ExpectedShare {
		sum += s
	}
	if math.Abs(sum-1.0) > 0.01 {
		return nil, fmt.Errorf("PSI baseline shares sum to %.4f, expected 1.0", sum)
	}
	return &b, nil
}

// MinimumSampleSize is the smallest window for which a PSI verdict is
// meaningful. With fewer observations than this, empty bins dominate the index
// and the number reports the sample size rather than the population.
const MinimumSampleSize = 200

// PSI computes the population stability index of the observed PDs against the
// baseline bins. Convention: < 0.10 stable, 0.10-0.25 moderate, > 0.25 critical.
// Empty bins use a half-count continuity correction instead of an arbitrary
// floor, which keeps small windows from reading as drift.
func (b *Baseline) PSI(observations []float64) (Result, error) {
	result := Result{SampleSize: len(observations), Feature: b.FeatureName}
	if len(observations) == 0 {
		return result, fmt.Errorf("no scored applications in window")
	}
	if len(observations) < MinimumSampleSize {
		return result, fmt.Errorf("insufficient sample: %d scored applications in window, %d required for a stable PSI", len(observations), MinimumSampleSize)
	}
	counts := make([]int, len(b.ExpectedShare))
	for _, v := range observations {
		counts[b.bucket(v)]++
	}
	n := float64(len(observations))
	for i := range counts {
		actual := (float64(counts[i]) + 0.5) / (n + 0.5*float64(len(counts)))
		expected := math.Max(b.ExpectedShare[i], 1e-6)
		result.Bins = append(result.Bins, Bin{
			Lower: b.BinEdges[i], Upper: b.BinEdges[i+1],
			Expected: b.ExpectedShare[i], Actual: actual, Count: counts[i],
			Contribution: (actual - expected) * math.Log(actual/expected),
		})
		result.Value += result.Bins[i].Contribution
	}
	result.Value = math.Round(result.Value*10000) / 10000
	result.Status = Status(result.Value)
	return result, nil
}

func (b *Baseline) bucket(v float64) int {
	for i := 1; i < len(b.BinEdges); i++ {
		if v < b.BinEdges[i] {
			return i - 1
		}
	}
	return len(b.BinEdges) - 2
}

func Status(psi float64) string {
	switch {
	case psi >= 0.25:
		return "CRITICAL_DRIFT"
	case psi >= 0.10:
		return "MODERATE_DRIFT"
	default:
		return "STABLE"
	}
}

type Bin struct {
	Lower        float64 `json:"lower"`
	Upper        float64 `json:"upper"`
	Expected     float64 `json:"expected_share"`
	Actual       float64 `json:"actual_share"`
	Count        int     `json:"count"`
	Contribution float64 `json:"psi_contribution"`
}

type Result struct {
	Feature    string  `json:"feature_name"`
	Value      float64 `json:"psi_value"`
	Status     string  `json:"status"`
	SampleSize int     `json:"sample_size"`
	Bins       []Bin   `json:"bins"`
}
