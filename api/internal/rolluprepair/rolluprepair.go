// Package rolluprepair rebuilds the daily rollup tables (tracium.metrics_daily
// and tracium.metrics_daily_cost) for specific days from the raw spans.
//
// Why this exists: each rollup is filled by a materialized view, which in
// ClickHouse is an INSERT trigger — it never retracts on DELETE. So removing a
// bad span from tracium.spans corrects the short-window (raw-span) reads but
// leaves the span's contribution baked into the rollups the long windows read.
// Remediation must clean both: delete the span, then rebuild the affected days
// here. It covers both rollups.
//
// The rebuild is delete-then-insert per day, bounded to that day's spans, so it
// is idempotent (re-running a day converges) and its cost tracks the days asked
// for, never the table size. There is deliberately no "rebuild everything" mode.
package rolluprepair

import (
	"fmt"
	"strings"
	"time"
)

// dateLayout is the bucket_date format ClickHouse prints and parses (a Date).
const dateLayout = "2006-01-02"

// Rollup describes one rollup table and the materialized view that fills it, so
// the repair can reproduce exactly what the trigger would have written.
//
// body renders the view's aggregating SELECT with a given WHERE-clause body (the
// predicate text, without the "WHERE" keyword; empty means no WHERE). baseWhere
// is the view's own predicate. The canonical select (baseWhere alone) mirrors the
// deployed view exactly and drives the drift check; the bounded select (baseWhere
// AND a day range) drives the rebuild INSERT, pushing the bound into the scan so
// it prunes on the spans sort key rather than aggregating the whole table.
type Rollup struct {
	Table     string // e.g. "tracium.metrics_daily"
	View      string // e.g. "tracium.metrics_daily_mv"
	baseWhere string // the view's own WHERE body (may be empty)
	body      func(whereBody string) string
}

// whereClause renders a WHERE body into a clause, or nothing when empty — so a
// view with no WHERE (metrics_daily_cost) produces text with no WHERE, matching
// the deployed view exactly under the drift check.
func whereClause(body string) string {
	if body == "" {
		return ""
	}
	return "\nWHERE " + body
}

// combine ANDs a base predicate with an extra one, handling an empty base.
func combine(base, extra string) string {
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + " AND " + extra
}

// Rollups are the tables this tool repairs, mirroring collector migrations
// 003 (metrics_daily) and 008 (metrics_daily_cost, as rebuilt by 015). If those migrations change,
// change these to match — the drift check (see DriftMatches) refuses to run when
// a mirror no longer matches the deployed view, so a stale mirror fails loud
// rather than writing aggregates the trigger would not have produced.
var Rollups = []Rollup{
	{
		Table:     "tracium.metrics_daily",
		View:      "tracium.metrics_daily_mv",
		baseWhere: "source = 'span'",
		body: func(where string) string {
			return `SELECT
    toDate(start_time_ms / 1000)                       AS bucket_date,
    user_id,
    workspace_id,
    workflow_name,
    workflow_version,
    if(model_normalized != '', model_normalized, model) AS model,
    sum(cost_usd)                                      AS cost,
    sum(input_tokens)                                  AS input_tokens,
    sum(output_tokens)                                 AS output_tokens,
    count()                                            AS span_count,
    uniqState(trace_id)                                AS runs,
    uniqIfState(trace_id, error_type != '' OR error_message != '') AS error_runs
FROM tracium.spans` + whereClause(where) + `
GROUP BY bucket_date, user_id, workspace_id, workflow_name, workflow_version, model`
		},
	},
	{
		Table:     "tracium.metrics_daily_cost",
		View:      "tracium.metrics_daily_cost_mv",
		baseWhere: "", // migration 008 has no WHERE — it splits sources with sumIf
		body: func(where string) string {
			return `SELECT
    toDate(start_time_ms / 1000)       AS bucket_date,
    user_id,
    workspace_id,
    sumIf(cost_usd, source = 'span')                AS span_cost,
    sumIf(cost_usd, source = 'metric')              AS metric_cost,
    sumIf(cost_usd, source = 'span' AND model = '') AS reported_cost
FROM tracium.spans` + whereClause(where) + `
GROUP BY bucket_date, user_id, workspace_id`
		},
	},
}

// CanonicalSelect is the view's own unbounded SELECT — the text compared against
// the deployed view to detect drift.
func (r Rollup) CanonicalSelect() string { return r.body(r.baseWhere) }

// dayPredicate is the half-open millisecond range for one day, in the server
// timezone — the same timezone toDate(start_time_ms/1000) uses in the view, so
// the range and the view's bucketing agree by construction (the runner still
// proves it per day before writing). day must be YYYY-MM-DD (validated upstream).
func dayPredicate(day string) string {
	from := fmt.Sprintf("toInt64(toUnixTimestamp(toDateTime('%s 00:00:00'))) * 1000", day)
	to := fmt.Sprintf("toInt64(toUnixTimestamp(toDateTime('%s 00:00:00') + INTERVAL 1 DAY)) * 1000", day)
	return fmt.Sprintf("start_time_ms >= %s AND start_time_ms < %s", from, to)
}

