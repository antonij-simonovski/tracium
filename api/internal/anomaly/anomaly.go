// Package anomaly is the statistical core of Tracium's anomaly detection. It is
// deliberately free of any database or HTTP dependency: it scores an
// evenly-bucketed numeric series against its own recent history and returns the
// buckets that deviate significantly. The query layer feeds it series pulled
// from the daily rollup; a future background/alerting job can reuse the exact
// same scoring without change.
//
// Method — robust z-score over a trailing baseline:
//
//	z = (observed − median) / (1.4826 × MAD)
//
// Median and MAD (median absolute deviation) are used instead of mean and
// standard deviation because LLM cost/latency/volume series are heavy-tailed:
// the very spikes we hunt would poison a mean-based baseline, masking themselves
// and the next few points. Robust statistics ignore a handful of outliers in the
// baseline, so a spike does not hide the spike after it. 1.4826 rescales MAD to
// be a consistent estimator of the standard deviation for normal data, so the
// familiar z thresholds (≈3σ) keep their usual meaning.
//
// Two floors keep the score from firing on noise, and both must be cleared in
// addition to the z threshold:
//
//   - AbsFloor: the absolute deviation |observed − expected| must exceed a
//     metric-specific minimum. This kills the "$0.01 → $0.05 is 5×!" problem and
//     is also what makes a flat (MAD=0) baseline safe to score.
//   - VolumeFloor: the series must carry enough activity (Support) to be worth
//     scoring at all, so a barely-used workflow with two runs a week does not
//     generate a stream of alerts.
package anomaly

import (
	"math"
	"sort"
)

// Direction is the sign of a deviation relative to the baseline.
type Direction string

const (
	// Spike is an upward deviation (observed above the baseline).
	Spike Direction = "spike"
	// Drop is a downward deviation (observed below the baseline) — e.g. run
	// volume collapsing toward zero, the signature of a silently failing workflow.
	Drop Direction = "drop"
)

// Severity buckets the magnitude of a deviation for ranking and display.
type Severity string

