package anomaly

import "testing"

var testBands = Bands{Info: 3, Warning: 5, Critical: 10}

func hoursFrom(n int, f func(i int) Count) []Count {
	out := make([]Count, n)
	for i := range out {
		out[i] = f(i)
		out[i].BucketMs = int64(i) * HourMs
	}
	return out
}

func TestRateSpike(t *testing.T) {
	o := RateOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinHits: 5, MinRate: 0.05, RateFloor: 0.01, Bands: testBands}
	steady := func(i int) Count { return Count{Hits: 1, Total: 100} }

	spike := hoursFrom(25, steady)
	spike[24] = Count{BucketMs: 24 * HourMs, Hits: 30, Total: 100}
	got := RateSpike(spike, o)
	if len(got) != 1 || got[0].Severity != SeverityCritical || got[0].Observed != 0.3 {
		t.Fatalf("30%% vs 1%% baseline = %+v, want one critical", got)
	}

	if got := RateSpike(hoursFrom(25, steady), o); len(got) != 0 {
		t.Errorf("steady rate flagged: %+v", got)
	}

	few := hoursFrom(25, func(int) Count { return Count{} })
	few[24] = Count{BucketMs: 24 * HourMs, Hits: 3, Total: 4}
	if got := RateSpike(few, o); len(got) != 0 {
		t.Errorf("3 hits under MinHits flagged: %+v", got)
	}

	chronic := hoursFrom(25, func(int) Count { return Count{Hits: 20, Total: 100} })
	chronic[24].Hits = 30
	if got := RateSpike(chronic, o); len(got) != 0 {
		t.Errorf("30%% vs a 20%% baseline is not a spike: %+v", got)
	}
}

func TestRunOutlier(t *testing.T) {
	o := RunOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinBaseRuns: 20, MinMax: 10, Bands: testBands}
	series := make([]RunStat, 25)
	for i := range series {
		series[i] = RunStat{BucketMs: int64(i) * HourMs, Runs: 5, Typical: 3, Max: 5}
	}
	series[24].Max = 40
	got := RunOutlier(series, o)
	if len(got) != 1 || got[0].Severity != SeverityCritical || got[0].Expected != 3 {
		t.Fatalf("40 calls vs median 3 = %+v, want one critical", got)
	}

	series[24].Max = 9
	if got := RunOutlier(series, o); len(got) != 0 {
		t.Errorf("9 calls below MinMax flagged: %+v", got)
	}

	busy := make([]RunStat, 25)
	for i := range busy {
		busy[i] = RunStat{BucketMs: int64(i) * HourMs, Runs: 5, Typical: 30, Max: 45}
	}
	busy[24].Max = 60
	if got := RunOutlier(busy, o); len(got) != 0 {
		t.Errorf("agent that normally makes 30 calls flagged at 60: %+v", got)
	}

	cold := []RunStat{{Runs: 1, Typical: 2, Max: 2}, {BucketMs: 24 * HourMs, Runs: 1, Typical: 50, Max: 50}}
	if got := RunOutlier(cold, o); len(got) != 0 {
		t.Errorf("no baseline yet, flagged: %+v", got)
	}
}

func TestRatioDrop(t *testing.T) {
	o := DropOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinBase: 0.2, MinDen: 10_000, Bands: Bands{Info: 1.6, Warning: 2.5, Critical: 6}}
	series := make([]Ratio, 25)
	for i := range series {
		series[i] = Ratio{BucketMs: int64(i) * HourMs, Num: 8_000, Den: 10_000}
	}
	series[24].Num = 500
	got := RatioDrop(series, o)
	if len(got) != 1 || got[0].Severity != SeverityCritical || got[0].Direction != Drop {
		t.Fatalf("80%% → 5%% = %+v, want one critical drop", got)
	}

	series[24].Num = 7_000
	if got := RatioDrop(series, o); len(got) != 0 {
		t.Errorf("80%% → 70%% flagged: %+v", got)
	}

	uncached := make([]Ratio, 25)
	for i := range uncached {
		uncached[i] = Ratio{BucketMs: int64(i) * HourMs, Num: 500, Den: 10_000}
	}
	uncached[24].Num = 0
	if got := RatioDrop(uncached, o); len(got) != 0 {
		t.Errorf("workflow that barely caches flagged: %+v", got)
	}

	series[24] = Ratio{BucketMs: 24 * HourMs, Num: 0, Den: 900}
	if got := RatioDrop(series, o); len(got) != 0 {
		t.Errorf("low-volume hour flagged: %+v", got)
	}
}

