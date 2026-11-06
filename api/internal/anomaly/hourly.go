package anomaly

import (
	"math"
	"sort"
)

// Hourly detectors compare each hour with the hours before it using plain
// thresholds. Their Score is the observed/expected multiple (negative for a
// drop), not a z-score. Series are gap-free and ordered by BucketMs.

const HourMs = int64(3600_000)

// Bands are the multiples of the baseline at which a deviation becomes info,
// warning and critical.
type Bands struct{ Info, Warning, Critical float64 }

func (b Bands) severity(multiple float64) Severity {
	switch {
	case multiple >= b.Critical:
		return SeverityCritical
	case multiple >= b.Warning:
		return SeverityWarning
	case multiple >= b.Info:
		return SeverityInfo
	}
	return SeverityNone
}

// Count is one hour's Hits out of Total (e.g. truncated LLM calls out of all).
type Count struct {
	BucketMs    int64
	Hits, Total int64
}

type RateOptions struct {
	BaselineHours int
	DetectFromMs  int64
	MinHits       int64
	MinRate       float64
	// RateFloor stands in for a baseline rate below it, so a first failure after
	// a clean baseline is not an infinite multiple.
	RateFloor float64
	Bands     Bands
}

// RateSpike flags hours whose hit rate is a multiple of the trailing baseline rate.
func RateSpike(series []Count, o RateOptions) []Result {
	var out []Result
	for i, p := range series {
		if p.BucketMs < o.DetectFromMs || p.Total == 0 || p.Hits < o.MinHits {
			continue
		}
		var hits, total int64
		for _, b := range series[max(0, i-o.BaselineHours):i] {
			hits, total = hits+b.Hits, total+b.Total
		}
		rate := float64(p.Hits) / float64(p.Total)
		if rate < o.MinRate {
			continue
		}
		var base float64
		if total > 0 {
			base = float64(hits) / float64(total)
		}
		multiple := rate / math.Max(base, o.RateFloor)
		if sev := o.Bands.severity(multiple); sev != SeverityNone {
			out = append(out, Result{BucketMs: p.BucketMs, Observed: rate, Expected: base,
				Deviation: rate - base, Score: multiple, Direction: Spike, Severity: sev})
		}
	}
	return out
}

// RunStat summarizes one hour of runs by a per-run count: its median and worst.
type RunStat struct {
	BucketMs int64
	Runs     int64
	Median   float64
	Max      float64
}

type RunOptions struct {
	BaselineHours int
	DetectFromMs  int64
	MinBaseRuns   int64
	// MinMax is the absolute count a run must reach before it is worth flagging.
	MinMax float64
	Bands  Bands
}

// RunOutlier flags hours with a run far above the workflow's typical run: the
// worst run against the median of the baseline hours' medians.
func RunOutlier(series []RunStat, o RunOptions) []Result {
	var out []Result
	for i, p := range series {
		if p.BucketMs < o.DetectFromMs || p.Runs == 0 || p.Max < o.MinMax {
			continue
		}
		var runs int64
		var medians []float64
		for _, b := range series[max(0, i-o.BaselineHours):i] {
			if b.Runs > 0 {
				runs += b.Runs
				medians = append(medians, b.Median)
			}
		}
		if runs < o.MinBaseRuns {
			continue
		}
		base := medianOf(medians)
		multiple := p.Max / math.Max(base, 1)
		if sev := o.Bands.severity(multiple); sev != SeverityNone {
			out = append(out, Result{BucketMs: p.BucketMs, Observed: p.Max, Expected: base,
				Deviation: p.Max - base, Score: multiple, Direction: Spike, Severity: sev})
		}
	}
	return out
}

// Ratio is one hour's Num/Den (e.g. cache-read tokens over input tokens).
type Ratio struct {
	BucketMs int64
	Num, Den float64
}

