package query

import (
	"strings"
	"testing"

	"github.com/tracium/api/internal/model"
)

func TestTraceScope(t *testing.T) {
	cols, where, args := traceScope(MetricsFilter{})
	if cols != "" || where != "" || args != nil {
		t.Errorf("unfiltered scope = (%q, %q, %v), want empty", cols, where, args)
	}

	cols, where, args = traceScope(MetricsFilter{Workflow: "planner", Version: "v12"})
	if !strings.Contains(cols, workflowExpr+" AS name") || !strings.Contains(cols, versionExpr+" AS version") {
		t.Errorf("cols = %q, want the workflow and version expressions", cols)
	}
	if where != "\nWHERE name = ? AND version = ?" {
		t.Errorf("where = %q, want both filters", where)
	}
	if len(args) != 2 || args[0] != "planner" || args[1] != "v12" {
		t.Errorf("args = %v, want [planner v12]", args)
	}
}

func TestVersionExprsPickTheSameSpan(t *testing.T) {
	if got := strings.ReplaceAll(versionSourceExpr, "version_source", "workflow_version"); got != versionExpr {
		t.Errorf("versionSourceExpr = %q, want versionExpr over version_source", versionSourceExpr)
	}
}

func TestCompareVersions(t *testing.T) {
	p95Base, p95Candidate := 1000.0, 800.0
	base := model.WorkflowVersion{Version: "v11", CostPerRun: 0.05, ErrorRate: 0.1, TokensPerRun: 1000, P95LatencyMs: &p95Base}
	candidate := model.WorkflowVersion{Version: "v12", CostPerRun: 0.07, ErrorRate: 0.1, TokensPerRun: 500, P95LatencyMs: &p95Candidate}

	got := compareVersions(base, candidate)

	if got.Base.Version != "v11" || got.Candidate.Version != "v12" {
		t.Errorf("versions = %s vs %s, want v11 vs v12", got.Base.Version, got.Candidate.Version)
	}
	for name, tc := range map[string]struct {
		kpi       model.KPI
		value     float64
		deltaType string
	}{
		"cost per run":   {got.CostPerRun, 0.07, "bad"},
		"p95 latency":    {got.P95LatencyMs, 800, "good"},
		"error rate":     {got.ErrorRate, 0.1, "neutral"},
		"tokens per run": {got.TokensPerRun, 500, "good"},
	} {
		if tc.kpi.Value != tc.value || tc.kpi.DeltaType != tc.deltaType {
			t.Errorf("%s = %+v, want value %v / %s", name, tc.kpi, tc.value, tc.deltaType)
		}
	}
}

func TestCompareVersionsWithoutLatency(t *testing.T) {
	got := compareVersions(model.WorkflowVersion{Version: "v11"}, model.WorkflowVersion{Version: "v12"})
	if got.P95LatencyMs != (model.KPI{DeltaType: "neutral"}) {
		t.Errorf("p95 = %+v, want a neutral zero", got.P95LatencyMs)
	}
}

func TestVersionIn(t *testing.T) {
	if clause, args := versionIn("version", nil); clause != "" || args != nil {
		t.Errorf("no versions = (%q, %v), want empty", clause, args)
	}
	clause, args := versionIn("workflow_version", []string{"v11", "v12"})
	if clause != " AND workflow_version IN (?,?)" || len(args) != 2 || args[1] != "v12" {
		t.Errorf("versionIn = (%q, %v), want two placeholders", clause, args)
	}
}