func TestRunOutlierThinBaseline(t *testing.T) {
	o := RunOptions{BaselineHours: 24, FallbackBaselineHours: 168, DetectFromMs: 168 * HourMs,
		MinBaseRuns: 20, AbsoluteMax: 50, MinMax: 10, Bands: testBands}
	lowVol := make([]RunStat, 169)
	for i := range lowVol {
		lowVol[i].BucketMs = int64(i) * HourMs
		if i%3 == 0 {
			lowVol[i].Runs, lowVol[i].Typical, lowVol[i].Max = 1, 3, 3
		}
	}
	lowVol[168] = RunStat{BucketMs: 168 * HourMs, Runs: 1, Typical: 200, Max: 200}
	got := RunOutlier(lowVol, o)
	if len(got) != 1 || got[0].Severity != SeverityCritical || got[0].Expected != 3 {
		t.Fatalf("200 calls vs a 7-day median of 3 = %+v, want one critical", got)
	}

	cold := make([]RunStat, 169)
	for i := range cold {
		cold[i].BucketMs = int64(i) * HourMs
	}
	cold[100] = RunStat{BucketMs: 100 * HourMs, Runs: 2, Typical: 3, Max: 3}
	cold[168] = RunStat{BucketMs: 168 * HourMs, Runs: 1, Typical: 60, Max: 60}
	got = RunOutlier(cold, o)
	if len(got) != 1 || got[0].Severity != SeverityWarning || got[0].Expected != 0 {
		t.Fatalf("60 calls with no baseline = %+v, want the absolute backstop", got)
	}
	cold[168].Max = 49
	if got := RunOutlier(cold, o); len(got) != 0 {
		t.Errorf("49 calls with no baseline flagged: %+v", got)
	}

	o.FallbackBaselineHours, o.AbsoluteMax = 0, 0
	if got := RunOutlier(lowVol, o); len(got) != 0 {
		t.Errorf("thin 24h baseline without fallback flagged: %+v", got)
	}
}

func TestRunOutlierP95(t *testing.T) {
	o := RunOptions{BaselineHours: 24, FallbackBaselineHours: 168, DetectFromMs: 24 * HourMs,
		MinBaseRuns: 20, AbsoluteMax: 30, MinMax: 10, Bands: testBands}
	crawler := make([]RunStat, 25)
	for i := range crawler {
		crawler[i] = RunStat{BucketMs: int64(i) * HourMs, Runs: 5, Typical: 12, Max: 12}
	}
	if got := RunOutlier(crawler, o); len(got) != 0 {
		t.Errorf("crawler that always repeats 12 times flagged: %+v", got)
	}
	o.DetectFromMs = 0
	if got := RunOutlier(crawler, o); len(got) != 0 {
		t.Errorf("crawler's first hours flagged: %+v", got)
	}

	sometimes := make([]RunStat, 25)
	for i := range sometimes {
		sometimes[i] = RunStat{BucketMs: int64(i) * HourMs, Runs: 20, Typical: 20, Max: 25}
	}
	sometimes[24].Max = 50
	o.DetectFromMs = 24 * HourMs
	if got := RunOutlier(sometimes, o); len(got) != 0 {
		t.Errorf("50 repeats vs a p95 of 20 flagged: %+v", got)
	}
	sometimes[24].Max = 200
	if got := RunOutlier(sometimes, o); len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Errorf("200 repeats vs a p95 of 20 = %+v, want critical", got)
	}
}

