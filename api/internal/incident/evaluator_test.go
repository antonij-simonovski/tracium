package incident_test

import (
	"context"
	"errors"
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

type releaseSource []model.Anomaly

func (s releaseSource) Anomalies(context.Context, string, time.Time) ([]model.Anomaly, error) {
	return s, nil
}

func TestEvaluatorFoldsReleaseRegressions(t *testing.T) {
	daily := &model.VersionChange{Workflow: "sf-truncation", Version: "v1", FirstSeenMs: 1}
	old := truncation()
	old.VersionChange = daily
	finding := func(metric, severity, summary string) model.Anomaly {
		return model.Anomaly{Metric: incident.ReleaseMetric + ":" + metric, Scope: "workflow", Workflow: "sf-truncation", BucketMs: 5, Severity: severity,
			Summary: "v2 regressed against v1: " + summary, VersionChange: &model.VersionChange{Workflow: "sf-truncation", Version: "v2", FirstSeenMs: 5}}
	}
	findings := releaseSource{
		finding("truncation_rate", "critical", "truncated LLM calls 24.6% on v2, 1.0% on v1 (38 runs against 400)."),
		finding("cost_per_run", "warning", "median cost per run $0.0400 on v2, $0.0200 on v1 (38 runs against 400)."),
	}
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": {old}, "quiet": nil}}
	store := &mocks.MockIncidentStore{}
	e := incident.NewEvaluator(det, store, freeLock{}, workspaces{"a", "quiet"}, nil, config(), findings)
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, inc := range store.Incidents {
		subjects = append(subjects, inc.WorkspaceID+"/"+inc.Subject)
	}
	if len(store.Incidents) != 2 {
		t.Fatalf("incidents = %v, want one per workspace", subjects)
	}
	inc := store.Incidents[0]
	if len(inc.Signals) != 3 || inc.VersionChange.Version != "v2" || inc.Title != "sf-truncation: v2 regressed against v1 (+2 more signals)" {
		t.Errorf("incident = %+v", inc)
	}

	store.MuteList = []incident.Mute{{WorkspaceID: "quiet", Scope: "workflow", Subject: "sf-truncation", Metric: incident.ReleaseMetric}}
	store.Incidents = nil
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, inc := range store.Incidents {
		if inc.WorkspaceID == "quiet" {
			t.Errorf("muted release regression opened %+v", inc)
		}
	}

	store.MuteList = []incident.Mute{{WorkspaceID: "quiet", Scope: "workflow", Subject: "sf-truncation", Metric: incident.ReleaseMetric + ":truncation_rate"}}
	store.Incidents = nil
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, inc := range store.Incidents {
		if inc.WorkspaceID == "quiet" && (len(inc.Signals) != 1 || inc.Signals[0].Metric != "release_regression:cost_per_run") {
			t.Errorf("a finding mute should leave only the cost signal: %+v", inc.Signals)
		}
	}
	if len(store.Incidents) != 2 {
		t.Errorf("incidents = %d, want the quiet workspace's cost finding to open one", len(store.Incidents))
	}
}

type source []model.Anomaly

func (s source) Anomalies(context.Context, string, time.Time) ([]model.Anomaly, error) { return s, nil }

func TestEvaluatorFoldsSourceSignals(t *testing.T) {
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": nil}}
	store := &mocks.MockIncidentStore{}
	budget := model.Anomaly{Metric: "budget", Scope: "client", Client: "acme", Severity: "critical", BucketMs: 0, Observed: 60, Expected: 50}
	e := incident.NewEvaluator(det, store, freeLock{}, workspaces{"a"}, nil, config(), source{budget})
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.Incidents) != 1 || store.Incidents[0].Subject != "acme" || store.Incidents[0].Signals[0].Metric != "budget" {
		t.Fatalf("source signal not folded: %+v", store.Incidents)
	}
}

type flakySource struct {
	source
	fail bool
}

func (s *flakySource) Anomalies(ctx context.Context, ws string, now time.Time) ([]model.Anomaly, error) {
	if s.fail {
		return nil, errors.New("clickhouse timeout")
	}
	return s.source.Anomalies(ctx, ws, now)
}

func TestEvaluatorKeepsIncidentsOpenWhenASourceFails(t *testing.T) {
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": nil}}
	store := &mocks.MockIncidentStore{}
	rec := &recorder{}
	src := &flakySource{source: source{{Metric: "budget", Scope: "client", Client: "acme", Severity: "critical", Observed: 60, Expected: 50}}}
	e := incident.NewEvaluator(det, store, freeLock{}, workspaces{"a"}, rec, config(), src)
	_ = e.Tick(context.Background())
	src.fail = true
	_ = e.Tick(context.Background())
	if len(store.Incidents) != 1 || store.Incidents[0].Status != incident.StatusOpen || len(rec.events) != 1 {
		t.Fatalf("a failing source resolved its incident: %+v %+v", store.Incidents, rec.events)
	}
}

// resolvingDetector stands in for a user resolving the incident while a tick samples a new signal.
type resolvingDetector struct {
	*fakeDetector
	store *mocks.MockIncidentStore
}

func (d resolvingDetector) SignalSamples(ctx context.Context, f query.MetricsFilter, metric string, b int64, n int) ([]string, error) {
	for _, inc := range d.store.Incidents {
		_ = d.store.Resolve(ctx, inc.ID, time.Now(), "user")
	}
	return d.fakeDetector.SignalSamples(ctx, f, metric, b, n)
}

func TestEvaluatorLeavesAUserResolveAlone(t *testing.T) {
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": {truncation()}}}
	store := &mocks.MockIncidentStore{}
	rec := &recorder{}
	if err := incident.NewEvaluator(det, store, freeLock{}, workspaces{"a"}, rec, config()).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := truncation()
	next.BucketMs += time.Hour.Milliseconds()
	next.Severity = "critical"
	det.anomalies["a"] = append(det.anomalies["a"], next)
	if err := incident.NewEvaluator(resolvingDetector{det, store}, store, freeLock{}, workspaces{"a"}, rec, config()).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.Incidents[0].Status != incident.StatusResolved || len(rec.events) != 1 {
		t.Fatalf("the tick overwrote the resolve: %+v, events %+v", store.Incidents[0], rec.events)
	}
}

type flakyNotifier struct {
	recorder
	fail int
}

func (f *flakyNotifier) Notify(ctx context.Context, e incident.Event) error {
	if f.fail > 0 {
		f.fail--
		return errors.New("postgres down")
	}
	return f.recorder.Notify(ctx, e)
}

func TestEvaluatorRetriesAnEventItCouldNotQueue(t *testing.T) {
	det := &fakeDetector{anomalies: map[string][]model.Anomaly{"a": {truncation()}}}
	store := &mocks.MockIncidentStore{}
	n := &flakyNotifier{fail: 1}
	e := incident.NewEvaluator(det, store, freeLock{}, workspaces{"a"}, n, config())
	for range 3 {
		if err := e.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.Incidents) != 1 || len(n.events) != 1 || n.events[0].Type != incident.EventOpened {
		t.Fatalf("want the opened event queued once on the next tick, got %+v", n.events)
	}
}
