package release_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/release"
	"github.com/tracium/api/testing/pgtest"
)

func TestStore(t *testing.T) {
	pool := pgtest.NewPool(t)
	s := release.NewStore(pool)
	ctx := context.Background()
	for _, ws := range []string{"ws-1", "ws-2"} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ($1, 'u', $1, $1, 'prod', 'owner')`, ws); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC().Truncate(time.Second)
	for _, r := range []release.Report{
		{WorkspaceID: "ws-1", Workflow: "wf", Version: "v2", PrevVersion: "v1", FirstSeenAt: at},
		{WorkspaceID: "ws-1", Workflow: "wf", Version: "v2", PrevVersion: "other", FirstSeenAt: at},
		{WorkspaceID: "ws-1", Workflow: "wf", Version: "v3", PrevVersion: "v2", FirstSeenAt: at},
		{WorkspaceID: "ws-2", Workflow: "wf", Version: "v2", PrevVersion: "v1", FirstSeenAt: at},
	} {
		if err := s.Ensure(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Get(ctx, []string{"ws-1"}, "wf", "v2")
	if err != nil || r.PrevVersion != "v1" || r.Status != release.StatusPending {
		t.Fatalf("get = %+v, %v", r, err)
	}

	r.Status, r.Verdict, r.Runs, r.FinalizedAt = release.StatusFinal, anomaly.Regressed, 38, &at
	r.Findings = []anomaly.Finding{{Metric: "truncation_rate", Prev: 0.01, Next: 0.25, Verdict: anomaly.Regressed, Severity: "critical"}}
	if err := s.Save(ctx, *r); err != nil {
		t.Fatal(err)
	}
	flipped := *r
	flipped.Verdict, flipped.Findings = "ok", nil
	if err := s.Save(ctx, flipped); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, []string{"ws-1"}, "wf", "v2"); got.Verdict != anomaly.Regressed || len(got.Findings) != 1 || got.Findings[0].Next != 0.25 {
		t.Errorf("regressed report overwritten: %+v", got)
	}

	active, err := s.Active(ctx, "ws-1", at.Add(-time.Hour))
	if err != nil || len(active) != 2 {
		t.Errorf("active = %+v, %v; want the regression and the pending v3", active, err)
	}
	if stale, _ := s.Active(ctx, "ws-1", at.Add(time.Hour)); len(stale) != 1 || stale[0].Version != "v3" {
		t.Errorf("active after the regression went stale = %+v", stale)
	}

	verdicts, err := s.Verdicts(ctx, []string{"ws-1"}, "wf")
	if err != nil || verdicts["v2"] != "regressed" || verdicts["v3"] != "pending" || len(verdicts) != 2 {
		t.Errorf("verdicts = %v, %v", verdicts, err)
	}
	if _, err := s.Get(ctx, []string{"ws-2"}, "wf", "v3"); !errors.Is(err, release.ErrNotFound) {
		t.Errorf("cross-workspace get: %v", err)
	}
	if list, _ := s.List(ctx, []string{"ws-2"}, "", 10); len(list) != 1 || list[0].WorkspaceID != "ws-2" {
		t.Errorf("ws-2 list = %+v", list)
	}
	if list, _ := s.List(ctx, []string{"ws-1"}, release.StatusFinal, 10); len(list) != 1 || list[0].Version != "v2" {
		t.Errorf("final list = %+v", list)
	}
	if list, _ := s.List(ctx, nil, "", 10); len(list) != 0 {
		t.Errorf("empty scope listed %+v", list)
	}
}
