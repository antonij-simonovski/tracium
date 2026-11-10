// Package release grades each new workflow version against the one before it
// and feeds regressions to the incident engine.
package release

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

const (
	StatusPending = "pending"
	StatusFinal   = "final"

	VerdictInsufficient = "insufficient"
)

var ErrNotFound = errors.New("release report not found")

type Report struct {
	WorkspaceID string            `json:"workspace_id"`
	Workflow    string            `json:"workflow"`
	Version     string            `json:"version"`
	PrevVersion string            `json:"prev_version"`
	FirstSeenAt time.Time         `json:"first_seen_at"`
	Status      string            `json:"status"`
	Verdict     string            `json:"verdict"`
	Runs        int64             `json:"runs"`
	PrevRuns    int64             `json:"prev_runs"`
	Findings    []anomaly.Finding `json:"findings"`
	Rechecked   bool              `json:"-"`
	CreatedAt   time.Time         `json:"created_at"`
	FinalizedAt *time.Time        `json:"finalized_at"`
}

// Store persists reports. Reads taking workspaceIDs are scoped to them.
type Store interface {
	// Ensure creates a pending report unless the version already has one.
	Ensure(ctx context.Context, r Report) error
	// Active lists the reports a tick looks at: pending ones, ok ones not yet
	// rechecked, and regressions finalized at or after since.
	Active(ctx context.Context, workspaceID string, since time.Time) ([]Report, error)
	// Save writes a report's outcome; a regressed report is never overwritten.
	Save(ctx context.Context, r Report) error
	List(ctx context.Context, workspaceIDs []string, status string, limit int) ([]Report, error)
	Get(ctx context.Context, workspaceIDs []string, workflow, version string) (*Report, error)
	// Verdicts maps each of the workflow's versions to its verdict, or "pending".
	Verdicts(ctx context.Context, workspaceIDs []string, workflow string) (map[string]string, error)
}

// Stats is the part of the ClickHouse repository the tracker reads.
type Stats interface {
	NewVersions(ctx context.Context, f query.MetricsFilter, since time.Time) ([]model.NewVersion, error)
	VersionRunStats(ctx context.Context, f query.MetricsFilter, workflow, version string, from, to time.Time) (model.VersionStats, error)
}

// Options holds every threshold of a report's lifecycle and comparison.
type Options struct {
	// Window is how recently a version must first appear to get a report.
	Window time.Duration
	// A pending report is graded at EvaluateRuns, or at EarlyRuns once
	// EarlyAfter has passed, and gives up as insufficient after GiveUpAfter.
	EvaluateRuns, EarlyRuns int64
	EarlyAfter, GiveUpAfter time.Duration
	// An ok report is graded once more at RecheckRuns or RecheckAfter.
	RecheckRuns  int64
	RecheckAfter time.Duration
	// The previous version is read over PrevWindow, widened to PrevWideWindow
	// when it has fewer than PrevMinRuns runs there.
	PrevWindow, PrevWideWindow time.Duration
	PrevMinRuns                int64
	// Current is how long after grading a regression keeps its incident open.
	Current time.Duration
	Compare anomaly.CompareOptions
}

func DefaultOptions() Options {
	return Options{Window: 48 * time.Hour, EvaluateRuns: 30, EarlyRuns: 10, EarlyAfter: 2 * time.Hour, GiveUpAfter: 24 * time.Hour,
		RecheckRuns: 300, RecheckAfter: 24 * time.Hour, PrevWindow: 24 * time.Hour, PrevWideWindow: 7 * 24 * time.Hour, PrevMinRuns: 30,
		Current: 24 * time.Hour, Compare: anomaly.DefaultCompareOptions()}
}

// Tracker creates and grades reports on every evaluator tick.
type Tracker struct {
	stats Stats
	store Store
	o     Options
}

func NewTracker(stats Stats, store Store, o Options) *Tracker {
	return &Tracker{stats: stats, store: store, o: o}
}

// Anomalies advances the workspace's reports and returns one release_regression
// anomaly per regression graded within Options.Current.
func (t *Tracker) Anomalies(ctx context.Context, workspaceID string, now time.Time) ([]model.Anomaly, error) {
	f := query.MetricsFilter{WorkspaceIDs: []string{workspaceID}, Start: now.Add(-t.o.Window), End: now}
	versions, err := t.stats.NewVersions(ctx, f, f.Start)
	if err != nil {
		return nil, err
	}
	runs := map[[2]string]int64{}
	for _, v := range versions {
		runs[[2]string{v.Workflow, v.Version}] = v.Runs
		err := t.store.Ensure(ctx, Report{WorkspaceID: workspaceID, Workflow: v.Workflow, Version: v.Version, PrevVersion: v.PrevVersion,
			FirstSeenAt: time.UnixMilli(v.FirstSeenMs).UTC(), Status: StatusPending})
		if err != nil {
			return nil, err
		}
	}
	reports, err := t.store.Active(ctx, workspaceID, now.Add(-t.o.Current))
	if err != nil {
		return nil, err
	}
	var out []model.Anomaly
	var errs []error
	for _, r := range reports {
		if err := t.step(ctx, f, &r, runs[[2]string{r.Workflow, r.Version}], now); err != nil {
			errs = append(errs, fmt.Errorf("release %s %s: %w", r.Workflow, r.Version, err))
			continue
		}
		if r.Verdict == anomaly.Regressed && r.FinalizedAt != nil && now.Sub(*r.FinalizedAt) < t.o.Current {
			out = append(out, Signals(r)...)
		}
	}
	return out, errors.Join(errs...)
}

