package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
)

// VersionReviews counts one workflow version's run reviews. Unflagged counts
// reviews of runs with no silent-failure flag and no incident.
type VersionReviews struct {
	Workflow, Version           string
	Reviews, Correct            int64
	Unflagged, UnflaggedCorrect int64
	FirstAt, LastAt             time.Time
}

// AccuracyOptions holds every threshold of the accuracy-drop alert.
type AccuracyOptions struct {
	// MinReviews is the fewest run reviews a version needs to be compared.
	MinReviews int64
	// A drop fires at z >= Z and at least Drop accuracy lost; critical from CriticalDrop.
	Z, Drop, CriticalDrop float64
	// Current is how long after the latest contributing review the drop stays current.
	Current time.Duration
}

func DefaultAccuracyOptions() AccuracyOptions {
	return AccuracyOptions{MinReviews: 20, Z: 2.5, Drop: 0.10, CriticalDrop: 0.25, Current: 24 * time.Hour}
}

type AccuracySource interface {
	RunAccuracy(ctx context.Context, workspaceID string) ([]VersionReviews, error)
}

// FindingWriter records the drop on the newer version's release report, when it has one.
type FindingWriter interface {
	AddFinding(ctx context.Context, workspaceID, workflow, version string, f anomaly.Finding) error
}

// AccuracyWatch is an incident signal source: one review_accuracy anomaly per
// workflow whose newest reviewed version lost accuracy against the one before.
type AccuracyWatch struct {
	reviews AccuracySource
	reports FindingWriter
	o       AccuracyOptions
}

func NewAccuracyWatch(reviews AccuracySource, reports FindingWriter, o AccuracyOptions) *AccuracyWatch {
	return &AccuracyWatch{reviews: reviews, reports: reports, o: o}
}

func (w *AccuracyWatch) Anomalies(ctx context.Context, workspaceID string, now time.Time) ([]model.Anomaly, error) {
	rows, err := w.reviews.RunAccuracy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	eligible := map[string][]VersionReviews{}
	var order []string
	for _, v := range rows {
		if v.Reviews < w.o.MinReviews {
			continue
		}
		if _, ok := eligible[v.Workflow]; !ok {
			order = append(order, v.Workflow)
		}
		eligible[v.Workflow] = append(eligible[v.Workflow], v)
	}
	var out []model.Anomaly
	var errs []error
	for _, wf := range order {
		vs := eligible[wf]
		if len(vs) < 2 {
			continue
		}
		a, ok := AccuracyDrop(vs[len(vs)-2], vs[len(vs)-1], now, w.o)
		if !ok {
			continue
		}
		out = append(out, a)
		if w.reports != nil {
			f := anomaly.Finding{Metric: incident.AccuracyMetric, Prev: a.Expected, Next: a.Observed, Verdict: anomaly.Regressed, Severity: a.Severity}
			if err := w.reports.AddFinding(ctx, workspaceID, wf, a.VersionChange.Version, f); err != nil {
				errs = append(errs, fmt.Errorf("accuracy finding %s: %w", wf, err))
			}
		}
	}
	return out, errors.Join(errs...)
}

// AccuracyDrop compares next's reviewed accuracy with prev's, unflagged runs
// against unflagged runs when both versions have enough of them.
func AccuracyDrop(prev, next VersionReviews, now time.Time, o AccuracyOptions) (model.Anomaly, bool) {
	latest := next.LastAt
	if prev.LastAt.After(latest) {
		latest = prev.LastAt
	}
	if now.Sub(latest) >= o.Current {
		return model.Anomaly{}, false
	}
	pc, pn, nc, nn := prev.Correct, prev.Reviews, next.Correct, next.Reviews
	basis := "all reviews compared, too few of unflagged runs"
	if prev.Unflagged >= o.MinReviews && next.Unflagged >= o.MinReviews {
		pc, pn, nc, nn, basis = prev.UnflaggedCorrect, prev.Unflagged, next.UnflaggedCorrect, next.Unflagged, "unflagged runs only"
	}
	p, n := float64(pc)/float64(pn), float64(nc)/float64(nn)
	z := -anomaly.TwoProportionZ(pc, pn, nc, nn)
	if !(z >= o.Z) || p-n < o.Drop {
		return model.Anomaly{}, false
	}
	severity := "warning"
	if p-n >= o.CriticalDrop {
		severity = "critical"
	}
	first := next.FirstAt.UnixMilli()
	return model.Anomaly{
		Metric: incident.AccuracyMetric, Scope: "workflow", Workflow: next.Workflow, BucketMs: first,
		Observed: n, Expected: p, Deviation: n - p, Score: z, Direction: "down", Severity: severity,
		Summary: fmt.Sprintf("Reviewed accuracy %.1f%% on %s, %.1f%% on %s (%d reviews against %d; %s).",
			n*100, next.Version, p*100, prev.Version, nn, pn, basis),
		VersionChange: &model.VersionChange{Workflow: next.Workflow, Version: next.Version, FirstSeenMs: first},
	}, true
}