func TestSilence(t *testing.T) {
	const day = 24 * HourMs
	// 1970-01-05 was a Monday; now is Monday 00:00 of the fifth week.
	monday := 4*day + 4*7*day
	opts := func(now int64) SilenceOptions {
		return SilenceOptions{NowMs: now, MinDays: 4, MinSilenceMs: 3 * HourMs,
			MissedBands: Bands{Info: 3, Warning: 6, Critical: 12}, Bands: Bands{Info: 2, Warning: 4, Critical: 8}}
	}
	office := func(start, end int64, until int64) []int64 {
		var out []int64
		for d := monday - 28*day; d < until; d += day {
			if (d/day+3)%7 >= 5 {
				continue
			}
			for h := d + start*HourMs; h < d+end*HourMs && h < until; h += HourMs {
				out = append(out, h)
			}
		}
		return out
	}

	fri := monday - 3*day
	hist := Activity{Client: "office", Hours: office(9, 18, monday)}
	for name, now := range map[string]int64{"Saturday 20:30": fri + day + 20*HourMs + HourMs/2, "Sunday 14:30": fri + 2*day + 14*HourMs + HourMs/2, "Monday 09:30": monday + 9*HourMs + HourMs/2} {
		if got := Silence([]Activity{hist}, opts(now)); len(got) != 0 {
			t.Errorf("office client flagged on %s: %+v", name, got)
		}
	}

	got := Silence([]Activity{hist}, opts(monday+12*HourMs))
	if len(got) != 1 || got[0].Missed != 3 || got[0].Severity != SeverityInfo || got[0].LastSeenMs != fri+17*HourMs {
		t.Fatalf("office client silent through Monday 12:00 = %+v, want 3 missed hours", got)
	}

	dst := Activity{Client: "office"}
	for _, h := range office(9, 18, monday) {
		if h < monday-7*day {
			dst.Hours = append(dst.Hours, h)
		}
	}
	for _, h := range office(10, 19, monday) {
		if h >= monday-7*day {
			dst.Hours = append(dst.Hours, h)
		}
	}
	for _, now := range []int64{monday + 10*HourMs, fri + day + 20*HourMs} {
		if got := Silence([]Activity{dst}, opts(now)); len(got) != 0 {
			t.Errorf("office client an hour later after a clock change flagged at %d: %+v", now, got)
		}
	}

	var allDay []int64
	for h := monday - 28*day; h < monday-5*HourMs; h += HourMs {
		allDay = append(allDay, h)
	}
	got = Silence([]Activity{{Client: "24/7", Hours: allDay}}, opts(monday))
	if len(got) != 1 || got[0].Missed != 5 || got[0].Observed != 5 {
		t.Fatalf("24/7 client silent 5h = %+v, want 5 missed hours", got)
	}

	var weekly []int64
	for w := int64(1); w <= 4; w++ {
		weekly = append(weekly, monday-w*7*day+9*HourMs, monday-w*7*day+10*HourMs)
	}
	if got := Silence([]Activity{{Client: "weekly", Hours: weekly}}, opts(monday-HourMs)); len(got) != 0 {
		t.Errorf("weekly client flagged before its day: %+v", got)
	}
	if got := Silence([]Activity{{Client: "weekly", Hours: weekly}}, opts(monday+14*day)); len(got) != 1 || got[0].Missed != 0 {
		t.Errorf("weekly client silent two weeks = %+v, want the daily-gap alert", got)
	}

	var daily []int64
	for d := int64(4); d <= 25; d++ {
		daily = append(daily, monday-d*day+3*HourMs)
	}
	if got := Silence([]Activity{{Client: "daily", Hours: daily}}, opts(monday)); len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Errorf("daily client silent 4 days = %+v, want info", got)
	}

	if got := Silence([]Activity{{Client: "new", Hours: daily[:3]}}, opts(monday)); len(got) != 0 {
		t.Errorf("client with 3 active days judged: %+v", got)
	}
}

