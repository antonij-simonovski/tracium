package review

import (
	"context"
	"testing"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/pgtest"
)

func TestStoreReviewedAndRunAccuracy(t *testing.T) {
	pool := pgtest.NewPool(t)
	s := NewStore(pool)
	ctx := context.Background()
	for _, ws := range []string{"ws-1", "ws-2"} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ($1, 'u', $1, $1, 'prod', 'owner')`, ws); err != nil {
			t.Fatal(err)
		}
	}
	save := func(ws, trace, span, version, verdict, reason string) {
		t.Helper()
		r := &model.Review{TraceID: trace, SpanID: span, WorkspaceID: ws, WorkflowName: "wf", WorkflowVersion: version, Verdict: verdict,
			ReviewedBy: "u", Source: model.ReviewSourceUser, Reason: reason}
		if err := s.Upsert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	save("ws-1", "a", "", "v1", "correct", "")
	save("ws-1", "b", "", "v1", "incorrect", "truncated")
	save("ws-1", "c", "", "v2", "incorrect", "sample")
	save("ws-1", "s", "span-1", "v2", "incorrect", "")
	save("ws-2", "d", "", "v2", "correct", "")
	// Re-saving from outside the queue keeps the queue's reason.
	save("ws-1", "b", "", "v1", "correct", "")

	got, err := s.Reviewed(ctx, []string{"ws-1"}, []string{"a", "c", "s", "d", "x"})
	if err != nil || len(got) != 2 || !got["a"] || !got["c"] {
		t.Errorf("reviewed = %v, %v; span reviews and other workspaces do not count", got, err)
	}
	if rev, _ := s.Get(ctx, "b", "", []string{"ws-1"}); rev.Reason != "truncated" {
		t.Errorf("reason = %q", rev.Reason)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO release_reports (workspace_id, workflow, version, prev_version, first_seen_at, status)
		VALUES ('ws-1', 'wf', 'v2', 'v1', '2026-11-01T00:00:00Z', 'pending')`); err != nil {
		t.Fatal(err)
	}
	acc, err := s.RunAccuracy(ctx, "ws-1")
	if err != nil || len(acc) != 2 {
		t.Fatalf("run accuracy = %+v, %v", acc, err)
	}
	v1, v2 := acc[0], acc[1]
	if v1.Version != "v1" || v1.Reviews != 2 || v1.Correct != 2 || v1.Unflagged != 1 || v1.UnflaggedCorrect != 1 || v1.ShippedAt != nil {
		t.Errorf("v1 = %+v", v1)
	}
	if v2.Version != "v2" || v2.Reviews != 1 || v2.Correct != 0 || v2.Unflagged != 1 || v2.FirstAt.IsZero() || v2.ShippedAt == nil || v2.ShippedAt.Day() != 1 {
		t.Errorf("v2 = %+v", v2)
	}
}
