package query

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestPickNewVersions(t *testing.T) {
	end := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	since := end.Add(-48 * time.Hour)
	h := func(workflow, version string, at time.Time, runs int64) versionHour {
		return versionHour{workflow: workflow, version: version, hourMs: at.Truncate(time.Hour).UnixMilli(), firstMs: at.UnixMilli(), runs: runs}
	}
	shipped := end.Add(-3 * time.Hour)
	hours := []versionHour{
		h("agent", "v1", shipped.Add(-20*time.Hour), 40),
		h("agent", "v0", shipped.Add(-10*time.Hour), 5),
		h("agent", "v2", shipped, 31),
		h("brand-new", "v1", shipped, 50),
		h("old", "v1", shipped.Add(-5*time.Hour), 10),
		h("old", "v7", shipped, 10),
		h("unversioned", "", shipped, 10),
	}
	for i := range 6 {
		hours = append(hours, h("per-run", "v1", shipped.Add(-30*time.Hour), 1), h("per-run", fmt.Sprintf("r%d", i), shipped.Add(time.Duration(i)*time.Minute), 1))
	}
	seen := map[[2]string]bool{{"old", "v7"}: true, {"agent", "v0"}: true}

	got := pickNewVersions(hours, seen, since, end)
	want := []model.NewVersion{{Workflow: "agent", Version: "v2", PrevVersion: "v1", FirstSeenMs: shipped.UnixMilli(), Runs: 31}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