func TestRateSpikeBaselineGate(t *testing.T) {
	o := RateOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinHits: 3, MinRate: 0.05, RateFloor: 0.01,
		MinBaseTotal: 20, MaxBaseRate: 0.05, Bands: testBands}
	stuck := hoursFrom(25, func(int) Count { return Count{Hits: 0, Total: 10} })
	stuck[24] = Count{BucketMs: 24 * HourMs, Hits: 5, Total: 10}
	if got := RateSpike(stuck, o); len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("5 of 10 stuck vs none = %+v, want one critical", got)
	}

	noRoot := hoursFrom(25, func(int) Count { return Count{Hits: 10, Total: 10} })
	if got := RateSpike(noRoot, o); len(got) != 0 {
		t.Errorf("workflow that never emits a root flagged: %+v", got)
	}

	fresh := hoursFrom(25, func(int) Count { return Count{} })
	fresh[24] = Count{BucketMs: 24 * HourMs, Hits: 10, Total: 10}
	if got := RateSpike(fresh, o); len(got) != 0 {
		t.Errorf("new workflow with no baseline flagged: %+v", got)
	}
}

func TestSilenceBusy(t *testing.T) {
	const day = 24 * HourMs
	now := 4*day + 4*7*day
	var hours []int64
	for h := now - 28*day; h < now-2*HourMs; h += HourMs {
		hours = append(hours, h)
	}
	o := SilenceOptions{NowMs: now, MinDays: 4, MinSilenceMs: 3 * HourMs,
		MissedBands: Bands{Info: 3, Warning: 6, Critical: 12}, Bands: Bands{Info: 2, Warning: 4, Critical: 8}}
	if got := Silence([]Activity{{Client: "svc", Hours: hours}}, o); len(got) != 0 {
		t.Errorf("2h silence flagged without the busy rule: %+v", got)
	}
	o.BusyHours, o.BusyMinSilenceMs, o.BusyMissedBands = 20, HourMs, Bands{Info: 1, Warning: 3, Critical: 6}
	got := Silence([]Activity{{Client: "svc", Hours: hours}}, o)
	if len(got) != 1 || got[0].Missed != 2 || got[0].Severity != SeverityInfo || got[0].BucketMs != now-2*HourMs {
		t.Fatalf("busy service silent 2h = %+v, want info at the first missed hour", got)
	}

	var sparse []int64
	for _, h := range hours {
		if h%(2*HourMs) == 0 {
			sparse = append(sparse, h)
		}
	}
	if got := Silence([]Activity{{Client: "svc", Hours: sparse}}, o); len(got) != 0 {
		t.Errorf("service active every other hour flagged on a short gap: %+v", got)
	}
}

func TestVolumeSpike(t *testing.T) {
	o := VolumeOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinCount: 20, Bands: Bands{Info: 5, Warning: 5, Critical: 20}}
	series := make([]Volume, 25)
	for i := range series {
		series[i] = Volume{BucketMs: int64(i) * HourMs}
	}
	series[24].N = 25
	if got := VolumeSpike(series, o); len(got) != 1 || got[0].Severity != SeverityWarning {
		t.Fatalf("25 rejected after none = %+v, want warning", got)
	}
	for i := 0; i < 24; i++ {
		series[i].N = 1
	}
	series[24].N = 30
	if got := VolumeSpike(series, o); len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("30 rejected vs 1/h = %+v, want critical", got)
	}
	for i := 0; i < 24; i++ {
		series[i].N = 10
	}
	if got := VolumeSpike(series, o); len(got) != 0 {
		t.Errorf("30 rejected vs 10/h flagged: %+v", got)
	}
	series[24].N = 19
	for i := 0; i < 24; i++ {
		series[i].N = 0
	}
	if got := VolumeSpike(series, o); len(got) != 0 {
		t.Errorf("19 rejected under MinCount flagged: %+v", got)
	}
}

