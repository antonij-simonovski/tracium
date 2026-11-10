package incident

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// Options holds every threshold of the fold.
type Options struct {
	// MinSeverity is the lowest severity that opens or reopens an incident.
	MinSeverity string
	// HourlyCurrent is how long an hourly anomaly's bucket stays current.
	HourlyCurrent time.Duration
	// ReopenWindow is how soon after resolving an incident new evidence reopens it.
	ReopenWindow time.Duration
	// HourlyQuiet and DailyQuiet are how long an incident must go without a
	// current anomaly before it resolves, by the slowest metric it holds.
	HourlyQuiet time.Duration
	DailyQuiet  time.Duration
}

func DefaultOptions() Options {
	return Options{MinSeverity: "warning", HourlyCurrent: 3 * time.Hour, ReopenWindow: 24 * time.Hour,
		HourlyQuiet: 2 * time.Hour, DailyQuiet: 26 * time.Hour}
}

// ResolvedLookback is how far back State.Resolved must reach: a daily bucket
// stays current for up to two days, and evidence already seen never reopens.
const ResolvedLookback = 48 * time.Hour

var dailyMetrics = map[string]bool{"cost": true, "error_rate": true, "runs": true, "latency_p95": true}

// ReleaseMetric prefixes a release report's regressed findings, as in
// release_regression:truncation_rate; the report decides how long they stay current.
const ReleaseMetric = "release_regression"

func IsReleaseMetric(metric string) bool { return strings.HasPrefix(metric, ReleaseMetric+":") }

// alwaysCurrent metrics describe now, and resolve as soon as they stop.
func alwaysCurrent(metric string) bool {
	return metric == "heartbeat" || metric == "ingest_silence" || IsReleaseMetric(metric) || metric == AccuracyMetric
}

// Current reports whether an anomaly describes the present, so it may open or
// extend an incident.
func Current(a model.Anomaly, now time.Time, o Options) bool {
	switch {
	case alwaysCurrent(a.Metric):
		return true
	case dailyMetrics[a.Metric]:
		return a.BucketMs >= now.UTC().Truncate(24*time.Hour).Add(-24*time.Hour).UnixMilli()
	default:
		return a.BucketMs >= now.Add(-o.HourlyCurrent).UnixMilli()
	}
}

// Subject is the scope and subject an anomaly's incident is grouped by.
func Subject(a model.Anomaly) (scope, subject string) {
	switch a.Scope {
	case "workflow":
		return a.Scope, a.Workflow
	case "client":
		return a.Scope, a.Client
	}
	return a.Scope, ""
}

func Muted(mutes []Mute, scope, subject, metric string, now time.Time) bool {
	for _, m := range mutes {
		matches := m.Metric == "" || m.Metric == metric || (m.Metric == ReleaseMetric && IsReleaseMetric(metric))
		if m.Scope == scope && m.Subject == subject && matches && (m.Until == nil || m.Until.After(now)) {
			return true
		}
	}
	return false
}

// State is one workspace's incidents before a fold. Resolved holds each
// subject's latest incident resolved within ResolvedLookback.
type State struct {
	Open     []Incident
	Resolved []Incident
	Mutes    []Mute
}

type ChangeKind string

const (
	ChangeCreate  ChangeKind = "create"
	ChangeUpdate  ChangeKind = "update"
	ChangeReopen  ChangeKind = "reopen"
	ChangeResolve ChangeKind = "resolve"
)

// Change is one store operation. Incident is the state after it, with every
// signal; Signals are the ones to upsert. Event is empty when nothing is emitted.
type Change struct {
	Kind     ChangeKind
	Incident Incident
	Signals  []Signal
	Event    EventType
}

type subjectKey struct{ scope, subject string }

// Fold turns one workspace's anomalies and incidents into store changes. It
// does no I/O, so the evaluator and a backtest share it.
func Fold(workspaceID string, now time.Time, anomalies []model.Anomaly, st State, o Options) []Change {
	groups := map[subjectKey][]model.Anomaly{}
	var order []subjectKey
	for _, a := range anomalies {
		scope, subject := Subject(a)
		if !Current(a, now, o) || Muted(st.Mutes, scope, subject, a.Metric, now) {
			continue
		}
		k := subjectKey{scope, subject}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], a)
	}
	open := map[subjectKey]Incident{}
	for _, inc := range st.Open {
		open[subjectKey{inc.Scope, inc.Subject}] = inc
	}
	resolved := map[subjectKey]Incident{}
	for _, inc := range st.Resolved {
		k := subjectKey{inc.Scope, inc.Subject}
		if prev, ok := resolved[k]; inc.ResolvedAt != nil && (!ok || inc.ResolvedAt.After(*prev.ResolvedAt)) {
			resolved[k] = inc
		}
	}

	var out []Change
	for _, k := range order {
		group := groups[k]
		signals := make([]Signal, len(group))
		for i, a := range group {
			signals[i] = Signal{Metric: a.Metric, BucketMs: a.BucketMs, Observed: a.Observed, Expected: a.Expected, Score: a.Score,
				Direction: a.Direction, Severity: a.Severity, Summary: a.Summary, TraceIDs: a.TraceIDs}
		}
		if inc, ok := open[k]; ok {
			before := inc.Severity
			merge(&inc, group, signals, now)
			var ev EventType
			if severityRank(inc.Severity) > severityRank(before) {
				ev = EventEscalated
			}
			out = append(out, Change{Kind: ChangeUpdate, Incident: inc, Signals: signals, Event: ev})
			continue
		}
		prev, seen := resolved[k]
		if !opens(signals, prev, o) {
			continue
		}
		if seen && now.Sub(*prev.ResolvedAt) < o.ReopenWindow {
			prev.Status, prev.ResolvedAt, prev.ResolvedBy = StatusOpen, nil, ""
			merge(&prev, group, signals, now)
			out = append(out, Change{Kind: ChangeReopen, Incident: prev, Signals: signals, Event: EventOpened})
			continue
		}
		inc := Incident{WorkspaceID: workspaceID, Scope: k.scope, Subject: k.subject, Status: StatusOpen, OpenedAt: now}
		merge(&inc, group, signals, now)
		out = append(out, Change{Kind: ChangeCreate, Incident: inc, Signals: signals, Event: EventOpened})
	}

	for _, inc := range st.Open {
		if _, active := groups[subjectKey{inc.Scope, inc.Subject}]; active || now.Sub(inc.LastSignalAt) < quiet(inc, o) {
			continue
		}
		at := now
		inc.Status, inc.ResolvedAt, inc.UpdatedAt = StatusResolved, &at, now
		out = append(out, Change{Kind: ChangeResolve, Incident: inc, Event: EventResolved})
	}
	return out
}

