package incident

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// Detector is the part of the ClickHouse repository the evaluator reads.
type Detector interface {
	Anomalies(ctx context.Context, f query.MetricsFilter) ([]model.Anomaly, error)
	SignalSamples(ctx context.Context, f query.MetricsFilter, metric string, bucketMs int64, limit int) ([]string, error)
	ActiveWorkspaces(ctx context.Context, since time.Time) ([]string, error)
}

// SignalSource adds anomalies of its own to a workspace's evaluation, such as release regressions.
type SignalSource interface {
	Anomalies(ctx context.Context, workspaceID string, now time.Time) ([]model.Anomaly, error)
}

type WorkspaceLister interface {
	AllIDs(ctx context.Context) ([]string, error)
}

type Locker interface {
	TryLock(ctx context.Context) (release func(), ok bool, err error)
}

type Config struct {
	Interval time.Duration
	Timeout  time.Duration
	Fold     Options
}

const (
	activeWindow = 28 * 24 * time.Hour
	sampleLimit  = 3
)

type Evaluator struct {
	detector   Detector
	store      Store
	lock       Locker
	workspaces WorkspaceLister
	notifier   Notifier
	sources    []SignalSource
	now        func() time.Time
	cfg        Config
}

func NewEvaluator(d Detector, s Store, l Locker, w WorkspaceLister, n Notifier, cfg Config, sources ...SignalSource) *Evaluator {
	if n == nil {
		n = NopNotifier{}
	}
	return &Evaluator{detector: d, store: s, lock: l, workspaces: w, notifier: n, sources: sources, now: time.Now, cfg: cfg}
}

// Run evaluates on every interval until ctx is cancelled.
func (e *Evaluator) Run(ctx context.Context) {
	t := time.NewTicker(e.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
				log.Printf("incident evaluator: %v", err)
			}
		}
	}
}

// Tick evaluates every active workspace once, unless another replica holds the lock.
func (e *Evaluator) Tick(ctx context.Context) error {
	release, ok, err := e.lock.TryLock(ctx)
	if err != nil || !ok {
		return err
	}
	defer release()
	now := e.now()
	all, err := e.workspaces.AllIDs(ctx)
	if err != nil {
		return err
	}
	active, err := e.detector.ActiveWorkspaces(ctx, now.Add(-activeWindow))
	if err != nil {
		return err
	}
	isActive := map[string]bool{}
	for _, id := range active {
		isActive[id] = true
	}
	for _, id := range all {
		if !isActive[id] {
			continue
		}
		wctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
		if err := e.evaluate(wctx, id, now); err != nil {
			log.Printf("incident evaluator: workspace %s: %v", id, err)
		} else if err := e.store.MarkEvaluated(wctx, id, now); err != nil {
			log.Printf("incident evaluator: workspace %s: %v", id, err)
		}
		cancel()
	}
	return nil
}

func (e *Evaluator) evaluate(ctx context.Context, workspaceID string, now time.Time) error {
	f, err := query.ParseRange("7d", now)
	if err != nil {
		return err
	}
	f.WorkspaceIDs = []string{workspaceID}
	anomalies, err := e.detector.Anomalies(ctx, f)
	if err != nil {
		return err
	}
	var errs []error
	for _, src := range e.sources {
		extra, err := src.Anomalies(ctx, workspaceID, now)
		if err != nil {
			errs = append(errs, err)
		}
		anomalies = append(anomalies, extra...)
	}
	st := State{}
	if st.Open, err = e.store.ListOpen(ctx, workspaceID); err != nil {
		return err
	}
	if st.Mutes, err = e.store.Mutes(ctx, workspaceID); err != nil {
		return err
	}
	looked := map[subjectKey]bool{}
	for _, inc := range st.Open {
		looked[subjectKey{inc.Scope, inc.Subject}] = true
	}
	for _, a := range anomalies {
		scope, subject := Subject(a)
		if k := (subjectKey{scope, subject}); !looked[k] && Current(a, now, e.cfg.Fold) {
			looked[k] = true
			inc, err := e.store.RecentlyResolved(ctx, workspaceID, scope, subject, now.Add(-ResolvedLookback))
			if err != nil {
				return err
			}
			if inc != nil {
				st.Resolved = append(st.Resolved, *inc)
			}
		}
	}
	prior := map[string]Incident{}
	for _, inc := range append(st.Open, st.Resolved...) {
		prior[inc.ID] = inc
	}

	for _, c := range Fold(workspaceID, now, anomalies, st, e.cfg.Fold) {
		if err := e.sample(ctx, f, &c, prior[c.Incident.ID]); err != nil {
			errs = append(errs, err)
		}
		notify, err := e.apply(ctx, &c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if notify && c.Event != "" {
			if err := e.notifier.Notify(ctx, Event{Type: c.Event, Incident: c.Incident}); err != nil {
				errs = append(errs, fmt.Errorf("notify %s: %w", c.Event, err))
			}
		}
	}
	return errors.Join(errs...)
}

// sample fills example runs for signals the incident did not hold before, so
// the spans are read once per signal, never on every tick.
func (e *Evaluator) sample(ctx context.Context, f query.MetricsFilter, c *Change, prior Incident) error {
	if c.Incident.Scope == "workflow" {
		f.Workflow = c.Incident.Subject
	}
	for i, s := range c.Signals {
		if len(s.TraceIDs) > 0 || indexOf(prior.Signals, s) >= 0 {
			continue
		}
		ids, err := e.detector.SignalSamples(ctx, f, s.Metric, s.BucketMs, sampleLimit)
		if err != nil {
			return fmt.Errorf("sample %s: %w", s.Metric, err)
		}
		c.Signals[i].TraceIDs = ids
		if j := indexOf(c.Incident.Signals, s); j >= 0 {
			c.Incident.Signals[j].TraceIDs = ids
		}
	}
	return nil
}

// apply writes one change; notify is false when the change turned out to be a no-op.
func (e *Evaluator) apply(ctx context.Context, c *Change) (notify bool, err error) {
	switch c.Kind {
	case ChangeCreate:
		return e.store.Create(ctx, &c.Incident)
	case ChangeResolve:
		err := e.store.Resolve(ctx, c.Incident.ID, *c.Incident.ResolvedAt, "")
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	default:
		return true, e.store.Update(ctx, c.Incident, c.Signals)
	}
}