func (t *Tracker) step(ctx context.Context, f query.MetricsFilter, r *Report, runs int64, now time.Time) error {
	o, age := t.o, now.Sub(r.FirstSeenAt)
	switch {
	case r.Status == StatusPending && (runs >= o.EvaluateRuns || (age >= o.EarlyAfter && runs >= o.EarlyRuns)):
	case r.Status == StatusPending && age >= o.GiveUpAfter:
		r.Status, r.Verdict, r.Runs, r.FinalizedAt = StatusFinal, VerdictInsufficient, runs, &now
		return t.store.Save(ctx, *r)
	case r.Status == StatusFinal && r.Verdict == "ok" && !r.Rechecked && (runs >= o.RecheckRuns || age >= o.RecheckAfter):
	default:
		return nil
	}
	// Capped so a late grade never reads more than a day of the new version.
	to := now
	if limit := r.FirstSeenAt.Add(o.RecheckAfter); limit.Before(to) {
		to = limit
	}
	next, err := t.stats.VersionRunStats(ctx, f, r.Workflow, r.Version, r.FirstSeenAt, to)
	if err != nil {
		return err
	}
	prev, err := t.stats.VersionRunStats(ctx, f, r.Workflow, r.PrevVersion, r.FirstSeenAt.Add(-o.PrevWindow), r.FirstSeenAt)
	if err == nil && prev.Runs < o.PrevMinRuns {
		prev, err = t.stats.VersionRunStats(ctx, f, r.Workflow, r.PrevVersion, r.FirstSeenAt.Add(-o.PrevWideWindow), r.FirstSeenAt)
	}
	if err != nil {
		return err
	}
	findings := anomaly.CompareVersions(prev, next, o.Compare)
	verdict := anomaly.Verdict(findings)
	if r.Status == StatusFinal {
		r.Rechecked = true
		if verdict != anomaly.Regressed {
			return t.store.Save(ctx, *r)
		}
	}
	r.Status, r.Verdict, r.Runs, r.PrevRuns, r.Findings, r.FinalizedAt = StatusFinal, verdict, next.Runs, prev.Runs, findings, &now
	return t.store.Save(ctx, *r)
}

var findingNouns = map[string]string{
	"error_rate": "errored runs", "truncation_rate": "truncated LLM calls", "refusal_rate": "refused LLM calls",
	"tool_error_rate": "tool errors", "cost_per_run": "median cost per run", "latency_p95": "p95 run latency",
	"llm_calls_per_run": "median LLM calls per run", "output_tokens_per_run": "median output tokens per run", "cache_hit_ratio": "cache hit ratio",
}

// Signals are the anomalies a regressed report feeds the incident engine, one
// per regressed finding, so grouping, mutes and notifications treat them like
// any detector's.
func Signals(r Report) []model.Anomaly {
	var out []model.Anomaly
	for _, f := range r.Findings {
		// A review_accuracy finding raises its own signal.
		if f.Verdict != anomaly.Regressed || !anomaly.FindingMetrics[f.Metric] {
			continue
		}
		direction := "up"
		if f.Next < f.Prev {
			direction = "down"
		}
		out = append(out, model.Anomaly{Metric: incident.ReleaseMetric + ":" + f.Metric, Scope: "workflow", Workflow: r.Workflow,
			BucketMs: r.FirstSeenAt.UnixMilli(), Observed: f.Next, Expected: f.Prev, Direction: direction, Severity: f.Severity,
			Summary: fmt.Sprintf("%s regressed against %s: %s %s on %s, %s on %s (%d runs against %d).", r.Version, r.PrevVersion, findingNouns[f.Metric],
				query.FormatAnomalyValue(f.Metric, f.Next), r.Version, query.FormatAnomalyValue(f.Metric, f.Prev), r.PrevVersion, r.Runs, r.PrevRuns),
			VersionChange: &model.VersionChange{Workflow: r.Workflow, Version: r.Version, FirstSeenMs: r.FirstSeenAt.UnixMilli()}})
	}
	return out
}
