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
		series[i] = RunStat{BucketMs: int64(i) * HourMs, Runs: 5, Median: 3, Max: 5}
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
		busy[i] = RunStat{BucketMs: int64(i) * HourMs, Runs: 5, Median: 30, Max: 45}
	}
	busy[24].Max = 60
	if got := RunOutlier(busy, o); len(got) != 0 {
		t.Errorf("agent that normally makes 30 calls flagged at 60: %+v", got)
	}

	cold := []RunStat{{Runs: 1, Median: 2, Max: 2}, {BucketMs: 24 * HourMs, Runs: 1, Median: 50, Max: 50}}
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

func TestSilence(t *testing.T) {
	const day = 24 * HourMs
	now := 30 * day
	o := SilenceOptions{NowMs: now, MinDays: 4, MinSilenceMs: 3 * HourMs, Bands: Bands{Info: 2, Warning: 4, Critical: 8}}
	daysBack := func(step int64, last int64, n int) []int64 {
		var out []int64
		for i := 0; i < n; i++ {
			out = append(out, last-int64(i)*step)
		}
		return out
	}
	hourly := func(from, to int64) []int64 {
		var out []int64
		for h := from; h <= to; h += HourMs {
			out = append(out, h)
		}
		return out
	}

	got := Silence([]Activity{{Client: "hourly-bot", Days: daysBack(day, 29*day, 20), RecentHours: hourly(now-48*HourMs, now-10*HourMs)}}, o)
	if len(got) != 1 || got[0].Client != "hourly-bot" || got[0].Severity != SeverityCritical {
		t.Fatalf("hourly client silent 9h = %+v, want critical", got)
	}

	got = Silence([]Activity{{Client: "daily", Days: daysBack(day, 26*day, 20)}}, o)
	if len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Fatalf("daily client silent 3 days = %+v, want info", got)
	}

	weekly := Activity{Client: "weekly", Days: daysBack(7*day, 24*day, 4)}
	if got := Silence([]Activity{weekly}, o); len(got) != 0 {
		t.Errorf("weekly client 6 days after its last run flagged: %+v", got)
	}
	weekly.Days = daysBack(7*day, 14*day, 4)
	if got := Silence([]Activity{weekly}, o); len(got) != 1 {
		t.Errorf("weekly client silent 15 days not flagged: %+v", got)
	}

	var office []int64
	for d := now - 2*day; d < now; d += day {
		office = append(office, hourly(d+9*HourMs, d+17*HourMs)...)
	}
	nightOwl := Activity{Client: "office", Days: daysBack(day, 29*day, 20), RecentHours: office}
	o.NowMs = now + 8*HourMs
	if got := Silence([]Activity{nightOwl}, o); len(got) != 0 {
		t.Errorf("business-hours client flagged overnight: %+v", got)
	}

	o.NowMs = now
	if got := Silence([]Activity{{Client: "new", Days: daysBack(day, 20*day, 3)}}, o); len(got) != 0 {
		t.Errorf("client with 3 active days judged: %+v", got)
	}
}
