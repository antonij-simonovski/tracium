package incident_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/pgtest"
)

func TestStore(t *testing.T) {
	pool := pgtest.NewPool(t)
	s := incident.NewStore(pool)
	ctx := context.Background()
	for _, ws := range []string{"ws-1", "ws-2"} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ($1, 'u', $1, $1, 'prod', 'owner')`, ws); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC().Truncate(time.Second)
	newIncident := func(ws, subject string) *incident.Incident {
		return &incident.Incident{WorkspaceID: ws, Scope: "workflow", Subject: subject, Status: incident.StatusOpen, Severity: "warning",
			Title: subject, OpenedAt: at, UpdatedAt: at, LastSignalAt: at, VersionChange: &model.VersionChange{Workflow: subject, Version: "v2"},
			Signals: []incident.Signal{{Metric: "truncation_rate", BucketMs: 1, Severity: "warning", TraceIDs: []string{"t1"}}}}
	}

	var wg sync.WaitGroup
	created := make([]bool, 8)
	ids := make([]string, 8)
	for i := range created {
		wg.Go(func() {
			inc := newIncident("ws-1", "wf")
			var err error
			if created[i], err = s.Create(ctx, inc); err != nil {
				t.Error(err)
			}
			ids[i] = inc.ID
		})
	}
	wg.Wait()
	n := 0
	for i, c := range created {
		if c {
			n++
		}
		if ids[i] != ids[0] {
			t.Fatalf("concurrent creates returned different incidents: %v", ids)
		}
	}
	if n != 1 {
		t.Fatalf("%d creates won, want 1", n)
	}

	inc, err := s.Get(ctx, []string{"ws-1"}, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(inc.Signals) != 1 || inc.VersionChange == nil || inc.VersionChange.Version != "v2" {
		t.Fatalf("unexpected incident %+v", inc)
	}
	inc.Severity = "critical"
	if err := s.Update(ctx, *inc, []incident.Signal{
		{Metric: "truncation_rate", BucketMs: 1, Severity: "critical", Observed: 0.9},
		{Metric: "refusal_rate", BucketMs: 1, Severity: "warning"},
	}); err != nil {
		t.Fatal(err)
	}
	inc, _ = s.Get(ctx, []string{"ws-1"}, ids[0])
	if len(inc.Signals) != 2 || inc.Severity != "critical" || inc.Signals[1].Observed != 0.9 || inc.Signals[1].TraceIDs[0] != "t1" {
		t.Fatalf("signal upsert: %+v", inc.Signals)
	}
	if len(inc.Signals[0].TraceIDs) != 0 || inc.Signals[0].TraceIDs == nil {
		t.Fatalf("a signal without samples has empty trace ids, got %#v", inc.Signals[0].TraceIDs)
	}

	if _, err := s.Get(ctx, []string{"ws-2"}, ids[0]); !errors.Is(err, incident.ErrNotFound) {
		t.Fatalf("another workspace read the incident: %v", err)
	}
	if _, err := s.Get(ctx, nil, ids[0]); !errors.Is(err, incident.ErrNotFound) {
		t.Fatalf("an empty scope read the incident: %v", err)
	}

	for _, subject := range []string{"a", "b", "c"} {
		if _, err := s.Create(ctx, newIncident("ws-1", subject)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create(ctx, newIncident("ws-2", "wf")); err != nil {
		t.Fatal(err)
	}
	if err := s.Resolve(ctx, ids[0], at, "u"); err != nil {
		t.Fatal(err)
	}
	if err := s.Resolve(ctx, ids[0], at, "u"); !errors.Is(err, incident.ErrNotFound) {
		t.Fatalf("second resolve: %v", err)
	}
	page, total, err := s.List(ctx, []string{"ws-1"}, "", 1, 3)
	if err != nil || total != 4 || len(page) != 3 || page[0].Status != incident.StatusOpen {
		t.Fatalf("list page 1: %d %d %v", total, len(page), err)
	}
	page, _, _ = s.List(ctx, []string{"ws-1"}, "", 2, 3)
	if len(page) != 1 || page[0].ID != ids[0] || page[0].ResolvedBy != "u" {
		t.Fatalf("resolved incidents list last: %+v", page)
	}
	if _, total, _ := s.List(ctx, []string{"ws-1"}, incident.StatusResolved, 1, 10); total != 1 {
		t.Fatalf("resolved filter total %d", total)
	}
	if open, _ := s.ListOpen(ctx, "ws-2"); len(open) != 1 {
		t.Fatalf("ws-2 open: %d", len(open))
	}
	if r, err := s.RecentlyResolved(ctx, "ws-1", "workflow", "wf", at.Add(-time.Hour)); err != nil || r == nil || r.ID != ids[0] {
		t.Fatalf("recently resolved: %+v %v", r, err)
	}
	if r, _ := s.RecentlyResolved(ctx, "ws-1", "workflow", "wf", at.Add(time.Hour)); r != nil {
		t.Fatal("resolved before since was returned")
	}

	past := at.Add(-time.Hour)
	for _, m := range []*incident.Mute{
		{WorkspaceID: "ws-1", Scope: "workflow", Subject: "wf", CreatedBy: "u"},
		{WorkspaceID: "ws-1", Scope: "workflow", Subject: "a", Metric: "cost", Until: &past, CreatedBy: "u"},
	} {
		if err := s.AddMute(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	mutes, err := s.Mutes(ctx, "ws-1")
	if err != nil || len(mutes) != 1 {
		t.Fatalf("active mutes: %+v %v", mutes, err)
	}
	if err := s.DeleteMute(ctx, "ws-2", mutes[0].ID); !errors.Is(err, incident.ErrNotFound) {
		t.Fatalf("another workspace deleted the mute: %v", err)
	}
	if err := s.DeleteMute(ctx, "ws-1", mutes[0].ID); err != nil {
		t.Fatal(err)
	}

	if last, err := s.LastEvaluated(ctx, []string{"ws-1"}); err != nil || last != nil {
		t.Fatalf("never evaluated: %v %v", last, err)
	}
	for _, ev := range []struct {
		ws string
		at time.Time
	}{{"ws-1", at.Add(-time.Hour)}, {"ws-1", at}, {"ws-2", at.Add(-time.Minute)}} {
		if err := s.MarkEvaluated(ctx, ev.ws, ev.at); err != nil {
			t.Fatal(err)
		}
	}
	if last, err := s.LastEvaluated(ctx, []string{"ws-1"}); err != nil || last == nil || !last.Equal(at) {
		t.Fatalf("ws-1 last evaluated: %v %v", last, err)
	}
	if last, _ := s.LastEvaluated(ctx, []string{"ws-1", "ws-2"}); last == nil || !last.Equal(at.Add(-time.Minute)) {
		t.Fatalf("oldest across scope: %v", last)
	}
}