const (
	// SeverityNone is the zero value: below the info threshold, not an anomaly.
	SeverityNone     Severity = ""
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Point is one bucket of a series. Value is the metric being scored (cost in
// USD, run count, or error rate 0..1). Support is the magnitude the VolumeFloor
// gates on: for cost/runs it equals Value, but for a ratio metric like error
// rate it is the underlying run count, so the floor suppresses rates computed
// from a handful of runs rather than the rate itself.
type Point struct {
	BucketMs int64
	Value    float64
	Support  float64
}

// Options tunes detection. The zero value detects nothing (no directions
// enabled, zero thresholds); callers set the fields explicitly per metric.
type Options struct {
	// BaselineDays is how many buckets immediately preceding a candidate form its
	// baseline. The baseline is trailing (it never includes the candidate), so
	// each bucket is scored against only the history available before it.
	BaselineDays int
	// MinBaseline is the fewest baseline points required to score a bucket. Below
	// this the bucket is skipped (cold start), never flagged — a new workspace
	// with two days of data must not light up.
	MinBaseline int
	// MinBaselineActive is the fewest baseline buckets that must carry activity
	// (Support > 0) before the baseline is trusted. The series is gap-filled with
	// zeros, so a workspace with only a few days of real traffic still presents a
	// full-length baseline of mostly-zero days; without this gate the first busy
	// day would flag as an enormous spike against a ~0 baseline. Requiring a
	// minimum number of *active* days is the "enough traces to form a baseline"
	// rule — until it is met, nothing is scored.
	MinBaselineActive int
	// DetectFromMs bounds which buckets may be emitted: buckets before it serve
	// only as baseline for later ones. This lets the caller fetch history before
	// the display window without reporting anomalies inside that prefix.
	DetectFromMs int64

	// Z thresholds for |robust z|, ascending. A bucket at/above ZCritical is
	// critical, at/above ZWarning is warning, at/above ZInfo is info; below ZInfo
	// it is not an anomaly. Callers must set ZInfo ≤ ZWarning ≤ ZCritical.
	ZInfo     float64
	ZWarning  float64
	ZCritical float64

	// AbsFloor is the minimum |observed − expected| for a bucket to flag,
	// in the metric's own units.
	AbsFloor float64
	// VolumeFloor is the minimum activity (max Support over baseline and
	// candidate) for a bucket to be considered.
	VolumeFloor float64

	// Weekly drops an anomaly whose value is usual for its weekday: within a
	// quarter of the range the same weekday took in the baseline.
	// Daily buckets only, so a quiet weekend is not an outage.
	Weekly bool

	// Spike / Drop enable upward / downward detection. A metric where only one
	// direction is meaningful (cost spikes matter, cost dips are not incidents)
	// enables just that one.
	Spike bool
	Drop  bool
}

// Result is one detected anomaly.
type Result struct {
	BucketMs  int64
	Observed  float64
	Expected  float64 // baseline median
	Deviation float64 // Observed − Expected
	Score     float64 // signed robust z (NaN/Inf coerced away; see below)
	Direction Direction
	Severity  Severity
}

// Detect scores every eligible bucket of series and returns the anomalies,
// ascending by bucket time (the input order). series must be sorted ascending by
// BucketMs and gap-filled onto a fixed grid — absent buckets present as zero
// Value/Support — so that a genuine drop to zero is a real point in the series
// rather than a missing row. A nil/empty series yields no results.
func Detect(series []Point, o Options) []Result {
	if len(series) == 0 {
		return nil
	}
	var out []Result
	for i := range series {
		p := series[i]
		if p.BucketMs < o.DetectFromMs {
			continue // baseline-only prefix
		}
		lo := i - o.BaselineDays
		if lo < 0 {
			lo = 0
		}
		base := series[lo:i] // trailing, excludes the candidate itself
		if len(base) < o.MinBaseline {
			continue // cold start: not enough history to judge
		}
		// Baseline statistics are computed over the ACTIVE days only (Support > 0),
		// not the gap-filled zeros. Idle days would otherwise drag the median to ~0
		// and make the first busy day look like an enormous spike; measuring against
		// a typical active day is what makes "5× your usual" meaningful. Requiring
		// MinBaselineActive of them is the "enough traces to form a baseline" rule.
		active := activePoints(base)
		if len(active) < o.MinBaselineActive {
			continue // not enough real traffic in the baseline to trust it
		}

		med := median(active)
		dev := p.Value - med
		dir := Spike
		if dev < 0 {
			dir = Drop
		}
		if (dir == Spike && !o.Spike) || (dir == Drop && !o.Drop) {
			continue
		}
		// Volume floor, direction-aware. A spike is only trustworthy if the
		// candidate bucket itself carries enough activity (a 50% error rate over
		// four runs is noise, not an incident), so it gates on the candidate's
		// Support. A drop's candidate is near zero by definition, so gating on it
		// would suppress the very drops we want; instead require that the baseline
		// had real activity to fall from, gating on the baseline's typical
		// Support.
		support := p.Support
		if dir == Drop {
			support = medianSupport(active)
		}
		if support < o.VolumeFloor {
			continue // too little activity to be worth scoring
		}
		if math.Abs(dev) < o.AbsFloor {
			continue // deviation too small in absolute terms
		}

		sigma := 1.4826 * mad(active, med)
		var z float64
		if sigma > 0 {
			z = dev / sigma
		} else {
			// Flat baseline (every recent bucket identical): variance is zero, so
			// a z-score is undefined. Having already cleared AbsFloor and
			// VolumeFloor, a departure from a perfectly stable baseline is a strong
			// signal — score it at the critical threshold, signed by direction, so
			// it ranks as a decisive anomaly without an infinite score.
			z = math.Copysign(o.ZCritical, dev)
		}

		sev := severity(math.Abs(z), o)
		if sev == SeverityNone || (o.Weekly && usualForWeekday(series[lo:i+1], dir)) {
			continue
		}
		out = append(out, Result{
			BucketMs:  p.BucketMs,
			Observed:  p.Value,
			Expected:  med,
			Deviation: dev,
			Score:     z,
			Direction: dir,
			Severity:  sev,
		})
	}
	return out
}

// usualForWeekday reports whether the last point sits within the range of the
// points 7, 14, 21... buckets before it, widened by a quarter. Idle days count
// once the series has shown activity, so a workflow that never runs on Sundays
// is not down on one.
func usualForWeekday(series []Point, dir Direction) bool {
	first := len(series)
	for i, s := range series[:len(series)-1] {
		if s.Support > 0 {
			first = i
			break
		}
	}
	p := series[len(series)-1]
	lo, hi, n := math.Inf(1), math.Inf(-1), 0
	for i := len(series) - 8; i >= first; i -= 7 {
		lo, hi, n = math.Min(lo, series[i].Value), math.Max(hi, series[i].Value), n+1
	}
	if n == 0 {
		return false
	}
	if dir == Drop {
		return p.Value >= 0.75*lo
	}
	return p.Value <= 1.25*hi
}

func severity(absZ float64, o Options) Severity {
	switch {
	case absZ >= o.ZCritical:
		return SeverityCritical
	case absZ >= o.ZWarning:
		return SeverityWarning
	case absZ >= o.ZInfo:
		return SeverityInfo
	default:
		return SeverityNone
	}
}

// median returns the median Value of the points. Points is not mutated.
func median(pts []Point) float64 {
	vals := make([]float64, len(pts))
	for i, p := range pts {
		vals[i] = p.Value
	}
	return medianOf(vals)
}

// mad returns the median absolute deviation of the points' Values from med.
func mad(pts []Point, med float64) float64 {
	dev := make([]float64, len(pts))
	for i, p := range pts {
		dev[i] = math.Abs(p.Value - med)
	}
	return medianOf(dev)
}

// medianOf returns the median of xs, mutating a copy (not xs). Empty → 0.
func medianOf(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	s := make([]float64, n)
	copy(s, xs)
	sort.Float64s(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// activePoints returns the baseline buckets that carry activity (Support > 0) —
// the days the baseline statistics are computed over, and whose count backs
// MinBaselineActive.
func activePoints(base []Point) []Point {
	active := make([]Point, 0, len(base))
	for _, p := range base {
		if p.Support > 0 {
			active = append(active, p)
		}
	}
	return active
}

// medianSupport is the median Support across the baseline — the typical activity
// level a drop is measured against.
func medianSupport(base []Point) float64 {
	sup := make([]float64, len(base))
	for i, p := range base {
		sup[i] = p.Support
	}
	return medianOf(sup)
}
