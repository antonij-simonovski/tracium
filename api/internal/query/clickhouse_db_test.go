package query

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

// newTestRepository opens the migrated ClickHouse in TEST_CLICKHOUSE_DSN and
// returns it with a workspace id of its own. It skips the test when the DSN is unset.
func newTestRepository(t *testing.T) (*ClickHouseRepository, string) {
	t.Helper()
	dsn := os.Getenv("TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TEST_CLICKHOUSE_DSN not set")
	}
	r, err := NewClickHouseRepository(dsn, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.db.Close() })
	return r, fmt.Sprintf("test-%d", time.Now().UnixNano())
}

type testSpan struct {
	workspace, user, trace, workflow, version string
	startMs                                   int64
}

func insertSpans(t *testing.T, r *ClickHouseRepository, spans []testSpan) {
	t.Helper()
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO tracium.spans (trace_id, span_id, name, start_time_ms, end_time_ms,
    user_id, workspace_id, workflow_name, workflow_version)`)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range spans {
		if _, err := stmt.Exec(s.trace, fmt.Sprint(i), s.workflow, s.startMs, s.startMs+10,
			s.user, s.workspace, s.workflow, s.version); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestVersionChangesFromClickHouse(t *testing.T) {
	r, ws := newTestRepository(t)
	today := time.Now().UTC().Truncate(24 * time.Hour).UnixMilli()
	run := func(user, workflow, version string, daysAgo int64) testSpan {
		return testSpan{ws, user, fmt.Sprintf("%s-%s-%s-%d", user, workflow, version, daysAgo), workflow, version, today - daysAgo*day + 1000}
	}
	insertSpans(t, r, []testSpan{
		run("c1", "planner", "v1", 10),
		run("c1", "planner", "v1", 5),
		run("c1", "planner", "v2", 0),
		// c2 first runs the long-released v1 today.
		run("c2", "planner", "v1", 0),
		// Starting to send versions is not a change.
		run("c1", "rewrite-message", "v1", 1),
	})

	f := MetricsFilter{UserID: "c2", WorkspaceIDs: []string{ws}, End: time.UnixMilli(today)}
	got, err := r.versionChanges(context.Background(), f, time.UnixMilli(today-30*day), today-day)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.VersionChange{{Workflow: "planner", Version: "v2", FirstSeenMs: today}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("version changes = %+v, want %+v", got, want)
	}
}

func TestVersionPerRunCheckFromClickHouse(t *testing.T) {
	r, perRun := newTestRepository(t)
	a, b := perRun+"-a", perRun+"-b"
	now := time.Now().UnixMilli()
	var spans []testSpan
	for i := 0; i < versionPerRunMinRuns; i++ {
		spans = append(spans, testSpan{perRun, "", fmt.Sprintf("%s-%d", perRun, i), "planner", fmt.Sprintf("run-%d", i), now - 1000})
	}
	// Each workspace alone is under the run floor; counted together they would be flagged.
	for _, ws := range []string{a, b} {
		for i := 0; i < versionPerRunMinRuns/2+1; i++ {
			spans = append(spans, testSpan{ws, "", fmt.Sprintf("%s-%d", ws, i), "planner", fmt.Sprintf("%s-v%d", ws, i%6), now - 1000})
		}
	}
	insertSpans(t, r, spans)

	for _, tc := range []struct {
		workspaces []string
		flagged    bool
	}{{[]string{perRun}, true}, {[]string{a, b}, false}} {
		clause, args := window("", tc.workspaces, now-time.Hour.Milliseconds(), now)
		c, err := r.versionPerRunCheck(context.Background(), clause, args)
		if err != nil {
			t.Fatal(err)
		}
		if (c.Spans > 0) != tc.flagged {
			t.Errorf("workspaces %v: spans = %d, want flagged %v", tc.workspaces, c.Spans, tc.flagged)
		}
	}
}
