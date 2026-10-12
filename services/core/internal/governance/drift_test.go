package governance

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// evenBaseline is a decile-scale reference: ten bins of equal expected share.
func evenBaseline() *Baseline {
	edges := make([]float64, 11)
	shares := make([]float64, 10)
	for i := range edges {
		edges[i] = float64(i) / 10.0
	}
	for i := range shares {
		shares[i] = 0.1
	}
	return &Baseline{FeatureName: "calibrated_pd", NSamples: 5000, BinEdges: edges, ExpectedShare: shares}
}

func observations(n int, f func(i int) float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

func TestPSIOfTheBaselineItselfIsZero(t *testing.T) {
	// Observations spread evenly across the bins are the reference population, so
	// the index must come back near zero rather than at some arbitrary floor.
	result, err := evenBaseline().PSI(observations(1000, func(i int) float64 {
		return float64(i%1000) / 1000.0
	}))
	if err != nil {
		t.Fatalf("PSI: %v", err)
	}
	if result.Value > 0.01 {
		t.Errorf("PSI of the reference population = %f, expected < 0.01", result.Value)
	}
	if result.Status != "STABLE" {
		t.Errorf("status = %s, want STABLE", result.Status)
	}
	if result.SampleSize != 1000 {
		t.Errorf("sample size = %d, want 1000", result.SampleSize)
	}
}

func TestPSIDetectsConcentrationShift(t *testing.T) {
	// Everything piled into the first bin is a large, unambiguous shift.
	result, err := evenBaseline().PSI(observations(500, func(int) float64 { return 0.01 }))
	if err != nil {
		t.Fatalf("PSI: %v", err)
	}
	if result.Value < 0.25 {
		t.Errorf("PSI of a fully concentrated population = %f, expected critical", result.Value)
	}
	if result.Status != "CRITICAL_DRIFT" {
		t.Errorf("status = %s, want CRITICAL_DRIFT", result.Status)
	}
}

func TestPSIRefusesSmallSamples(t *testing.T) {
	// Below the floor the index mostly reports the sample size, so it must be an
	// error rather than a number a dashboard can green-light.
	for _, n := range []int{0, 1, 199} {
		_, err := evenBaseline().PSI(observations(n, func(i int) float64 { return float64(i) / float64(n+1) }))
		if err == nil {
			t.Errorf("PSI accepted %d observations, below the %d floor", n, MinimumSampleSize)
		}
	}
	if _, err := evenBaseline().PSI(observations(201, func(i int) float64 { return float64(i) / 202 })); err != nil {
		t.Errorf("PSI rejected an adequate sample: %v", err)
	}
}

func TestPSIBinsSumToTheIndex(t *testing.T) {
	result, err := evenBaseline().PSI(observations(400, func(i int) float64 {
		return math.Mod(float64(i)*0.017, 1.0)
	}))
	if err != nil {
		t.Fatalf("PSI: %v", err)
	}
	if len(result.Bins) != 10 {
		t.Fatalf("got %d bins, want 10", len(result.Bins))
	}
	sum := 0.0
	counted := 0
	for _, bin := range result.Bins {
		sum += bin.Contribution
		counted += bin.Count
	}
	if math.Abs(sum-result.Value) > 1e-4 {
		t.Errorf("bin contributions sum to %f but PSI is %f", sum, result.Value)
	}
	if counted != 400 {
		t.Errorf("bins account for %d of 400 observations", counted)
	}
}

func TestStatusThresholds(t *testing.T) {
	cases := []struct {
		psi  float64
		want string
	}{
		{0.0, "STABLE"},
		{0.099, "STABLE"},
		{0.10, "MODERATE_DRIFT"},
		{0.249, "MODERATE_DRIFT"},
		{0.25, "CRITICAL_DRIFT"},
		{1.4, "CRITICAL_DRIFT"},
	}
	for _, c := range cases {
		if got := Status(c.psi); got != c.want {
			t.Errorf("Status(%f) = %s, want %s", c.psi, got, c.want)
		}
	}
}

func TestLoadBaselineRejectsMalformedFiles(t *testing.T) {
	dir := t.TempDir()

	write := func(name string, v any) string {
		path := filepath.Join(dir, name)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		return path
	}

	if _, err := LoadBaseline(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("LoadBaseline accepted a missing file")
	}

	oneEdge := write("one_edge.json", Baseline{FeatureName: "f", BinEdges: []float64{0}, ExpectedShare: []float64{}})
	if _, err := LoadBaseline(oneEdge); err == nil {
		t.Error("LoadBaseline accepted a single edge")
	}

	mismatched := write("mismatch.json", Baseline{FeatureName: "f", BinEdges: []float64{0, 0.5, 1}, ExpectedShare: []float64{0.5}})
	if _, err := LoadBaseline(mismatched); err == nil {
		t.Error("LoadBaseline accepted edges and shares of different length")
	}

	unnormalised := write("unnormalised.json", Baseline{
		FeatureName: "f", BinEdges: []float64{0, 0.5, 1}, ExpectedShare: []float64{0.5, 0.7},
	})
	if _, err := LoadBaseline(unnormalised); err == nil {
		t.Error("LoadBaseline accepted shares that do not sum to 1")
	}

	good := write("good.json", Baseline{
		FeatureName: "calibrated_pd", BinEdges: []float64{0, 0.5, 1}, ExpectedShare: []float64{0.5, 0.5},
	})
	b, err := LoadBaseline(good)
	if err != nil {
		t.Fatalf("LoadBaseline on a valid file: %v", err)
	}
	if b.FeatureName != "calibrated_pd" || len(b.BinEdges) != 3 {
		t.Errorf("parsed baseline = %+v", b)
	}
}

func TestBucketHandlesEdgesAndOutliers(t *testing.T) {
	b := evenBaseline()
	last := len(b.BinEdges) - 2

	cases := []struct {
		value float64
		want  int
	}{
		{0.0, 0},
		{0.05, 0},
		{0.099, 0},
		{0.1, 1},
		{0.95, last},
		{1.0, last},
		{2.0, last}, // above every edge
		{-3.0, 0},   // below every edge
	}
	for _, c := range cases {
		if got := b.bucket(c.value); got != c.want {
			t.Errorf("bucket(%v) = %d, want %d", c.value, got, c.want)
		}
	}
}
