package query

import (
	"context"
	"testing"
	"time"
)

func TestSpendFromClickHouse(t *testing.T) {
	r, ws := newTestRepository(t)
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	for i, row := range []struct {
		user, workflow string
		at             time.Time
		cost           float64
	}{
		{"acme", "etl", now, 3},
		{"acme", "chat", now, 1},
		{"globex", "etl", now, 2},
		{"acme", "etl", today.Add(-time.Hour), 100},
	} {
		if _, err := r.db.Exec(`INSERT INTO tracium.spans (trace_id, span_id, start_time_ms, end_time_ms, workspace_id, user_id, workflow_name, source, model, cost_usd)
VALUES (?, ?, ?, ?, ?, ?, ?, 'span', 'gpt-4o', ?)`, ws, string(rune('a'+i)), row.at.UnixMilli(), row.at.UnixMilli()+10, ws, row.user, row.workflow, row.cost); err != nil {
			t.Fatal(err)
		}
	}
	for scope, want := range map[string]map[string]float64{
		"workspace": {"": 6},
		"client":    {"acme": 4, "globex": 2},
		"workflow":  {"etl": 5, "chat": 1},
	} {
		got, err := r.Spend(context.Background(), ws, scope, today)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", scope, got, want)
		}
		for k, v := range want {
			if got[k] < v-0.001 || got[k] > v+0.001 {
				t.Errorf("%s %q: got %v, want %v", scope, k, got[k], v)
			}
		}
	}
}
