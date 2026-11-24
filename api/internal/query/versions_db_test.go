package query

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewVersionsAndRunStatsFromClickHouse(t *testing.T) {
	r, ws := newTestRepository(t)
	now := time.Now().UTC()
	shipped := now.Add(-3 * time.Hour)
	var spans []testSpan
	add := func(workflow, version string, at time.Time, n int) {
		for i := range n {
			spans = append(spans, testSpan{workspace: ws, trace: fmt.Sprintf("%s-%s-%d", workflow, version, i), workflow: workflow, version: version,
				startMs: at.UnixMilli() + int64(i)})
		}
	}
	add("agent", "v1", shipped.Add(-20*time.Hour), 40)
	add("agent", "v2", shipped, 31)
	add("old", "v1", now.Add(-20*24*time.Hour), 5)
	add("old", "v0", shipped.Add(-2*time.Hour), 5)
	add("old", "v1", shipped, 5)
	insertSpans(t, r, spans)

	f := MetricsFilter{WorkspaceIDs: []string{ws}, End: now}
	got, err := r.NewVersions(context.Background(), f, now.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Workflow != "agent" || got[0].Version != "v2" || got[0].PrevVersion != "v1" || got[0].Runs != 31 {
		t.Fatalf("new versions = %+v", got)
	}
	if other, _ := r.NewVersions(context.Background(), MetricsFilter{WorkspaceIDs: []string{ws + "-other"}, End: now}, now.Add(-48*time.Hour)); len(other) != 0 {
		t.Errorf("another workspace saw %+v", other)
	}

	stats, err := r.VersionRunStats(context.Background(), f, "agent", "v2", shipped, now)
	if err != nil || stats.Runs != 31 || stats.LatencyP95 != 10 {
		t.Errorf("stats = %+v, %v", stats, err)
	}
}
