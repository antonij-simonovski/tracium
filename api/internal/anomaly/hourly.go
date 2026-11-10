package anomaly

import (
	"math"
	"math/bits"
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
	// MinBaseTotal and MaxBaseRate, when set, skip hours whose baseline is too
	// thin or already too high to judge.
	MinBaseTotal int64
	MaxBaseRate  float64
	Bands        Bands
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
		if total < o.MinBaseTotal || (o.MaxBaseRate > 0 && base > o.MaxBaseRate) {
			continue
		}
		multiple := rate / math.Max(base, o.RateFloor)
		if sev := o.Bands.severity(multiple); sev != SeverityNone {
			out = append(out, Result{BucketMs: p.BucketMs, Observed: rate, Expected: base,
				Deviation: rate - base, Score: multiple, Direction: Spike, Severity: sev})
		}
	}
	return out
}

// RunStat summarizes one hour of runs by a per-run count: the typical run
// (median, or p95 where some runs legitimately go high) and the worst.
type RunStat struct {
	BucketMs int64
	Runs     int64
	Typical  float64
	Max      float64
}

const fallbackMinRuns = 5

type RunOptions struct {
	BaselineHours int
	DetectFromMs  int64
	MinBaseRuns   int64
	// FallbackBaselineHours widens a baseline with fewer than MinBaseRuns runs,
	// for low-volume workflows; it then needs fallbackMinRuns.
	FallbackBaselineHours int
	// AbsoluteMax flags a run with no usable baseline once it reaches this count.
	AbsoluteMax float64
	// MinMax is the absolute count a run must reach before it is worth flagging.
	MinMax float64
	Bands  Bands
}

// RunOutlier flags hours with a run far above the workflow's typical run: the
// worst run against the median of the baseline hours' typical runs. With no
// baseline it falls back to AbsoluteMax, with Expected 0.
func RunOutlier(series []RunStat, o RunOptions) []Result {
	var out []Result
	for i, p := range series {
		if p.BucketMs < o.DetectFromMs || p.Runs == 0 || p.Max < o.MinMax {
			continue
		}
		base, ok := runBaseline(series[max(0, i-o.BaselineHours):i], o.MinBaseRuns)
		if !ok && o.FallbackBaselineHours > 0 {
			base, ok = runBaseline(series[max(0, i-o.FallbackBaselineHours):i], fallbackMinRuns)
		}
		if !ok {
			if o.AbsoluteMax > 0 && p.Max >= o.AbsoluteMax {
				out = append(out, Result{BucketMs: p.BucketMs, Observed: p.Max, Deviation: p.Max,
					Score: p.Max, Direction: Spike, Severity: SeverityWarning})
			}
			continue
		}
		multiple := p.Max / math.Max(base, 1)
		if sev := o.Bands.severity(multiple); sev != SeverityNone {
			out = append(out, Result{BucketMs: p.BucketMs, Observed: p.Max, Expected: base,
				Deviation: p.Max - base, Score: multiple, Direction: Spike, Severity: sev})
		}
	}
	return out
}

