package alert_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tracium/api/internal/alert"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/testing/pgtest"
)

func TestStore(t *testing.T) {
	pool := pgtest.NewPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ('ws', 'u', 'production', 'ws', 'prod', 'owner')`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	inc := &incident.Incident{WorkspaceID: "ws", Scope: "workflow", Subject: "wf", Status: incident.StatusOpen, Severity: "critical", Title: "wf", OpenedAt: at, UpdatedAt: at, LastSignalAt: at}
	if _, err := incident.NewStore(pool).Create(ctx, inc); err != nil {
		t.Fatal(err)
	}
	s := alert.NewStore(pool)
	ch := alert.Channel{WorkspaceID: "ws", Kind: alert.KindWebhook, Name: "hook", Target: "https://example.com/x", Secret: "whsec_1", MinSeverity: "warning", NotifyResolved: true, Enabled: true, CreatedBy: "u"}
	if err := s.CreateChannel(ctx, &ch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Channel(ctx, "other", ch.ID); err != alert.ErrNotFound {
		t.Fatalf("channel visible from another workspace: %v", err)
	}
	if name, err := s.WorkspaceName(ctx, "ws"); err != nil || name != "production" {
		t.Fatal(name, err)
	}

	d := alert.NewDispatcher(s, nil, alert.DefaultOptions())
	for _, when := range []time.Time{at, at, at.Add(time.Hour)} {
		if err := d.Notify(ctx, incident.Event{Type: incident.EventOpened, Incident: incident.Incident{ID: inc.ID, WorkspaceID: "ws", Severity: "critical", UpdatedAt: when}}); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := s.Deliveries(ctx, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Seq != 0 || ds[1].Seq != 1 || ds[0].ChannelName != "hook" {
		t.Fatalf("deliveries %+v", ds)
	}

	var mu sync.Mutex
	claimed := map[int]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := s.Claim(ctx, time.Now(), time.Minute, 10)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, c := range got {
				claimed[c.Seq]++
				if c.Channel.Secret != "whsec_1" || c.Payload.Event != "opened" {
					t.Errorf("claim lacks channel or payload: %+v", c)
				}
			}
		})
	}
	wg.Wait()
	if len(claimed) != 2 || claimed[0] != 1 || claimed[1] != 1 {
		t.Fatalf("concurrent claims: %v", claimed)
	}

	now := time.Now().UTC()
	ds[0].Status, ds[0].Attempts, ds[0].SentAt = alert.StatusSent, 1, &now
	if err := s.Finish(ctx, ds[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(ctx, time.Now().Add(time.Hour), time.Minute, 10); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("after lease: %+v", got)
	}
	ch.Enabled, ch.MinSeverity = false, "critical"
	if err := s.UpdateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChannel(ctx, "ws", ch.ID); err != nil {
		t.Fatal(err)
	}
	if ds, _ := s.Deliveries(ctx, inc.ID); len(ds) != 0 {
		t.Fatal("deliveries outlived their channel")
	}
}

func TestRuleStore(t *testing.T) {
	pool := pgtest.NewPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ('ws', 'u', 'production', 'ws', 'prod', 'owner')`); err != nil {
		t.Fatal(err)
	}
	s := alert.NewStore(pool)
	r := alert.Rule{WorkspaceID: "ws", Scope: "client", Subject: "*", Period: "day", ThresholdUSD: 50, Enabled: true, CreatedBy: "u"}
	if err := s.CreateRule(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rule(ctx, "other", r.ID); err != alert.ErrRuleNotFound {
		t.Fatalf("rule visible from another workspace: %v", err)
	}
	if err := s.DeleteRule(ctx, "other", r.ID); err != alert.ErrRuleNotFound {
		t.Fatalf("rule deleted from another workspace: %v", err)
	}
	r.ThresholdUSD, r.Enabled = 75, false
	if err := s.UpdateRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	rules, err := s.Rules(ctx, "ws")
	if err != nil || len(rules) != 1 || rules[0].ThresholdUSD != 75 || rules[0].Enabled {
		t.Fatalf("rules %+v %v", rules, err)
	}
	bad := alert.Rule{WorkspaceID: "ws", Scope: "client", Period: "day", ThresholdUSD: 0, CreatedBy: "u"}
	if err := s.CreateRule(ctx, &bad); err == nil {
		t.Fatal("threshold 0 accepted")
	}
	if err := s.DeleteRule(ctx, "ws", r.ID); err != nil {
		t.Fatal(err)
	}
}
