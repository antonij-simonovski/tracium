package incident_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/testing/mocks"
	"github.com/tracium/api/testing/pgtest"
)

type fakeDetector struct {
	mu        sync.Mutex
	anomalies map[string][]model.Anomaly
	calls     int
	samples   []string
}

func (d *fakeDetector) Anomalies(_ context.Context, f query.MetricsFilter) ([]model.Anomaly, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return d.anomalies[f.WorkspaceIDs[0]], nil
}

func (d *fakeDetector) SignalSamples(_ context.Context, f query.MetricsFilter, metric string, _ int64, _ int) ([]string, error) {
	d.samples = append(d.samples, f.WorkspaceIDs[0]+"/"+f.Workflow+"/"+metric)
	return []string{"sample-" + f.WorkspaceIDs[0]}, nil
}

func (d *fakeDetector) ActiveWorkspaces(context.Context, time.Time) ([]string, error) {
	var ids []string
	for id := range d.anomalies {
		ids = append(ids, id)
	}
	return ids, nil
}

type workspaces []string

func (w workspaces) AllIDs(context.Context) ([]string, error) { return w, nil }

type freeLock struct{}

func (freeLock) TryLock(context.Context) (func(), bool, error) { return func() {}, true, nil }

type recorder struct{ events []incident.Event }

func (r *recorder) Notify(_ context.Context, e incident.Event) error {
	r.events = append(r.events, e)
	return nil
}

func truncation() model.Anomaly {
	return model.Anomaly{Metric: "truncation_rate", Scope: "workflow", Workflow: "sf-truncation", Severity: "warning",
		BucketMs: time.Now().Truncate(time.Hour).UnixMilli(), Observed: 0.5}
}

func config() incident.Config {
	return incident.Config{Interval: time.Minute, Timeout: time.Second, Fold: incident.DefaultOptions()}
}

func TestEvaluatorKeepsWorkspacesApart(t *testing.T) {
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": {truncation()}, "b": {truncation()}}}
	store := &mocks.MockIncidentStore{}
	rec := &recorder{}
	e := incident.NewEvaluator(det, store, freeLock{}, workspaces{"a", "b", "gone"}, rec, config())
	for range 2 {
		if err := e.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.Incidents) != 2 || store.Incidents[0].WorkspaceID == store.Incidents[1].WorkspaceID {
		t.Fatalf("want one incident per workspace, got %+v", store.Incidents)
	}
	if len(rec.events) != 2 || rec.events[0].Type != incident.EventOpened {
		t.Fatalf("want two opened events, got %+v", rec.events)
	}
	if len(det.samples) != 2 || det.samples[0] != "a/sf-truncation/truncation_rate" {
		t.Fatalf("samples should be read once per new signal, got %v", det.samples)
	}
	if _, ok := store.Evaluated["a"]; !ok || len(store.Evaluated) != 2 {
		t.Fatalf("want a and b marked evaluated, got %v", store.Evaluated)
	}
	for _, inc := range store.Incidents {
		if got := inc.Signals[0].TraceIDs; len(got) != 1 || got[0] != "sample-"+inc.WorkspaceID {
			t.Fatalf("trace ids %v", got)
		}
	}
}

func TestEvaluatorSkipsTickWhenLockHeld(t *testing.T) {
	pool := pgtest.NewPool(t)
	store := incident.NewStore(pool)
	release, ok, err := store.TryLock(context.Background())
	if err != nil || !ok {
		t.Fatalf("first lock: %v %v", ok, err)
	}
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": {truncation()}}}
	e := incident.NewEvaluator(det, &mocks.MockIncidentStore{}, store, workspaces{"a"}, nil, config())
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if det.calls != 0 {
		t.Fatal("tick ran while another connection held the lock")
	}
	release()
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if det.calls != 1 {
		t.Fatal("tick did not run after the lock was released")
	}
}
