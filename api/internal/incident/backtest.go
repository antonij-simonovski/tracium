package incident

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// BacktestDetector is the part of the ClickHouse repository a backtest reads.
type BacktestDetector interface {
	Anomalies(ctx context.Context, f query.MetricsFilter) ([]model.Anomaly, error)
	Heartbeats(ctx context.Context, f query.MetricsFilter, ticks []int64) ([][]model.Anomaly, error)
}

type BacktestItem struct {
	Scope         string               `json:"scope"`
	Subject       string               `json:"subject"`
	Severity      string               `json:"severity"`
	Title         string               `json:"title"`
	OpenedAt      time.Time            `json:"opened_at"`
	ResolvedAt    *time.Time           `json:"resolved_at"`
	Signals       int                  `json:"signals"`
	VersionChange *model.VersionChange `json:"version_change"`
}

type BacktestResult struct {
	Range      string         `json:"range"`
	Incidents  int            `json:"incidents"`
	BySeverity map[string]int `json:"by_severity"`
	ByMetric   map[string]int `json:"by_metric"`
	Items      []BacktestItem `json:"items"`
}

const (
	backtestItemLimit = 50
	// hourlySlice is how much of a window Anomalies scores hourly metrics over.
	hourlySlice = 7 * 24 * time.Hour
)

// Backtester answers what the evaluator would have opened over a past range,
// caching each workspace and range for ttl and sharing concurrent runs.
type Backtester struct {
	detector BacktestDetector
	opts     Options
	ttl      time.Duration
	timeout  time.Duration
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]*backtestEntry
}

type backtestEntry struct {
	done    chan struct{}
	res     *BacktestResult
	err     error
	expires time.Time
}

func NewBacktester(d BacktestDetector, o Options, ttl, timeout time.Duration) *Backtester {
	return &Backtester{detector: d, opts: o, ttl: ttl, timeout: timeout, now: time.Now, cache: map[string]*backtestEntry{}}
}

// Backtest replays rng ("7d" or "30d") for one workspace. It fails with
// context.DeadlineExceeded when the run takes longer than the timeout.
func (b *Backtester) Backtest(ctx context.Context, workspaceID, rng string) (*BacktestResult, error) {
	key := workspaceID + "|" + rng
	b.mu.Lock()
	now := b.now()
	for k, e := range b.cache {
		if !e.expires.IsZero() && now.After(e.expires) {
			delete(b.cache, k)
		}
	}
	e, ok := b.cache[key]
	if !ok {
		e = &backtestEntry{done: make(chan struct{})}
		b.cache[key] = e
		// The run outlives the request that started it, since others wait on it.
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.timeout)
		go func() {
			defer cancel()
			res, err := b.run(run, workspaceID, rng, now)
			if err != nil && run.Err() != nil {
				err = run.Err()
			}
			b.mu.Lock()
			e.res, e.err = res, err
			if err != nil {
				delete(b.cache, key)
			} else {
				e.expires = b.now().Add(b.ttl)
			}
			b.mu.Unlock()
			close(e.done)
		}()
	}
	b.mu.Unlock()
	select {
	case <-e.done:
		return e.res, e.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *Backtester) run(ctx context.Context, workspaceID, rng string, now time.Time) (*BacktestResult, error) {
	f, err := query.ParseRange(rng, now)
	if err != nil {
		return nil, err
	}
	f.WorkspaceIDs = []string{workspaceID}
	anomalies, err := detectRange(ctx, b.detector, f)
	if err != nil {
		return nil, err
	}
	ticks := replayTicks(f.Start, now)
	heartbeats, err := b.detector.Heartbeats(ctx, f, ticks)
	if err != nil {
		return nil, err
	}
	res := summarize(Replay(workspaceID, anomalies, ticks, heartbeats, b.opts))
	res.Range = rng
	return &res, nil
}

