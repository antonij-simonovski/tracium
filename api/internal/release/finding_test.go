package release_test

import (
	"context"
	"testing"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/release"
	"github.com/tracium/api/testing/pgtest"
)

func TestStoreAddFinding(t *testing.T) {
	pool := pgtest.NewPool(t)
	s := release.NewStore(pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ('ws', 'u', 'ws', 'ws', 'prod', 'owner')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Ensure(ctx, release.Report{WorkspaceID: "ws", Workflow: "wf", Version: "v2", PrevVersion: "v1", FirstSeenAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, next := range []float64{0.6, 0.5} {
		if err := s.AddFinding(ctx, "ws", "wf", "v2", anomaly.Finding{Metric: "review_accuracy", Prev: 0.9, Next: next, Verdict: anomaly.Regressed, Severity: "critical"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddFinding(ctx, "ws", "wf", "v9", anomaly.Finding{Metric: "review_accuracy"}); err != nil {
		t.Errorf("version without a report: %v", err)
	}
	r, err := s.Get(ctx, []string{"ws"}, "wf", "v2")
	if err != nil || len(r.Findings) != 1 || r.Findings[0].Next != 0.5 || r.Status != release.StatusPending {
		t.Errorf("report = %+v, %v", r, err)
	}
}