func TestPresenceDrop(t *testing.T) {
	o := PresenceOptions{BaselineHours: 24, FallbackBaselineHours: 168, DetectFromMs: 24 * HourMs,
		MinBaseRuns: 20, MinRuns: 5, Expected: 0.9, TopSteps: 30, Warning: 0.5, Critical: 0.1}
	series := make([]Presence, 25)
	for i := range series {
		series[i] = Presence{BucketMs: int64(i) * HourMs, Runs: 10,
			Steps: map[string]int64{"agent": 10, "retrieve": 10, "guard": 10, "rerank": 3}}
	}
	series[24].Steps = map[string]int64{"agent": 10, "retrieve": 0, "guard": 3, "rerank": 0}
	got := PresenceDrop(series, o)
	if len(got) != 1 || got[0].Step != "retrieve" || got[0].Others != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("retrieve and guard dropped = %+v, want one critical for retrieve and 1 more", got)
	}

	series[24].Steps = map[string]int64{"agent": 10, "retrieve": 6, "guard": 10, "rerank": 0}
	if got := PresenceDrop(series, o); len(got) != 0 {
		t.Errorf("step at 60%% or an optional step flagged: %+v", got)
	}

	series[24] = Presence{BucketMs: 24 * HourMs, Runs: 4, Steps: map[string]int64{"agent": 4}}
	if got := PresenceDrop(series, o); len(got) != 0 {
		t.Errorf("hour with 4 runs flagged: %+v", got)
	}

	cold := make([]Presence, 25)
	for i := range cold {
		cold[i].BucketMs = int64(i) * HourMs
	}
	cold[0] = Presence{Runs: 10, Steps: map[string]int64{"retrieve": 10}}
	cold[24] = Presence{BucketMs: 24 * HourMs, Runs: 10, Steps: map[string]int64{}}
	if got := PresenceDrop(cold, o); len(got) != 0 {
		t.Errorf("10 baseline runs judged: %+v", got)
	}

	o.TopSteps = 1
	series[24] = Presence{BucketMs: 24 * HourMs, Runs: 10, Steps: map[string]int64{"agent": 10}}
	if got := PresenceDrop(series, o); len(got) != 0 {
		t.Errorf("steps outside TopSteps flagged: %+v", got)
	}
}

func TestLevelCross(t *testing.T) {
	o := CrossOptions{DetectFromMs: 168 * HourMs, LagHours: 168, MinN: 10, Above: 0.85, Below: 0.7}
	series := make([]Level, 169)
	for i := range series {
		series[i] = Level{BucketMs: int64(i) * HourMs, N: 20, Value: 0.5}
	}
	series[168].Value = 0.9
	if got := LevelCross(series, o); len(got) != 1 || got[0].Severity != SeverityWarning || got[0].Expected != 0.5 {
		t.Fatalf("p95 0.5 -> 0.9 = %+v, want one warning", got)
	}

	series[0].Value = 0.8
	if got := LevelCross(series, o); len(got) != 0 {
		t.Errorf("already at 0.8 a week ago flagged: %+v", got)
	}

	series[0] = Level{BucketMs: 0}
	if got := LevelCross(series, o); len(got) != 0 {
		t.Errorf("no calls a week ago flagged: %+v", got)
	}
}

func sharesFrom(n int, f func(i int) Shares) []Shares {
	out := make([]Shares, n)
	for i := range out {
		out[i] = f(i)
		out[i].BucketMs = int64(i) * HourMs
	}
	return out
}

