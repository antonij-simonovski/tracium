package query

import (
	"strings"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

const day = int64(86_400_000)

func TestSeriesPoints_ZeroFillsAndProjects(t *testing.T) {
	axis := []int64{0, day, 2 * day} // index 1 (day) is deliberately absent below
	byBucket := map[int64]dailyAgg{
		0:       {cost: 5, runs: 100, errRuns: 10},
		2 * day: {cost: 3, runs: 50, errRuns: 0},
		// day (index 1) absent → a genuine no-activity bucket, must read as zero.
	}

	cost := seriesPoints(axis, byBucket, "cost")
	if cost[0].Value != 5 || cost[1].Value != 0 || cost[2].Value != 3 {
		t.Errorf("cost values = %v, want [5 0 3]", []float64{cost[0].Value, cost[1].Value, cost[2].Value})
	}
	if cost[0].Support != 5 {
		t.Errorf("cost support = %v, want value 5", cost[0].Support)
	}

	er := seriesPoints(axis, byBucket, "error_rate")
	if er[0].Value != 0.1 { // 10/100
		t.Errorf("error_rate[0] value = %v, want 0.1", er[0].Value)
	}
	if er[1].Value != 0 || er[1].Support != 0 { // absent bucket: 0/0 → 0 rate, 0 runs support
		t.Errorf("error_rate[1] = %+v, want zero value and support", er[1])
	}
	if er[0].Support != 100 { // support is the run count, not the rate
		t.Errorf("error_rate[0] support = %v, want run count 100", er[0].Support)
	}

	runs := seriesPoints(axis, byBucket, "runs")
	if runs[0].Value != 100 || runs[2].Value != 50 {
		t.Errorf("runs values = %v, want [100 _ 50]", []float64{runs[0].Value, runs[1].Value, runs[2].Value})
	}
}

func TestSortAnomalies_SeverityThenScoreThenRecency(t *testing.T) {
	in := []model.Anomaly{
		{Severity: "info", Score: 3.2, BucketMs: 10},
		{Severity: "critical", Score: 6.5, BucketMs: 10},
		{Severity: "warning", Score: 5.0, BucketMs: 10},
		{Severity: "critical", Score: -9.0, BucketMs: 10}, // larger |score| than the other critical
		{Severity: "critical", Score: 6.5, BucketMs: 20},  // same as [1] but more recent
	}
	sortAnomalies(in)
	wantOrder := []struct {
		sev   string
		score float64
	}{
		{"critical", -9.0}, // |9| largest
		{"critical", 6.5},  // bucket 20 (more recent) before bucket 10
		{"critical", 6.5},
		{"warning", 5.0},
		{"info", 3.2},
	}
	for i, w := range wantOrder {
		if in[i].Severity != w.sev || in[i].Score != w.score {
			t.Errorf("position %d = {%s %v}, want {%s %v}", i, in[i].Severity, in[i].Score, w.sev, w.score)
		}
	}
}

func TestSeverityRank(t *testing.T) {
	if !(severityRank("critical") > severityRank("warning") &&
		severityRank("warning") > severityRank("info") &&
		severityRank("info") > severityRank("")) {
		t.Errorf("severity ranks not strictly ordered")
	}
}

func TestAnomalySummary(t *testing.T) {
	cases := []struct {
		a        model.Anomaly
		contains []string
	}{
		{
			model.Anomaly{Metric: "cost", Scope: "workflow", Workflow: "planner", Direction: "spike", Observed: 120, Expected: 10},
			[]string{`Workflow "planner"`, "cost", "$120.00", "12.0×", "$10.00"},
		},
		{
			model.Anomaly{Metric: "runs", Scope: "workspace", Direction: "drop", Observed: 0, Expected: 200},
			[]string{"Workspace", "run volume", "fell to 0", "typical 200"},
		},
		{
			model.Anomaly{Metric: "error_rate", Scope: "workflow", Workflow: "x", Direction: "spike", Observed: 0.14, Expected: 0.01},
			[]string{"error rate", "14.0%", "1.0%"},
		},
		{
			model.Anomaly{Metric: "cost", Scope: "workspace", Direction: "spike", Observed: 50, Expected: 0},
			[]string{"from near zero"},
		},
	}
	for _, tc := range cases {
		got := anomalySummary(tc.a)
		for _, want := range tc.contains {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q missing %q", got, want)
			}
		}
	}
}

