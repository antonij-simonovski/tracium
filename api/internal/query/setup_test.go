package query

import (
	"strings"
	"testing"
)

func TestSpanIssuesSQLCoversOnlySpanLevelChecks(t *testing.T) {
	for _, c := range setupChecks {
		want := c.cond != "" && !c.everySpan
		if got := strings.Contains(spanIssuesSQL, "'"+c.Code+"'"); got != want {
			t.Errorf("%s in spanIssuesSQL = %v, want %v", c.Code, got, want)
		}
		if setupIssueByCode[c.Code] != c.SetupIssue {
			t.Errorf("setupIssueByCode[%q] does not resolve to its check", c.Code)
		}
		if (c.cond == "") == (c.agg == "") {
			t.Errorf("%s must define exactly one of cond and agg", c.Code)
		}
	}
}

func TestVersionPerRunSQLComparesVersionsWithinAWorkflow(t *testing.T) {
	for _, want := range []string{workflowExpr + " AS name", versionExpr + " AS version", "WHERE version != ''", "GROUP BY workspace_id, name", "HAVING count() >= 20"} {
		if !strings.Contains(versionPerRunSQL, want) {
			t.Errorf("versionPerRunSQL lacks %q", want)
		}
	}
}