func TestShareShift(t *testing.T) {
	o := ShareOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinBaseTotal: 20,
		MaxBaseShare: 0.02, MinShare: 0.2, MinCount: 10, Critical: 0.5}
	steady := func(int) Shares { return Shares{Total: 20, By: map[string]int64{"gpt-4o": 20}} }

	fallback := sharesFrom(25, steady)
	fallback[24].Total, fallback[24].By = 20, map[string]int64{"gpt-4o": 8, "gpt-4o-mini": 12}
	got := ShareShift(fallback, o)
	if len(got) != 1 || got[0].Key != "gpt-4o-mini" || got[0].Severity != SeverityCritical || got[0].Observed != 0.6 {
		t.Fatalf("fallback to gpt-4o-mini = %+v, want one critical at 60%%", got)
	}

	fallback[24].By = map[string]int64{"gpt-4o": 15, "gpt-4o-mini": 5}
	if got := ShareShift(fallback, o); len(got) != 0 {
		t.Errorf("5 calls under MinCount flagged: %+v", got)
	}

	mixed := sharesFrom(25, func(int) Shares { return Shares{Total: 20, By: map[string]int64{"gpt-4o": 14, "gpt-4o-mini": 6}} })
	mixed[24].By = map[string]int64{"gpt-4o": 6, "gpt-4o-mini": 14}
	if got := ShareShift(mixed, o); len(got) != 0 {
		t.Errorf("model already 30%% of the baseline flagged: %+v", got)
	}

	fresh := sharesFrom(25, func(int) Shares { return Shares{} })
	fresh[24] = Shares{BucketMs: 24 * HourMs, Total: 20, By: map[string]int64{"gpt-4o": 20}}
	if got := ShareShift(fresh, o); len(got) != 0 {
		t.Errorf("new workflow with no baseline flagged: %+v", got)
	}
}

func outputsFrom(n int, f func(i int) Outputs) []Outputs {
	out := make([]Outputs, n)
	for i := range out {
		out[i] = f(i)
		out[i].BucketMs = int64(i) * HourMs
	}
	return out
}

func TestDuplicateShare(t *testing.T) {
	o := DuplicateOptions{BaselineHours: 24, DetectFromMs: 24 * HourMs, MinBaseTotal: 20,
		MinCalls: 10, MinInputs: 5, MinShare: 0.3, MaxBaseShare: 0.1, Critical: 0.7}
	varied := func(int) Outputs { return Outputs{Total: 10} }

	stuck := outputsFrom(25, varied)
	stuck[24].Total, stuck[24].Repeats = 20, map[uint64]Repeat{7: {Dup: 15, Inputs: 16}}
	got := DuplicateShare(stuck, o)
	if len(got) != 1 || got[0].Hash != 7 || got[0].Severity != SeverityCritical || got[0].Observed != 0.75 {
		t.Fatalf("canned answer for 16 inputs = %+v, want one critical at 75%%", got)
	}

	stuck[24].Repeats = map[uint64]Repeat{7: {Dup: 15, Inputs: 2}}
	if got := DuplicateShare(stuck, o); len(got) != 0 {
		t.Errorf("same answer for 2 distinct inputs flagged: %+v", got)
	}

	classifier := outputsFrom(25, func(int) Outputs {
		return Outputs{Total: 10, Repeats: map[uint64]Repeat{1: {Dup: 5, Inputs: 6}, 2: {Dup: 3, Inputs: 4}}}
	})
	classifier[24].Repeats = map[uint64]Repeat{1: {Dup: 8, Inputs: 9}}
	if got := DuplicateShare(classifier, o); len(got) != 0 {
		t.Errorf("classifier with fixed answers flagged: %+v", got)
	}

	few := outputsFrom(25, varied)
	few[24].Total, few[24].Repeats = 8, map[uint64]Repeat{7: {Dup: 7, Inputs: 8}}
	if got := DuplicateShare(few, o); len(got) != 0 {
		t.Errorf("8 calls under MinCalls flagged: %+v", got)
	}
}