func TestAttachVersionChanges(t *testing.T) {
	firsts := []model.VersionChange{
		{Workflow: "planner", Version: "v1", FirstSeenMs: 2 * day},
		{Workflow: "rewrite-message", Version: "v2", FirstSeenMs: 9 * day},
		{Workflow: "planner", Version: "v2", FirstSeenMs: 10 * day},
		{Workflow: "planner", Version: "v3", FirstSeenMs: 13 * day},
		{Workflow: "rewrite-message", Version: "v3", FirstSeenMs: 14 * day},
	}
	byWorkflow := map[string]map[int64]dailyAgg{
		"planner":         {10 * day: {cost: 9, runs: 10}, 13 * day: {runs: 3}, 14 * day: {runs: 50, errRuns: 1}},
		"rewrite-message": {10 * day: {cost: 1, runs: 40}, 14 * day: {runs: 10, errRuns: 5}},
		"steady":          {13 * day: {runs: 50}},
	}
	in := []model.Anomaly{
		{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 10 * day, Summary: "x."},
		{Metric: "cost", Scope: "workflow", Workflow: "rewrite-message", BucketMs: 10 * day},
		{Metric: "cost", Scope: "workspace", BucketMs: 10 * day},
		{Metric: "runs", Scope: "workspace", BucketMs: 10 * day},
		{Metric: "runs", Scope: "workspace", BucketMs: 11 * day},
		{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 5 * day},
		{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 12 * day},
		// steady carried the day; planner's v3 there is not the cause.
		{Metric: "runs", Scope: "workspace", BucketMs: 13 * day},
		// rewrite-message carried the errors though planner carried the runs.
		{Metric: "error_rate", Scope: "workspace", BucketMs: 14 * day},
	}
	attachVersionChanges(in, firsts, byWorkflow)

	want := []string{"planner/v2", "rewrite-message/v2", "planner/v2", "rewrite-message/v2", "", "", "", "", "rewrite-message/v3"}
	for i, a := range in {
		got := ""
		if a.VersionChange != nil {
			got = a.VersionChange.Workflow + "/" + a.VersionChange.Version
		}
		if got != want[i] {
			t.Errorf("anomaly %d version change = %q, want %q", i, got, want[i])
		}
	}
	if in[0].Summary != "x. Started near planner v2 (first seen 1970-01-11)." {
		t.Errorf("summary = %q", in[0].Summary)
	}
}

func TestUncoveredWorkspace(t *testing.T) {
	ws := []model.Anomaly{
		{Metric: "runs", Scope: "workspace", BucketMs: 1, Deviation: 100},
		{Metric: "latency_p95", Scope: "workspace", BucketMs: 1, Deviation: 1000},
		{Metric: "cost", Scope: "workspace", BucketMs: 1, Deviation: 10},
	}
	wf := []model.Anomaly{
		{Metric: "runs", Scope: "workflow", Workflow: "bad", BucketMs: 1, Deviation: 80},
		{Metric: "latency_p95", Scope: "workflow", Workflow: "bad", BucketMs: 2, Deviation: 1000},
		{Metric: "cost", Scope: "workflow", Workflow: "small", BucketMs: 1, Deviation: 3},
	}
	got := uncoveredWorkspace(ws, wf)
	if len(got) != 2 || got[0].Metric != "latency_p95" || got[1].Metric != "cost" {
		t.Errorf("uncoveredWorkspace = %+v, want latency_p95 (other day) and cost (workflow carries 30%%)", got)
	}
}

func TestNewDetectorSummaries(t *testing.T) {
	a := model.Anomaly{Metric: "missing_step", Workflow: "rag", BucketMs: 0, Observed: 0.08, Expected: 0.99}
	if got := missingStepSummary(a, "retrieve", 2); got != `Workflow "rag": step "retrieve" ran in 8% of runs in the hour from 1970-01-01 00:00 UTC; it normally runs in 99%. 2 more steps dropped too.` {
		t.Errorf("missing_step summary = %q", got)
	}
	r := model.Anomaly{Metric: "ingest_rejected", Observed: 25, Expected: 0}
	if got := rejectedSummary(r, "invalid_timestamp"); !strings.Contains(got, "rejected 25 spans") || !strings.Contains(got, "timestamps are invalid") {
		t.Errorf("ingest_rejected summary = %q", got)
	}
}

