package query

import (
	"strings"
	"testing"
	"time"
)

// Trace reconstruction reads the tracium.calls view (per-call spans only), so
// metric rows — which carry an empty trace_id and would collapse into one phantom
// trace under GROUP BY trace_id — are excluded structurally, with no source
// predicate in the query itself.
func TestTraceQueriesReadCallsView(t *testing.T) {
	for _, sel := range []struct {
		name string
		sql  string
	}{
		{"traceSelect", traceSelect},
		{"spanSelect", spanSelect},
	} {
		if !strings.Contains(sel.sql, "tracium.calls") {
			t.Errorf("%s does not read tracium.calls:\n%s", sel.name, sel.sql)
		}
		if strings.Contains(sel.sql, "tracium.spans") {
			t.Errorf("%s still reads the raw tracium.spans union:\n%s", sel.name, sel.sql)
		}
	}
}

// The trace-list and trace-detail clauses no longer carry a source predicate —
// the calls view supplies the exclusion — so their bound args are only the time
// bounds / trace id and the business + access filters, in that order.
func TestTraceFilterSQLHasNoSourcePredicate(t *testing.T) {
	f := TraceFilter{StartAfter: time.UnixMilli(100), StartBefore: time.UnixMilli(200)}
	clause, args := traceFilterSQL(f)

	if strings.Contains(clause, "source") {
		t.Errorf("clause should not mention source: %q", clause)
	}
	// Just the two time bounds, no source arg between them and the user.
	if len(args) != 2 || args[0] != int64(100) || args[1] != int64(200) {
		t.Errorf("args = %v, want [100 200]", args)
	}

	// A user filter binds immediately after the time bounds.
	f.UserID = "acme"
	_, args = traceFilterSQL(f)
	if len(args) != 3 || args[2] != "acme" {
		t.Errorf("user args = %v, want [100 200 acme]", args)
	}
}

func TestTraceFilterSQLVersion(t *testing.T) {
	f := TraceFilter{StartAfter: time.UnixMilli(100), Workflow: "planner", Version: "v12"}
	clause, args := traceFilterSQL(f)

	if !strings.Contains(clause, "HAVING "+workflowExpr+" = ? AND "+versionExpr+" = ?") {
		t.Errorf("clause = %q, want workflow and version in HAVING", clause)
	}
	if n := len(args); n < 2 || args[n-2] != "planner" || args[n-1] != "v12" {
		t.Errorf("args = %v, want planner then v12 last", args)
	}
}

func TestTraceDetailScopeHasNoSourcePredicate(t *testing.T) {
	clause, _ := traceDetailScope("trace", []string{"workspace"})
	if strings.Contains(clause, "source") {
		t.Errorf("traceDetailScope should not mention source: %q", clause)
	}
}

func TestTraceDetailsRequireWorkspaceScope(t *testing.T) {
	for _, tc := range []struct {
		ids       []string
		predicate string
		count     int
	}{
		{nil, "AND 1 = 0", 1},
		{[]string{"workspace-a"}, "AND workspace_id = ?", 2},
		{[]string{"workspace-a", "workspace-b"}, "AND workspace_id IN (?,?)", 3},
	} {
		clause, args := traceDetailScope("known-trace", tc.ids)
		if !strings.Contains(clause, tc.predicate) || len(args) != tc.count {
			t.Fatalf("unsafe scope: %s %v", clause, args)
		}
		// Workspace ids bind after the single trace-id arg (no source arg anymore).
		for i, id := range tc.ids {
			if args[i+1] != id {
				t.Fatalf("scope argument mismatch: %v", args)
			}
		}
	}
}