func runBaseline(hours []RunStat, minRuns int64) (float64, bool) {
	var runs int64
	var typical []float64
	for _, b := range hours {
		if b.Runs > 0 {
			runs += b.Runs
			typical = append(typical, b.Typical)
		}
	}
	if runs == 0 || runs < minRuns {
		return 0, false
	}
	return medianOf(typical), true
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

// Activity is one client's active hours over the last four weeks, as bucket ms.
type Activity struct {
	Client string
	Hours  []int64
}

type SilenceOptions struct {
	NowMs int64
	// MinDays is how many active days make a client regular enough to judge.
	MinDays int
	// MinSilenceMs keeps hourly clients from paging on a short pause.
	MinSilenceMs int64
	// MissedBands are counts of expected hours missed.
	MissedBands Bands
	// Bands are multiples of the usual gap between days, for irregular clients.
	Bands Bands
	// A client active in at least BusyHours of the 24 before its silence uses
	// BusyMinSilenceMs and BusyMissedBands instead.
	BusyHours        int
	BusyMinSilenceMs int64
	BusyMissedBands  Bands
}

// SilenceResult is a client gone quiet: Observed is the silence and Expected
// the hours from its last activity to the first missed expected hour. Missed is
// 0 for a client judged by its gap between days, whose Expected is that gap.
type SilenceResult struct {
	Client     string
	Missed     int
	LastSeenMs int64
	Result
}

const (
	weekMs           = 7 * 24 * HourMs
	slotsPerWeek     = 168
	minSlotWeeks     = 2
	minExpectedSlots = 8
)

// Silence flags regular clients that missed hours they are usually active in.
// An hour of the week (UTC) is expected when the client was active in it in at
// least two of the last four weeks, so nights and weekends are never missed.
// Clients with too few expected hours fall back to the median gap between
// their active days.
func Silence(clients []Activity, o SilenceOptions) []SilenceResult {
	const dayMs = 24 * HourMs
	var out []SilenceResult
	for _, c := range clients {
		hours := sorted(c.Hours)
		var days []int64
		var weeks [slotsPerWeek]uint8
		for _, h := range hours {
			if d := h / dayMs * dayMs; len(days) == 0 || days[len(days)-1] != d {
				days = append(days, d)
			}
			if w := (o.NowMs - h) / weekMs; w >= 0 && w < 4 {
				weeks[slot(h)] |= 1 << w
			}
		}
		if len(days) < o.MinDays {
			continue
		}
		var expected [slotsPerWeek]bool
		n := 0
		for s, w := range weeks {
			if bits.OnesCount8(w) >= minSlotWeeks {
				expected[s] = true
				n++
			}
		}
		last := hours[len(hours)-1] + HourMs
		silence := o.NowMs - last
		minSilence, missedBands := o.MinSilenceMs, o.MissedBands
		if o.BusyHours > 0 && len(hours) >= o.BusyHours && hours[len(hours)-o.BusyHours] >= last-24*HourMs {
			minSilence, missedBands = o.BusyMinSilenceMs, o.BusyMissedBands
		}
		if silence < minSilence {
			continue
		}
		res := SilenceResult{Client: c.Client, LastSeenMs: last - HourMs}
		var multiple, interval float64
		var sev Severity
		var overdue int64
		if n >= minExpectedSlots {
			var first int64
			for h := last; h+HourMs <= o.NowMs; h += HourMs {
				if expected[slot(h)] {
					if res.Missed++; res.Missed == 1 {
						first = h
					}
					if res.Missed == int(missedBands.Info) {
						overdue = h
					}
				}
			}
			multiple = float64(res.Missed)
			sev = missedBands.severity(multiple)
			interval = float64(first - last)
		} else {
			gaps := make([]float64, 0, len(days)-1)
			for i := 1; i < len(days); i++ {
				gaps = append(gaps, float64(days[i]-days[i-1]))
			}
			interval = math.Max(medianOf(gaps), float64(HourMs))
			multiple = float64(silence) / interval
			sev = o.Bands.severity(multiple)
			overdue = last + int64(o.Bands.Info*interval)
		}
		if sev == SeverityNone {
			continue
		}
		res.Result = Result{
			BucketMs:  overdue / HourMs * HourMs,
			Observed:  float64(silence) / float64(HourMs),
			Expected:  interval / float64(HourMs),
			Deviation: (float64(silence) - interval) / float64(HourMs),
			Score:     -multiple,
			Direction: Drop,
			Severity:  sev,
		}
		out = append(out, res)
	}
	return out
}

func slot(h int64) int { return int(h / HourMs % slotsPerWeek) }

func sorted(xs []int64) []int64 {
	out := append([]int64(nil), xs...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Volume is one hour's count of events (e.g. spans rejected at ingest).
type Volume struct {
	BucketMs int64
	N        int64
}

type VolumeOptions struct {
	BaselineHours int
	DetectFromMs  int64
	MinCount      int64
	// Bands are multiples of the baseline hourly average; a zero baseline is
	// Bands.Warning.
	Bands Bands
}

// VolumeSpike flags hours whose count is a multiple of the trailing hourly average.
func VolumeSpike(series []Volume, o VolumeOptions) []Result {
	var out []Result
	for i, p := range series {
		if p.BucketMs < o.DetectFromMs || p.N < o.MinCount {
			continue
		}
		var sum int64
		for _, b := range series[max(0, i-o.BaselineHours):i] {
			sum += b.N
		}
		base := float64(sum) / float64(o.BaselineHours)
		multiple := o.Bands.Warning
		if base > 0 {
			multiple = float64(p.N) / base
		}
		if sev := o.Bands.severity(multiple); sev != SeverityNone {
			out = append(out, Result{BucketMs: p.BucketMs, Observed: float64(p.N), Expected: base,
				Deviation: float64(p.N) - base, Score: multiple, Direction: Spike, Severity: sev})
		}
	}
	return out
}

// Presence is one hour's runs and, per step name, how many of them ran it.
type Presence struct {
	BucketMs int64
	Runs     int64
	Steps    map[string]int64
}

type PresenceOptions struct {
	BaselineHours         int
	FallbackBaselineHours int
	DetectFromMs          int64
	MinBaseRuns           int64
	MinRuns               int64
	// Expected is the baseline share of runs that makes a step expected.
	Expected float64
	// TopSteps caps the baseline steps considered, most frequent first.
	TopSteps int
	// Warning and Critical are the presence shares below which a step is missing.
	Warning, Critical float64
}

// PresenceResult names the worst missing step and how many others went missing
// in the same hour. Observed is its share of runs, Expected its baseline share.
type PresenceResult struct {
	Step   string
	Others int
	Result
}

// PresenceDrop flags hours in which a step that runs in nearly every run stopped
// appearing: one result per hour, for the step with the lowest presence.
func PresenceDrop(series []Presence, o PresenceOptions) []PresenceResult {
	var out []PresenceResult
	for i, p := range series {
		if p.BucketMs < o.DetectFromMs || p.Runs < o.MinRuns {
			continue
		}
		runs, steps := presenceBaseline(series[max(0, i-o.BaselineHours):i])
		if runs < o.MinBaseRuns && o.FallbackBaselineHours > 0 {
			runs, steps = presenceBaseline(series[max(0, i-o.FallbackBaselineHours):i])
		}
		if runs < o.MinBaseRuns {
			continue
		}
		var res PresenceResult
		found := 0
		for _, name := range topSteps(steps, o.TopSteps) {
			base := float64(steps[name]) / float64(runs)
			cur := float64(p.Steps[name]) / float64(p.Runs)
			if base < o.Expected || cur >= o.Warning {
				continue
			}
			if found++; found == 1 || cur < res.Observed || (cur == res.Observed && base > res.Expected) {
				res.Step, res.Result = name, Result{BucketMs: p.BucketMs, Observed: cur, Expected: base,
					Deviation: cur - base, Score: -base / math.Max(cur, 0.01), Direction: Drop}
			}
		}
		if found == 0 {
			continue
		}
		res.Others, res.Severity = found-1, SeverityWarning
		if res.Observed < o.Critical {
			res.Severity = SeverityCritical
		}
		out = append(out, res)
	}
	return out
}

func presenceBaseline(hours []Presence) (int64, map[string]int64) {
	var runs int64
	steps := map[string]int64{}
	for _, h := range hours {
		runs += h.Runs
		for name, n := range h.Steps {
			steps[name] += n
		}
	}
	return runs, steps
}

func topSteps(steps map[string]int64, n int) []string {
	names := make([]string, 0, len(steps))
	for name := range steps {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if steps[names[i]] != steps[names[j]] {
			return steps[names[i]] > steps[names[j]]
		}
		return names[i] < names[j]
	})
	return names[:min(n, len(names))]
}