// opens reports whether signals hold new evidence at or above MinSeverity:
// evidence the subject's resolved incident already holds never reopens it.
func opens(signals []Signal, resolved Incident, o Options) bool {
	for _, s := range signals {
		if severityRank(s.Severity) >= severityRank(o.MinSeverity) && indexOf(resolved.Signals, s) < 0 {
			return true
		}
	}
	return false
}

func merge(inc *Incident, group []model.Anomaly, signals []Signal, now time.Time) {
	inc.Signals = append([]Signal(nil), inc.Signals...)
	for _, s := range signals {
		if i := indexOf(inc.Signals, s); i >= 0 {
			if len(s.TraceIDs) == 0 {
				s.TraceIDs = inc.Signals[i].TraceIDs
			}
			inc.Signals[i] = s
		} else {
			inc.Signals = append(inc.Signals, s)
		}
	}
	for _, s := range inc.Signals {
		if severityRank(s.Severity) > severityRank(inc.Severity) {
			inc.Severity = s.Severity
		}
	}
	for _, a := range group {
		if a.VersionChange != nil && (inc.VersionChange == nil || IsReleaseMetric(a.Metric)) {
			inc.VersionChange = a.VersionChange
		}
	}
	inc.LastSignalAt, inc.UpdatedAt = now, now
	inc.Title = Title(inc.Scope, inc.Subject, inc.Signals)
}

func indexOf(signals []Signal, s Signal) int {
	for i, x := range signals {
		if x.Metric == s.Metric && x.BucketMs == s.BucketMs {
			return i
		}
	}
	return -1
}

func quiet(inc Incident, o Options) time.Duration {
	var d time.Duration
	for _, s := range inc.Signals {
		switch {
		case dailyMetrics[s.Metric]:
			d = max(d, o.DailyQuiet)
		case !alwaysCurrent(s.Metric):
			d = max(d, o.HourlyQuiet)
		}
	}
	return d
}

var metricNouns = map[string]string{
	"cost": "cost", "error_rate": "error rate", "runs": "run volume", "latency_p95": "p95 run latency",
	"truncation_rate": "truncated LLM calls", "refusal_rate": "refused LLM calls", "tool_error_rate": "tool errors",
	"llm_calls_per_run": "LLM calls per run", "tool_repeat": "repeated tool calls", "cache_hit_ratio": "cache hit ratio",
	AccuracyMetric: "reviewed accuracy",
}

// Title names the subject and its most severe signal, e.g.
// "sf-truncation: truncated LLM calls at 66.7% (+1 more signal)".
func Title(scope, subject string, signals []Signal) string {
	if len(signals) == 0 {
		return ""
	}
	top := signals[0]
	metrics := map[string]bool{}
	for _, s := range signals {
		metrics[s.Metric] = true
		if r, t := severityRank(s.Severity), severityRank(top.Severity); r > t || (r == t && math.Abs(s.Score) > math.Abs(top.Score)) {
			top = s
		}
	}
	name := subject
	if scope == "workspace" || name == "" {
		name = "Workspace"
	}
	value := query.FormatAnomalyValue(top.Metric, top.Observed)
	var title string
	if noun, ok := metricNouns[top.Metric]; ok {
		title = fmt.Sprintf("%s: %s at %s", name, noun, value)
	} else if IsReleaseMetric(top.Metric) {
		headline, _, _ := strings.Cut(top.Summary, ": ")
		title = fmt.Sprintf("%s: %s", name, headline)
	} else if top.Metric == "heartbeat" {
		title = fmt.Sprintf("%s: silent for %s", name, value)
	} else {
		title = fmt.Sprintf("%s: %s", name, top.Metric)
	}
	switch n := len(metrics) - 1; {
	case n == 1:
		title += " (+1 more signal)"
	case n > 1:
		title += fmt.Sprintf(" (+%d more signals)", n)
	}
	return title
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	}
	return 0
}
