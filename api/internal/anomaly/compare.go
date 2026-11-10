package anomaly

import (
	"math"

	"github.com/tracium/api/internal/model"
)

// Finding compares one metric between a workflow's previous and new version.
type Finding struct {
	Metric   string  `json:"metric"`
	Prev     float64 `json:"prev"`
	Next     float64 `json:"next"`
	Verdict  string  `json:"verdict"` // regressed | improved | same
	Severity string  `json:"severity"`
}

// CompareOptions holds every threshold of CompareVersions.
type CompareOptions struct {
	// Rates regress when z >= RateZ, the difference is at least RateDiff and
	// the new version has at least RateMinHits hits; critical from RateCritical.
	RateZ, RateDiff, RateCritical float64
	RateMinHits                   int64
	// Medians and p95 regress at RatioWarning times the previous value and
	// above the metric's floor, critical at RatioCritical.
	RatioWarning, RatioCritical float64
	LatencyFloorMs              float64
	CostFloor                   float64
	LLMCallsFloor               float64
	// MinRuns is the fewest runs on either side for a median to be compared.
	MinRuns int64
	// OutputTokensRatio flags output tokens per run at this multiple or its inverse.
	OutputTokensRatio float64
	// Cache regresses when the previous ratio was at least CacheMinRatio and
	// the new one is below CacheDrop of it.
	CacheMinRatio, CacheDrop float64
}

func DefaultCompareOptions() CompareOptions {
	return CompareOptions{RateZ: 3, RateDiff: 0.05, RateCritical: 0.20, RateMinHits: 5,
		RatioWarning: 1.3, RatioCritical: 2.0, LatencyFloorMs: 500, CostFloor: 0.01, LLMCallsFloor: 2, MinRuns: 10,
		OutputTokensRatio: 2.0, CacheMinRatio: 0.2, CacheDrop: 0.5}
}

// FindingMetrics are the metrics CompareVersions reports on.
var FindingMetrics = map[string]bool{"error_rate": true, "truncation_rate": true, "refusal_rate": true, "tool_error_rate": true,
	"cost_per_run": true, "latency_p95": true, "llm_calls_per_run": true, "output_tokens_per_run": true, "cache_hit_ratio": true}

const (
	Regressed = "regressed"
	Improved  = "improved"
	Same      = "same"
)

// CompareVersions compares a new version's runs with the previous version's.
// Metrics either side has no data for are left out.
func CompareVersions(prev, next model.VersionStats, o CompareOptions) []Finding {
	var out []Finding
	rates := []struct {
		metric    string
		pHits, pN int64
		nHits, nN int64
	}{
		{"error_rate", prev.ErrorRuns, prev.Runs, next.ErrorRuns, next.Runs},
		{"truncation_rate", prev.TruncatedCalls, prev.LLMCalls, next.TruncatedCalls, next.LLMCalls},
		{"refusal_rate", prev.RefusedCalls, prev.LLMCalls, next.RefusedCalls, next.LLMCalls},
		{"tool_error_rate", prev.ToolErrors, prev.ToolCalls, next.ToolErrors, next.ToolCalls},
	}
	for _, r := range rates {
		if r.pN > 0 && r.nN > 0 {
			out = append(out, compareRate(r.metric, r.pHits, r.pN, r.nHits, r.nN, o))
		}
	}
	if prev.Runs >= o.MinRuns && next.Runs >= o.MinRuns {
		out = append(out,
			compareRatio("cost_per_run", prev.CostPerRunP50, next.CostPerRunP50, o.CostFloor, o),
			compareRatio("latency_p95", prev.LatencyP95, next.LatencyP95, o.LatencyFloorMs, o),
			compareRatio("llm_calls_per_run", prev.LLMCallsPerRunP50, next.LLMCallsPerRunP50, o.LLMCallsFloor, o),
			compareOutputTokens(prev.OutputTokensPerRunP50, next.OutputTokensPerRunP50, o))
	}
	if prev.InputTokens > 0 && next.InputTokens > 0 {
		p, n := prev.CacheRead/prev.InputTokens, next.CacheRead/next.InputTokens
		f := Finding{Metric: "cache_hit_ratio", Prev: p, Next: n, Verdict: Same}
		switch {
		case p >= o.CacheMinRatio && n < p*o.CacheDrop:
			f.Verdict, f.Severity = Regressed, "warning"
		case n >= o.CacheMinRatio && p < n*o.CacheDrop:
			f.Verdict, f.Severity = Improved, "warning"
		}
		out = append(out, f)
	}
	return out
}

func compareRate(metric string, pHits, pN, nHits, nN int64, o CompareOptions) Finding {
	p, n := float64(pHits)/float64(pN), float64(nHits)/float64(nN)
	f := Finding{Metric: metric, Prev: p, Next: n, Verdict: Same}
	z, diff := TwoProportionZ(pHits, pN, nHits, nN), n-p
	if math.IsNaN(z) {
		return f
	}
	switch {
	case z >= o.RateZ && diff >= o.RateDiff && nHits >= o.RateMinHits:
		f.Verdict = Regressed
	case z <= -o.RateZ && -diff >= o.RateDiff && pHits >= o.RateMinHits:
		f.Verdict = Improved
	default:
		return f
	}
	f.Severity = "warning"
	if math.Abs(diff) >= o.RateCritical {
		f.Severity = "critical"
	}
	return f
}

func compareRatio(metric string, p, n, floor float64, o CompareOptions) Finding {
	f := Finding{Metric: metric, Prev: p, Next: n, Verdict: Same}
	hi, lo := n, p
	if n < p {
		hi, lo = p, n
	}
	if hi-lo < floor || hi < lo*o.RatioWarning {
		return f
	}
	f.Verdict, f.Severity = Regressed, "warning"
	if n < p {
		f.Verdict = Improved
	}
	if hi >= lo*o.RatioCritical {
		f.Severity = "critical"
	}
	return f
}

// compareOutputTokens flags both directions: a halved answer is as suspicious as a doubled one.
func compareOutputTokens(p, n float64, o CompareOptions) Finding {
	f := Finding{Metric: "output_tokens_per_run", Prev: p, Next: n, Verdict: Same}
	if p > 0 && (n >= p*o.OutputTokensRatio || n <= p/o.OutputTokensRatio) {
		f.Verdict, f.Severity = Regressed, "info"
	}
	return f
}

// Verdict is regressed when any finding regressed at warning or above, else
// improved when any improved, else ok.
func Verdict(findings []Finding) string {
	verdict := "ok"
	for _, f := range findings {
		if f.Verdict == Regressed && f.Severity != "info" {
			return Regressed
		}
		if f.Verdict == Improved {
			verdict = Improved
		}
	}
	return verdict
}

// TwoProportionZ is the pooled z statistic of the new rate against the
// previous one; NaN when the pooled rate is 0 or 1.
func TwoProportionZ(pHits, pN, nHits, nN int64) float64 {
	p, n := float64(pHits)/float64(pN), float64(nHits)/float64(nN)
	pooled := float64(pHits+nHits) / float64(pN+nN)
	se := math.Sqrt(pooled * (1 - pooled) * (1/float64(pN) + 1/float64(nN)))
	if se == 0 {
		return math.NaN()
	}
	return (n - p) / se
}