type DropOptions struct {
	BaselineHours int
	DetectFromMs  int64
	// MinBase skips series whose baseline ratio is too low to matter.
	MinBase float64
	// MinDen is the volume an hour needs, in Den units, to be scored.
	MinDen float64
	// Bands are multiples of baseline/observed: 2 means the ratio halved.
	Bands Bands
}

// RatioDrop flags hours whose ratio fell to a fraction of the trailing baseline.
func RatioDrop(series []Ratio, o DropOptions) []Result {
	var out []Result
	for i, p := range series {
		if p.BucketMs < o.DetectFromMs || p.Den < o.MinDen {
			continue
		}
		var num, den float64
		for _, b := range series[max(0, i-o.BaselineHours):i] {
			num, den = num+b.Num, den+b.Den
		}
		if den < o.MinDen {
			continue
		}
		base := num / den
		if base < o.MinBase {
			continue
		}
		cur := p.Num / p.Den
		multiple := base / math.Max(cur, base/100)
		if sev := o.Bands.severity(multiple); sev != SeverityNone {
			out = append(out, Result{BucketMs: p.BucketMs, Observed: cur, Expected: base,
				Deviation: cur - base, Score: -multiple, Direction: Drop, Severity: sev})
		}
	}
	return out
}

// Activity is one client's history: the UTC days it ran on and the hours it ran
// in over the recent window, both as bucket ms.
type Activity struct {
	Client      string
	Days        []int64
	RecentHours []int64
}

type SilenceOptions struct {
	NowMs int64
	// MinDays is how many active days make a client regular enough to judge.
	MinDays int
	// MinSilenceMs keeps hourly clients from paging on a short pause.
	MinSilenceMs int64
	// Bands are multiples of the client's usual interval.
	Bands Bands
}

// SilenceResult is a client gone quiet: Observed and Expected are the silence
// and the usual interval, in hours. BucketMs is the hour it became overdue.
type SilenceResult struct {
	Client string
	Result
}

// Silence flags regular clients whose silence is a multiple of their usual
// interval between runs. A client that skips days uses the median gap between
// its days; one that runs daily uses the longest recent pause between its
// active hours, so a client that sleeps overnight is judged by that gap.
func Silence(clients []Activity, o SilenceOptions) []SilenceResult {
	const dayMs = 24 * HourMs
	var out []SilenceResult
	for _, c := range clients {
		if len(c.Days) < o.MinDays {
			continue
		}
		days := sorted(c.Days)
		gaps := make([]float64, 0, len(days)-1)
		for i := 1; i < len(days); i++ {
			gaps = append(gaps, float64(days[i]-days[i-1]))
		}
		interval := medianOf(gaps)
		last := days[len(days)-1] + dayMs
		if hours := sorted(c.RecentHours); len(hours) > 0 {
			last = hours[len(hours)-1] + HourMs
			if interval <= float64(dayMs) && len(hours) > 1 {
				var longest int64
				for i := 1; i < len(hours); i++ {
					longest = max(longest, hours[i]-hours[i-1])
				}
				interval = math.Min(float64(longest), float64(dayMs))
			}
		}
		interval = math.Max(interval, float64(HourMs))
		silence := float64(o.NowMs - last)
		if silence < float64(o.MinSilenceMs) {
			continue
		}
		multiple := silence / interval
		sev := o.Bands.severity(multiple)
		if sev == SeverityNone {
			continue
		}
		overdue := last + int64(o.Bands.Info*interval)
		out = append(out, SilenceResult{Client: c.Client, Result: Result{
			BucketMs:  overdue / HourMs * HourMs,
			Observed:  silence / float64(HourMs),
			Expected:  interval / float64(HourMs),
			Deviation: (silence - interval) / float64(HourMs),
			Score:     -multiple,
			Direction: Drop,
			Severity:  sev,
		}})
	}
	return out
}

func sorted(xs []int64) []int64 {
	out := append([]int64(nil), xs...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