// RebuildInsert returns the INSERT that repopulates one day of this rollup from
// the raw spans, bounded to that day.
func (r Rollup) RebuildInsert(day string) string {
	return "INSERT INTO " + r.Table + "\n" + r.body(combine(r.baseWhere, dayPredicate(day)))
}

// SpanStatsWhere is the WHERE body used to gather a day's span stats: always the
// span source (both rollups derive their guards from real spans) plus the day.
func SpanStatsWhere(day string) string {
	return combine("source = 'span'", dayPredicate(day))
}

// DeleteDay returns the mutation that clears one day of this rollup. Paired with
// RebuildInsert and run first, it makes the rebuild idempotent.
func (r Rollup) DeleteDay(day string) string {
	return fmt.Sprintf("ALTER TABLE %s DELETE WHERE bucket_date = toDate('%s')", r.Table, day)
}

// Days returns the inclusive [from, to] range of YYYY-MM-DD dates to rebuild.
// It errors on malformed dates or an inverted range.
func Days(from, to string) ([]string, error) {
	lo, err := time.Parse(dateLayout, from)
	if err != nil {
		return nil, fmt.Errorf("--from %q is not YYYY-MM-DD: %w", from, err)
	}
	hi, err := time.Parse(dateLayout, to)
	if err != nil {
		return nil, fmt.Errorf("--to %q is not YYYY-MM-DD: %w", to, err)
	}
	if hi.Before(lo) {
		return nil, fmt.Errorf("--to (%s) is before --from (%s)", to, from)
	}
	var days []string
	for d := lo; !d.After(hi); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format(dateLayout))
	}
	return days, nil
}

// DayStats summarises a single day's raw spans, gathered from tracium.spans.
type DayStats struct {
	RawSpans     int64  // source='span' rows in the day's time range
	DistinctDays int64  // distinct bucket_date those rows fall in (must be 1)
	MinDay       string // earliest bucket_date among them (YYYY-MM-DD)
	MaxDay       string // latest bucket_date among them
	AgeSeconds   int64  // seconds since the most recent span's received_at
}

// Options controls the guards.
type Options struct {
	QuiesceSeconds int64 // refuse a day whose newest span is younger than this
	AllowEmpty     bool  // rebuild days that have no spans left (empties the rollup day)
	Force          bool  // skip the quiescence guard (only when ingestion is stopped)
}

// Action is the decision for one day.
type Action int

const (
	// Rebuild: delete the day and re-insert it from raw spans.
	Rebuild Action = iota
	// SkipEmpty: no spans remain and AllowEmpty is false — leave the day as-is.
	SkipEmpty
	// RefuseOpen: the day is still receiving spans; rebuilding would double-count.
	RefuseOpen
	// RefuseTimezone: the day's time range spans more than the one bucket_date,
	// so the server timezone disagrees with the view's bucketing.
	RefuseTimezone
)

// Classify decides what to do with one day from its stats and the options. It is
// the whole guard policy, pure and table-independent (the stats come from the
// shared source, tracium.spans), so both rollups reuse one decision per day.
func Classify(day string, s DayStats, o Options) (Action, string) {
	if s.RawSpans == 0 {
		if o.AllowEmpty {
			return Rebuild, "no spans remain; rebuilding to an empty day (--allow-empty)"
		}
		return SkipEmpty, "no spans remain (aged out under the spans TTL?); pass --allow-empty to empty this day"
	}
	if s.DistinctDays != 1 || s.MinDay != day || s.MaxDay != day {
		return RefuseTimezone, fmt.Sprintf("time range for %s covers bucket_date %s..%s — the server timezone disagrees with the view's bucketing", day, s.MinDay, s.MaxDay)
	}
	if s.AgeSeconds < o.QuiesceSeconds && !o.Force {
		return RefuseOpen, fmt.Sprintf("newest span for %s arrived %ds ago (< %ds); rebuilding a day still receiving spans double-counts — wait, stop ingestion, or pass --force", day, s.AgeSeconds, o.QuiesceSeconds)
	}
	return Rebuild, "rebuilding from raw spans"
}

// squash removes all whitespace so two SQL texts compare on meaning, not layout.
// Both sides are first passed through the server's own formatter (EXPLAIN SYNTAX)
// by the caller, so this only strips the remaining formatting noise.
func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// DriftMatches reports whether the mirror's SELECT and the deployed view's
// SELECT are the same query once formatted and whitespace-stripped. Both inputs
// must already be EXPLAIN SYNTAX output from the same server.
func DriftMatches(mineNormalized, liveNormalized string) bool {
	return squash(mineNormalized) == squash(liveNormalized) && liveNormalized != ""
}
