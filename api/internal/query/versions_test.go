package query

import (
	"strings"
	"testing"
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
