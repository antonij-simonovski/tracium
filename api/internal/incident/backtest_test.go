package incident

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

type fakeDetector struct {
	mu        sync.Mutex
	filters   []query.MetricsFilter
	ticks     []int64
	anomalies func(f query.MetricsFilter) []model.Anomaly
	block     chan struct{}
}

func (d *fakeDetector) Anomalies(_ context.Context, f query.MetricsFilter) ([]model.Anomaly, error) {
	if d.block != nil {
		<-d.block
	}
	d.mu.Lock()
	d.filters = append(d.filters, f)
	d.mu.Unlock()
	if d.anomalies == nil {
		return nil, nil
	}
	return d.anomalies(f), nil
}

func (d *fakeDetector) Heartbeats(_ context.Context, _ query.MetricsFilter, ticks []int64) ([][]model.Anomaly, error) {
	d.ticks = ticks
	return make([][]model.Anomaly, len(ticks)), nil
}

func (d *fakeDetector) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.filters)
}

func TestReplay(t *testing.T) {
	start := now.Add(-30 * 24 * time.Hour)
	ticks := replayTicks(start, now)
	burst := func(daysAgo int64, severity string) []model.Anomaly {
		var out []model.Anomaly
		for h := int64(0); h < 3; h++ {
			out = append(out, hourly("truncation_rate", "wf", severity, daysAgo*24+h))
		}
		return out
	}
	cases := []struct {
		name      string
		anomalies []model.Anomaly
		want      int
	}{
		{"three consecutive hours are one incident", burst(5, "warning"), 1},
		{"two bursts three days apart are two", append(burst(5, "warning"), burst(8, "critical")...), 2},
		{"info only opens nothing", burst(5, "info"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Replay("ws", tc.anomalies, ticks, nil, DefaultOptions())
			if len(got) != tc.want {
				t.Fatalf("incidents = %d, want %d: %+v", len(got), tc.want, got)
			}
			for _, inc := range got {
				if len(inc.Signals) != 3 || inc.Status != StatusResolved {
					t.Fatalf("incident = %+v, want 3 signals, resolved", inc)
				}
			}
		})
	}
}

func TestReplayHeartbeatResolvesWhenClientReturns(t *testing.T) {
	ticks := replayTicks(now.Add(-24*time.Hour), now)
	hb := make([][]model.Anomaly, len(ticks))
	for i := 3; i < 6; i++ {
		hb[i] = []model.Anomaly{{Metric: "heartbeat", Scope: "client", Client: "acme", BucketMs: ticks[2], Severity: "warning"}}
	}
	got := Replay("ws", nil, ticks, hb, DefaultOptions())
	if len(got) != 1 || got[0].Status != StatusResolved || got[0].ResolvedAt.UnixMilli() != ticks[6] {
		t.Fatalf("incidents = %+v, want one resolved at tick 6", got)
	}
}

func TestDetectRangeSlicesCoverEveryHourOnce(t *testing.T) {
	f, _ := query.ParseRange("30d", now)
	f.WorkspaceIDs = []string{"ws"}
	d := &fakeDetector{anomalies: func(f query.MetricsFilter) []model.Anomaly {
		end := f.End.UnixMilli()
		from := max(f.Start.UnixMilli(), end-hourlySlice.Milliseconds()) / hourMs * hourMs
		out := []model.Anomaly{
			{Metric: "cost", Scope: "workspace", BucketMs: f.Start.Truncate(24 * time.Hour).UnixMilli()},
			{Metric: "heartbeat", Scope: "client", Client: "c", BucketMs: end},
		}
		for h := from; h < end; h += hourMs {
			out = append(out, model.Anomaly{Metric: "tool_repeat", Scope: "workflow", Workflow: "wf", BucketMs: h})
		}
		return out
	}}
	got, err := detectRange(context.Background(), d, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.filters) != 5 {
		t.Fatalf("Anomalies calls = %d, want 5", len(d.filters))
	}
	hours := map[int64]int{}
	daily := 0
	for _, a := range got {
		switch a.Metric {
		case "tool_repeat":
			hours[a.BucketMs]++
		case "cost":
			daily++
		default:
			t.Fatalf("unexpected %s", a.Metric)
		}
	}
	if daily != 1 {
		t.Fatalf("daily anomalies = %d, want 1 (from the first slice)", daily)
	}
	for h := f.Start.Truncate(time.Hour).UnixMilli(); h < now.UnixMilli(); h += hourMs {
		if hours[h] != 1 {
			t.Fatalf("hour %s seen %d times", time.UnixMilli(h).UTC(), hours[h])
		}
	}
	if len(hours) != int(now.Sub(f.Start.Truncate(time.Hour))/time.Hour)+1 {
		t.Fatalf("hours = %d", len(hours))
	}
	for _, g := range d.filters {
		if len(g.WorkspaceIDs) != 1 || g.WorkspaceIDs[0] != "ws" || !g.Start.Equal(f.Start) {
			t.Fatalf("filter = %+v", g)
		}
	}
}

func TestBacktestCache(t *testing.T) {
	d := &fakeDetector{}
	b := NewBacktester(d, DefaultOptions(), time.Hour, time.Minute)
	clock := now
	b.now = func() time.Time { return clock }
	ctx := context.Background()
	if _, err := b.Backtest(ctx, "ws", "7d"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Backtest(ctx, "ws", "7d"); err != nil || d.calls() != 1 {
		t.Fatalf("calls = %d, err = %v, want the cached run", d.calls(), err)
	}
	if _, err := b.Backtest(ctx, "other", "7d"); err != nil || d.calls() != 2 {
		t.Fatalf("calls = %d, want a run per workspace", d.calls())
	}
	clock = clock.Add(time.Hour + time.Second)
	if _, err := b.Backtest(ctx, "ws", "7d"); err != nil || d.calls() != 3 {
		t.Fatalf("calls = %d, want a new run after expiry", d.calls())
	}
}

func TestBacktestConcurrentCallsShareOneRun(t *testing.T) {
	d := &fakeDetector{block: make(chan struct{})}
	b := NewBacktester(d, DefaultOptions(), time.Hour, time.Minute)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.Backtest(context.Background(), "ws", "7d"); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(d.block)
	wg.Wait()
	if d.calls() != 1 {
		t.Fatalf("calls = %d, want 1", d.calls())
	}
}

func TestBacktestTimeout(t *testing.T) {
	b := NewBacktester(blockingDetector{&fakeDetector{}}, DefaultOptions(), time.Hour, 10*time.Millisecond)
	if _, err := b.Backtest(context.Background(), "ws", "7d"); err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

type blockingDetector struct{ *fakeDetector }

func (b blockingDetector) Anomalies(ctx context.Context, _ query.MetricsFilter) ([]model.Anomaly, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
