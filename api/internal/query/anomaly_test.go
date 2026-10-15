package query

import (
	"strings"
	"testing"

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
	}
	byWorkflow := map[string]map[int64]dailyAgg{
		"planner":         {10 * day: {cost: 9, runs: 10}},
		"rewrite-message": {10 * day: {cost: 1, runs: 40}},
	}
	in := []model.Anomaly{
		{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 10 * day, Summary: "x."},
		{Metric: "cost", Scope: "workflow", Workflow: "rewrite-message", BucketMs: 10 * day},
		{Metric: "cost", Scope: "workspace", BucketMs: 10 * day},
		{Metric: "runs", Scope: "workspace", BucketMs: 10 * day},
		{Metric: "runs", Scope: "workspace", BucketMs: 11 * day},
		{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 5 * day},
		{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 12 * day},
	}
	attachVersionChanges(in, firsts, byWorkflow)

	want := []string{"planner/v2", "rewrite-message/v2", "planner/v2", "rewrite-message/v2", "", "", ""}
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