// detectRange runs detection once for the daily metrics over the whole range,
// and once per 7-day slice for the hourly ones, newest first. Slices end on an
// hour so no hourly bucket is scored twice or left out.
func detectRange(ctx context.Context, d BacktestDetector, f query.MetricsFilter) ([]model.Anomaly, error) {
	var out []model.Anomaly
	for g, first := f, true; g.End.After(f.Start); first = false {
		got, err := d.Anomalies(ctx, g)
		if err != nil {
			return nil, err
		}
		for _, a := range got {
			if a.Metric != "heartbeat" && (first || !dailyMetrics[a.Metric]) {
				out = append(out, a)
			}
		}
		g.End = g.End.Truncate(time.Hour).Add(-hourlySlice)
	}
	return out, nil
}

// replayTicks steps hourly through the range and ends at now.
func replayTicks(start, now time.Time) []int64 {
	var ticks []int64
	for t := start.Truncate(time.Hour).Add(time.Hour); t.Before(now); t = t.Add(time.Hour) {
		ticks = append(ticks, t.UnixMilli())
	}
	return append(ticks, now.UnixMilli())
}

// Replay folds anomalies into incidents one tick at a time, from an empty
// state and without mutes. An anomaly is seen once its bucket is over, or at
// the last tick; heartbeats[i] are the heartbeat anomalies at ticks[i].
func Replay(workspaceID string, anomalies []model.Anomaly, ticks []int64, heartbeats [][]model.Anomaly, o Options) []Incident {
	if len(ticks) == 0 {
		return nil
	}
	last := ticks[len(ticks)-1]
	seenAt := func(a model.Anomaly) int64 {
		width := time.Hour
		if dailyMetrics[a.Metric] {
			width = 24 * time.Hour
		}
		return min(a.BucketMs+width.Milliseconds(), last)
	}
	sorted := append([]model.Anomaly(nil), anomalies...)
	sort.SliceStable(sorted, func(i, j int) bool { return seenAt(sorted[i]) < seenAt(sorted[j]) })

	var all []Incident
	seen := 0
	for i, t := range ticks {
		for seen < len(sorted) && seenAt(sorted[seen]) <= t {
			seen++
		}
		batch := sorted[:seen:seen]
		if i < len(heartbeats) {
			batch = append(batch, heartbeats[i]...)
		}
		now := time.UnixMilli(t)
		var st State
		for _, inc := range all {
			if inc.Status == StatusOpen {
				st.Open = append(st.Open, inc)
			} else if !inc.ResolvedAt.Before(now.Add(-ResolvedLookback)) {
				st.Resolved = append(st.Resolved, inc)
			}
		}
		for _, c := range Fold(workspaceID, now, batch, st, o) {
			if c.Kind == ChangeCreate {
				c.Incident.ID = strconv.Itoa(len(all))
				all = append(all, c.Incident)
				continue
			}
			n, _ := strconv.Atoi(c.Incident.ID)
			all[n] = c.Incident
		}
	}
	return all
}

func summarize(incidents []Incident) BacktestResult {
	res := BacktestResult{Incidents: len(incidents), BySeverity: map[string]int{}, ByMetric: map[string]int{}, Items: []BacktestItem{}}
	sort.SliceStable(incidents, func(i, j int) bool {
		a, b := incidents[i], incidents[j]
		if r, s := severityRank(a.Severity), severityRank(b.Severity); r != s {
			return r > s
		}
		return a.OpenedAt.After(b.OpenedAt)
	})
	for _, inc := range incidents {
		res.BySeverity[inc.Severity]++
		res.ByMetric[topSignal(inc.Signals).Metric]++
		if len(res.Items) < backtestItemLimit {
			res.Items = append(res.Items, BacktestItem{Scope: inc.Scope, Subject: inc.Subject, Severity: inc.Severity, Title: inc.Title,
				OpenedAt: inc.OpenedAt, ResolvedAt: inc.ResolvedAt, Signals: len(inc.Signals), VersionChange: inc.VersionChange})
		}
	}
	return res
}
