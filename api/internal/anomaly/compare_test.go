package anomaly

import (
	"testing"

	"github.com/tracium/api/internal/model"
)

func find(fs []Finding, metric string) *Finding {
	for i := range fs {
		if fs[i].Metric == metric {
			return &fs[i]
		}
	}
	return nil
}

func base() model.VersionStats {
	return model.VersionStats{Runs: 400, ErrorRuns: 4, LLMCalls: 1200, TruncatedCalls: 10, RefusedCalls: 0, ToolCalls: 400, ToolErrors: 4,
		CostPerRunP50: 0.02, LatencyP95: 2000, LLMCallsPerRunP50: 3, OutputTokensPerRunP50: 300, CacheRead: 500, InputTokens: 1000}
}

func TestCompareRates(t *testing.T) {
	o := DefaultCompareOptions()
	cases := []struct {
		name              string
		prevHits, prevN   int64
		hits, n           int64
		verdict, severity string
	}{
		{"same rate", 10, 1200, 1, 120, Same, ""},
		{"25 points up", 10, 1200, 30, 120, Regressed, "critical"},
		{"10 points up", 10, 1200, 13, 120, Regressed, "warning"},
		{"under 5 points", 10, 1200, 6, 120, Same, ""},
		{"tiny sample never regresses", 0, 1200, 4, 4, Same, ""},
		{"z below 3", 20, 200, 6, 30, Same, ""},
		{"mirrored improvement", 300, 1200, 2, 120, Improved, "critical"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, n := base(), base()
			p.TruncatedCalls, p.LLMCalls = c.prevHits, c.prevN
			n.TruncatedCalls, n.LLMCalls = c.hits, c.n
			f := find(CompareVersions(p, n, o), "truncation_rate")
			if f == nil || f.Verdict != c.verdict || f.Severity != c.severity {
				t.Fatalf("got %+v, want %s %s", f, c.verdict, c.severity)
			}
		})
	}
}

func TestCompareRatios(t *testing.T) {
	o := DefaultCompareOptions()
	cases := []struct {
		name              string
		metric            string
		edit              func(*model.VersionStats)
		verdict, severity string
	}{
		{"cost at 1.3x under the floor", "cost_per_run", func(s *model.VersionStats) { s.CostPerRunP50 = 0.026 }, Same, ""},
		{"cost at 2x", "cost_per_run", func(s *model.VersionStats) { s.CostPerRunP50 = 0.04 }, Regressed, "critical"},
		{"cost at 1.25x", "cost_per_run", func(s *model.VersionStats) { s.CostPerRunP50 = 0.025 }, Same, ""},
		{"cost at 1.75x", "cost_per_run", func(s *model.VersionStats) { s.CostPerRunP50 = 0.035 }, Regressed, "warning"},
		{"latency at 1.5x", "latency_p95", func(s *model.VersionStats) { s.LatencyP95 = 3000 }, Regressed, "warning"},
		{"latency up under the floor", "latency_p95", func(s *model.VersionStats) { s.LatencyP95 = 2400 }, Same, ""},
		{"latency halved", "latency_p95", func(s *model.VersionStats) { s.LatencyP95 = 1000 }, Improved, "critical"},
		{"llm calls up by one", "llm_calls_per_run", func(s *model.VersionStats) { s.LLMCallsPerRunP50 = 4 }, Same, ""},
		{"llm calls doubled", "llm_calls_per_run", func(s *model.VersionStats) { s.LLMCallsPerRunP50 = 6 }, Regressed, "critical"},
		{"output halved", "output_tokens_per_run", func(s *model.VersionStats) { s.OutputTokensPerRunP50 = 150 }, Regressed, "info"},
		{"output doubled", "output_tokens_per_run", func(s *model.VersionStats) { s.OutputTokensPerRunP50 = 600 }, Regressed, "info"},
		{"output 1.9x", "output_tokens_per_run", func(s *model.VersionStats) { s.OutputTokensPerRunP50 = 570 }, Same, ""},
		{"cache under half", "cache_hit_ratio", func(s *model.VersionStats) { s.CacheRead = 240 }, Regressed, "warning"},
		{"cache at half", "cache_hit_ratio", func(s *model.VersionStats) { s.CacheRead = 250 }, Same, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := base()
			c.edit(&n)
			f := find(CompareVersions(base(), n, o), c.metric)
			if f == nil || f.Verdict != c.verdict || f.Severity != c.severity {
				t.Fatalf("got %+v, want %s %s", f, c.verdict, c.severity)
			}
		})
	}

	low := base()
	low.CacheRead = 100
	next := low
	next.CacheRead = 10
	if f := find(CompareVersions(low, next, o), "cache_hit_ratio"); f.Verdict != Same {
		t.Errorf("cache from 10%%: got %+v, want same", f)
	}

	small := base()
	small.Runs, small.CostPerRunP50 = 5, 1
	if f := find(CompareVersions(base(), small, o), "cost_per_run"); f != nil {
		t.Errorf("5 runs: got %+v, want no median comparison", f)
	}
}

func TestVerdict(t *testing.T) {
	cases := []struct {
		name     string
		findings []Finding
		want     string
	}{
		{"nothing", nil, "ok"},
		{"all same", []Finding{{Verdict: Same}}, "ok"},
		{"info regression only", []Finding{{Verdict: Regressed, Severity: "info"}}, "ok"},
		{"improved", []Finding{{Verdict: Improved, Severity: "warning"}, {Verdict: Regressed, Severity: "info"}}, Improved},
		{"regressed beats improved", []Finding{{Verdict: Improved, Severity: "warning"}, {Verdict: Regressed, Severity: "warning"}}, Regressed},
	}
	for _, c := range cases {
		if got := Verdict(c.findings); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}
