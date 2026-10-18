package anomaly

import (
	"math"
	"testing"
)

const day = int64(86_400_000)

// costOpts is a representative cost-metric configuration used across tests:
// spike-only, 28-day baseline, floors that suppress trivial dollar moves.
func costOpts(detectFrom int64) Options {
	return Options{
		BaselineDays:      28,
		MinBaseline:       7,
		MinBaselineActive: 5,
		DetectFromMs:      detectFrom,
		ZInfo:             3,
		ZWarning:          4.5,
		ZCritical:         6,
		AbsFloor:          1,
		VolumeFloor:       1,
		Spike:             true,
	}
}

// series builds a gap-filled series starting at bucket 0 from per-bucket values;
// Support mirrors Value (the cost/runs case).
func series(vals ...float64) []Point {
	pts := make([]Point, len(vals))
	for i, v := range vals {
		pts[i] = Point{BucketMs: int64(i) * day, Value: v, Support: v}
	}
	return pts
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestDetect_SteadySeriesHasNoAnomaly(t *testing.T) {
	// Thirty days hovering around $10 with mild noise, then a normal $11 — nothing
	// should flag.
	vals := []float64{10, 11, 9, 10, 12, 8, 10, 11, 9, 10, 10, 12, 9, 11, 10, 9, 11, 10, 10, 12, 8, 10, 11, 9, 10, 10, 12, 9, 11}
	got := Detect(series(vals...), costOpts(0))
	if len(got) != 0 {
		t.Fatalf("expected no anomalies on a steady series, got %d: %+v", len(got), got)
	}
}

func TestDetect_CostSpike(t *testing.T) {
	vals := append(repeat(10, 28), 120) // 28 steady days, then a 12× jump
	got := Detect(series(vals...), costOpts(0))
	if len(got) != 1 {
		t.Fatalf("expected exactly one anomaly, got %d: %+v", len(got), got)
	}
	a := got[0]
	if a.Direction != Spike {
		t.Errorf("direction = %q, want spike", a.Direction)
	}
	if a.Severity != SeverityCritical {
		t.Errorf("severity = %q, want critical", a.Severity)
	}
	if a.Expected != 10 {
		t.Errorf("expected (baseline median) = %v, want 10", a.Expected)
	}
	if a.Observed != 120 {
		t.Errorf("observed = %v, want 120", a.Observed)
	}
	if a.Score <= 0 {
		t.Errorf("score = %v, want positive for a spike", a.Score)
	}
}

func TestDetect_RunVolumeDropToZero(t *testing.T) {
	// A workflow doing ~200 runs/day that suddenly does none — a drop anomaly, the
	// silent-failure signal. Cost-style opts but drop-enabled, spike-off.
	o := Options{
		BaselineDays: 28, MinBaseline: 7, DetectFromMs: 0,
		ZInfo: 3, ZWarning: 4.5, ZCritical: 6,
		AbsFloor: 5, VolumeFloor: 10, Drop: true,
	}
	vals := append(repeat(200, 28), 0)
	got := Detect(series(vals...), o)
	if len(got) != 1 {
		t.Fatalf("expected one drop anomaly, got %d: %+v", len(got), got)
	}
	if got[0].Direction != Drop {
		t.Errorf("direction = %q, want drop", got[0].Direction)
	}
	if got[0].Deviation >= 0 {
		t.Errorf("deviation = %v, want negative for a drop", got[0].Deviation)
	}
}

func TestDetect_DirectionGating(t *testing.T) {
	// A drop in a spike-only config must not flag, and vice versa.
	dropSeries := series(append(repeat(200, 28), 0)...)
	if got := Detect(dropSeries, costOpts(0)); len(got) != 0 {
		t.Errorf("spike-only config flagged a drop: %+v", got)
	}
	spikeSeries := series(append(repeat(10, 28), 120)...)
	dropOnly := costOpts(0)
	dropOnly.Spike, dropOnly.Drop = false, true
	if got := Detect(spikeSeries, dropOnly); len(got) != 0 {
		t.Errorf("drop-only config flagged a spike: %+v", got)
	}
}

func TestDetect_SparseBaselineNotEnoughActiveDays(t *testing.T) {
	// A workspace that only started sending traffic three days ago: its 28-day
	// baseline is gap-filled to mostly zeros, with just 3 active days before a big
	// day. MinBaselineActive is 5, so nothing is scored — the "enough traces to
	// form a baseline" rule. Without it, the busy day would flag as a huge spike
	// against a ~0 median.
	vals := make([]float64, 28)
	vals[24], vals[25], vals[26] = 10, 12, 11 // only 3 active baseline days
	vals = append(vals, 120)                  // a big day
	if got := Detect(series(vals...), costOpts(0)); len(got) != 0 {
		t.Fatalf("expected no anomalies with too few active baseline days, got %+v", got)
	}
}

func TestDetect_EnoughActiveDaysAllowsDetection(t *testing.T) {
	// Same shape but with 6 active baseline days (≥ MinBaselineActive of 5): once
	// there is enough real history, the spike is scored.
	vals := make([]float64, 28)
	for i := 22; i < 28; i++ {
		vals[i] = 10 // 6 active baseline days
	}
	vals = append(vals, 120)
	got := Detect(series(vals...), costOpts(0))
	if len(got) != 1 {
		t.Fatalf("expected one anomaly once the baseline has enough active days, got %+v", got)
	}
}

func TestDetect_ColdStartInsufficientBaseline(t *testing.T) {
	// Only 4 days of history precede a big jump; MinBaseline is 7, so it must not
	// flag — a brand-new workspace cannot be anomalous against itself.
	got := Detect(series(10, 10, 10, 10, 500), costOpts(0))
	if len(got) != 0 {
		t.Fatalf("expected no anomaly during cold start, got %d: %+v", len(got), got)
	}
}

func TestDetect_FlatBaselineZeroMAD(t *testing.T) {
	// A perfectly flat baseline gives MAD=0. A jump beyond AbsFloor must still
	// flag (as critical) rather than divide by zero or be silently dropped.
	vals := append(repeat(10, 28), 50)
	got := Detect(series(vals...), costOpts(0))
	if len(got) != 1 {
		t.Fatalf("expected one anomaly on a flat baseline jump, got %d: %+v", len(got), got)
	}
	if got[0].Severity != SeverityCritical {
		t.Errorf("severity = %q, want critical for a flat-baseline break", got[0].Severity)
	}
	if math.IsInf(got[0].Score, 0) || math.IsNaN(got[0].Score) {
		t.Errorf("score = %v, want finite", got[0].Score)
	}
}

func TestDetect_FlatBaselineSmallMoveSuppressedByAbsFloor(t *testing.T) {
	// Flat at $10, then $10.50: a real deviation against zero variance, but below
	// the $1 AbsFloor, so it must not flag. Guards the flat-baseline path from
	// firing on trivial moves.
	vals := append(repeat(10, 28), 10.5)
	if got := Detect(series(vals...), costOpts(0)); len(got) != 0 {
		t.Fatalf("expected AbsFloor to suppress a sub-dollar move, got %+v", got)
	}
}

func TestDetect_VolumeFloorSuppressesSparseSeries(t *testing.T) {
	// A tiny-traffic workflow: baseline of $0.10/day, then $0.80 — an 8× jump by
	// ratio, but max support ($0.80) is under the $1 VolumeFloor, so it is noise.
	vals := append(repeat(0.10, 28), 0.80)
	if got := Detect(series(vals...), costOpts(0)); len(got) != 0 {
		t.Fatalf("expected VolumeFloor to suppress a sparse series, got %+v", got)
	}
}

func TestDetect_DetectFromExcludesBaselinePrefix(t *testing.T) {
	// A spike sits inside the baseline-only prefix (before DetectFromMs); a later
	// steady point is the only candidate. The prefix spike must not be reported,
	// but it does still pollute... no: robust stats absorb it, so the later steady
	// point is normal. Net: zero anomalies emitted.
	vals := append(repeat(10, 20), 200) // spike at index 20
	vals = append(vals, repeat(10, 10)...)
	detectFrom := int64(25) * day // only buckets >= day 25 may be emitted
	got := Detect(series(vals...), costOpts(detectFrom))
	for _, a := range got {
		if a.BucketMs < detectFrom {
			t.Fatalf("emitted an anomaly before DetectFromMs: %+v", a)
		}
	}
	if len(got) != 0 {
		t.Fatalf("expected no anomalies after the prefix spike, got %+v", got)
	}
}

func TestDetect_SeverityBands(t *testing.T) {
	// Baseline median 10, MAD 1 → sigma ≈ 1.4826. Choose observed values landing
	// in each band: z = (obs-10)/1.4826.
	//   info:     z≈3.4  -> obs 15
	//   warning:  z≈5.4  -> obs 18
	//   critical: z≈8.1  -> obs 22
	base := []float64{9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 10}
	cases := []struct {
		obs  float64
		want Severity
	}{
		{15, SeverityInfo},
		{18, SeverityWarning},
		{22, SeverityCritical},
	}
	for _, tc := range cases {
		got := Detect(series(append(append([]float64{}, base...), tc.obs)...), costOpts(0))
		if len(got) != 1 {
			t.Fatalf("obs %v: expected one anomaly, got %d: %+v", tc.obs, len(got), got)
		}
		if got[0].Severity != tc.want {
			t.Errorf("obs %v: severity = %q, want %q (score %.2f)", tc.obs, got[0].Severity, tc.want, got[0].Score)
		}
	}
}

func TestDetect_ErrorRateUsesRunCountSupport(t *testing.T) {
	// Error rate is a ratio; Support carries the run count. A rate jump computed
	// from only a few runs must be suppressed by the VolumeFloor even though the
	// rate deviation is large.
	o := Options{
		BaselineDays: 28, MinBaseline: 7, DetectFromMs: 0,
		ZInfo: 3, ZWarning: 4.5, ZCritical: 6,
		AbsFloor: 0.05, VolumeFloor: 20, Spike: true,
	}
	pts := make([]Point, 0, 29)
	for i := 0; i < 28; i++ {
		pts = append(pts, Point{BucketMs: int64(i) * day, Value: 0.01, Support: 500}) // steady low error rate, high traffic
	}
	// Final day: 50% error rate but only 4 runs — support below floor.
	pts = append(pts, Point{BucketMs: 28 * day, Value: 0.5, Support: 4})
	if got := Detect(pts, o); len(got) != 0 {
		t.Fatalf("expected low-support error-rate spike to be suppressed, got %+v", got)
	}
	// Same rate jump with 500 runs must flag.
	pts[28].Support = 500
	got := Detect(pts, o)
	if len(got) != 1 || got[0].Severity == SeverityNone {
		t.Fatalf("expected a well-supported error-rate spike to flag, got %+v", got)
	}
}

func TestDetect_EmptySeries(t *testing.T) {
	if got := Detect(nil, costOpts(0)); got != nil {
		t.Errorf("nil series: got %+v, want nil", got)
	}
	if got := Detect([]Point{}, costOpts(0)); got != nil {
		t.Errorf("empty series: got %+v, want nil", got)
	}
}

func TestMedianOf(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{5}, 5},
		{[]float64{3, 1, 2}, 2},
		{[]float64{4, 1, 3, 2}, 2.5},
	}
	for _, tc := range cases {
		if got := medianOf(tc.in); got != tc.want {
			t.Errorf("medianOf(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