func TestModelFamily(t *testing.T) {
	same := [][2]string{
		{"gpt-4o", "gpt-4o-2024-08-06"},
		{"claude-3-5-sonnet-latest", "claude-3-5-sonnet-20241022"},
		{"gpt-4", "gpt-4-0613"},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "anthropic.claude-3-5-sonnet"},
		{"openai/gpt-4o-mini", "gpt-4o-mini-2024-07-18"},
	}
	for _, p := range same {
		if modelFamily(p[0]) != modelFamily(p[1]) {
			t.Errorf("%s and %s should be one family: %q vs %q", p[0], p[1], modelFamily(p[0]), modelFamily(p[1]))
		}
	}
	for _, p := range [][2]string{{"gpt-4o", "gpt-4o-mini"}, {"gpt-4", "gpt-4o"}, {"claude-sonnet-4-5", "claude-haiku-4-5"}} {
		if modelFamily(p[0]) == modelFamily(p[1]) {
			t.Errorf("%s and %s should differ", p[0], p[1])
		}
	}
}

func TestAttachModelChanges(t *testing.T) {
	const hour = 3600_000
	changes := []modelChange{
		{workflow: "rag", ModelChange: model.ModelChange{Model: "gpt-4o-2024-11-20", FirstSeenMs: 10 * hour}},
	}
	got := []model.Anomaly{
		{Metric: "duplicate_output", Scope: "workflow", Workflow: "rag", BucketMs: 12 * hour},
		{Metric: "duplicate_output", Scope: "workflow", Workflow: "other", BucketMs: 12 * hour},
		{Metric: "duplicate_output", Scope: "workflow", Workflow: "rag", BucketMs: 40 * hour},
		{Metric: "duplicate_output", Scope: "workflow", Workflow: "rag", BucketMs: 9 * hour},
		{Metric: "runs", Scope: "workspace", BucketMs: 12 * hour},
	}
	attachModelChanges(got, changes)
	if got[0].ModelChange == nil || !strings.Contains(got[0].Summary, "new response model gpt-4o-2024-11-20") {
		t.Errorf("change 2h before = %+v", got[0])
	}
	for _, i := range []int{1, 2, 3, 4} {
		if got[i].ModelChange != nil {
			t.Errorf("anomaly %d got a model change: %+v", i, got[i])
		}
	}
}

func TestModelAndOutputSummaries(t *testing.T) {
	a := model.Anomaly{Metric: "model_mix", Workflow: "rag", Observed: 0.43, Expected: 0}
	if got := modelMixSummary(a, "gpt-4o-mini"); got != `Workflow "rag": 43.0% of LLM calls used gpt-4o-mini in the hour from 1970-01-01 00:00 UTC; it was 0.0% over the prior 24h.` {
		t.Errorf("model_mix summary = %q", got)
	}
	c := model.Anomaly{Metric: "context_pressure", Workflow: "rag", Observed: 0.91, Expected: 0.62}
	if got := contextTrendSummary(c); !strings.Contains(got, "p95 LLM call used 91.0%") || !strings.Contains(got, "62.0% a week earlier") {
		t.Errorf("context trend summary = %q", got)
	}
	d := model.Anomaly{Metric: "duplicate_output", Workflow: "rag", Observed: 0.45, Expected: 0.02}
	if got := hourlySummary(d, "t1", ""); !strings.Contains(got, "45.0% of LLM calls returned the same output for different inputs") {
		t.Errorf("duplicate_output summary = %q", got)
	}
}

func TestDetectSeriesSkipsDropsOnTheDayInProgress(t *testing.T) {
	axis := make([]int64, 30)
	by := map[int64]dailyAgg{}
	for i := range axis {
		axis[i] = int64(i) * day
		by[axis[i]] = dailyAgg{runs: 1000}
	}
	by[axis[29]] = dailyAgg{runs: 400}
	f := MetricsFilter{AnomalyMetric: "runs", End: time.UnixMilli(axis[29] + day/2)}
	if got := detectSeries(f, "workspace", "", axis, by, axis[29], 0); len(got) != 0 {
		t.Fatalf("half a day of runs is no drop: %+v", got)
	}
	f.End = time.UnixMilli(axis[29] + day)
	if got := detectSeries(f, "workspace", "", axis, by, axis[29], 0); len(got) != 1 || got[0].Direction != "drop" {
		t.Fatalf("a finished day still drops: %+v", got)
	}
}
