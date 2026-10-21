package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/tracium/api/internal/model"
)

// Query is a structured telemetry query. It never carries SQL: every name is
// looked up in the view catalog below or in the calls relation's live columns,
// every value is bound as a parameter, and the caller's workspace scope is
// applied to every relation it reads.
//
// A query either aggregates (Metrics grouped by Dimensions) or, with Columns,
// lists the matching calls or runs themselves.
type Query struct {
	View       string        `json:"view"`
	Metrics    []string      `json:"metrics"`
	Dimensions []string      `json:"dimensions"`
	Columns    []string      `json:"columns"`
	Filters    []QueryFilter `json:"filters"`
	// Having filters the aggregated rows on the query's metrics and formulas.
	Having      []QueryFilter  `json:"having"`
	Formulas    []QueryFormula `json:"formulas"`
	Granularity string         `json:"granularity"`
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	OrderBy     string         `json:"order_by"`
	Order       string         `json:"order"`
	Limit       int            `json:"limit"`
	// Page is 1-indexed; each page holds Limit rows.
	Page int `json:"page"`

	WorkspaceIDs []string `json:"-"`

	cols Columns
	// runAttrs and parentCols number the run attributes and parent span fields
	// the query reads, which become columns of the run and parent joins.
	runAttrs   []string
	parentCols []string
}

// QueryFilter restricts a query on one field. eq, neq, contains and the
// numeric comparisons take Value; in and not_in take Values; is_empty and
// not_empty take neither. A filter with Any instead matches when any of its
// filters does.
type QueryFilter struct {
	Field  string        `json:"field,omitempty"`
	Op     string        `json:"op,omitempty"`
	Value  FilterValue   `json:"value,omitempty"`
	Values []string      `json:"values,omitempty"`
	Any    []QueryFilter `json:"any,omitempty"`
}

// QueryFormula is a metric computed from the query's metrics, e.g. cost / runs.
type QueryFormula struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

// FilterValue also accepts a JSON number, since gt/ge/lt/le compare numbers.
type FilterValue string

func (v *FilterValue) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*v = FilterValue(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return errors.New("filter value must be a string or a number")
	}
	*v = FilterValue(n)
	return nil
}

// Columns maps each column of the calls relation to its kind. It is read from
// ClickHouse (QueryColumns), so a column a migration adds is queryable with no
// code change.
type Columns map[string]string

const (
	kindNumber = "number"
	kindText   = "text"
	kindTime   = "time"
)

// ErrInvalidQuery wraps every validation failure, so handlers answer 400 with
// the message, which names the valid choices.
var ErrInvalidQuery = errors.New("invalid query")

const (
	viewCalls = "calls"
	viewRuns  = "runs"

	defaultQueryWindow = 7 * 24 * time.Hour
	// shortcut: queries scan raw spans only, so the window is capped near the
	// default retention; route long windows to the daily rollups to lift it.
	maxQueryWindow    = 92 * 24 * time.Hour
	defaultQueryLimit = 100
	maxQueryLimit     = 1000
	defaultRowLimit   = 50
	maxRowLimit       = 200
	maxRowColumns     = 30
	maxFilters        = 10
	maxFilterValues   = 100
	maxFormulas       = 5
	maxFormulaChars   = 500
	// maxQueryOffset bounds how deep pages go, since ClickHouse still reads
	// every skipped row.
	maxQueryOffset = 100_000
	// rowTextChars clips long text such as span input and output.
	rowTextChars = 2000
)

// querySlots caps the custom queries one API process runs at once, since each
// may hold max_memory_usage until queryTimeout.
var querySlots = make(chan struct{}, 4)

var attrKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,128}$`)

// columnPattern guards discovered names, which are written into the SQL.
var columnPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var formulaNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

const (
	attrPrefix   = "attr."
	runPrefix    = "run."
	parentPrefix = "parent."
)

// Joins a calls-view field can need: the per-run subquery or the parent span.
const (
	joinRun    = "tw"
	joinParent = "pp"
)

type queryField struct {
	expr string
	// kind is number or text; empty means text.
	kind string
	join string
	doc  string
}

type queryView struct {
	doc        string
	dimensions map[string]queryField
	metrics    map[string]queryField
	// fields are usable like dimensions: in filters, columns and <func>:<field>.
	fields map[string]queryField
	// columns exposes every live column of the calls relation as a field, and
	// the run.<field> and parent.<column> fields.
	columns bool
}

// The runs view aggregates a per-trace subquery whose columns are prefixed r_
// so no alias shadows a spans column (ClickHouse would substitute it back).
var queryViews = map[string]queryView{
	viewCalls: {
		doc: "One row per LLM/tool call (span). Use for model, token and per-call questions, and to list calls.",
		dimensions: map[string]queryField{
			"model":         {expr: modelExpr, doc: "normalized model name"},
			"user":          {expr: "user_id", doc: "end-user / client id"},
			"workspace":     {expr: "workspace_id", doc: "workspace id"},
			"service":       {expr: "service_name", doc: "OTel service.name"},
			"span_name":     {expr: "name", doc: "span name"},
			"error_type":    {expr: "error_type", doc: "error type, empty when the call succeeded"},
			"finish_reason": {expr: "finish_reason", doc: "model finish reason"},
			"workflow":      {expr: "tw.r_workflow", join: joinRun, doc: "the workflow of the run the call belongs to"},
			"version":       {expr: "tw.r_version", join: joinRun, doc: "the workflow version of the run the call belongs to"},
		},
		metrics: map[string]queryField{
			"cost":            {expr: "sum(cost_usd)", doc: "USD spend"},
			"calls":           {expr: "count()", doc: "number of calls"},
			"runs":            {expr: "uniq(trace_id)", doc: "distinct runs (traces)"},
			"input_tokens":    {expr: "sum(input_tokens)", doc: "input tokens"},
			"output_tokens":   {expr: "sum(output_tokens)", doc: "output tokens"},
			"total_tokens":    {expr: "sum(input_tokens + output_tokens)", doc: "input + output tokens"},
			"errors":          {expr: "countIf" + spanErrored, doc: "failed calls"},
			"error_rate":      {expr: "avg(" + spanErrored + ")", doc: "share of calls that failed, 0-1"},
			"latency_avg_ms":  {expr: "avg(duration_ms)", doc: "mean call duration"},
			"latency_p50_ms":  {expr: "quantile(0.5)(duration_ms)", doc: "median call duration"},
			"latency_p95_ms":  {expr: "quantile(0.95)(duration_ms)", doc: "p95 call duration"},
			"latency_p99_ms":  {expr: "quantile(0.99)(duration_ms)", doc: "p99 call duration"},
			"unmetered_calls": {expr: "countIf(unmetered = 1)", doc: "calls that returned output without usage, so their cost is unknown"},
		},
		columns: true,
	},
	viewRuns: {
		doc: "One row per run (trace). Use for workflow, end-to-end latency and failure-rate questions, and to list runs.",
		dimensions: map[string]queryField{
			"workflow":  {expr: "r_workflow", doc: "workflow name"},
			"version":   {expr: "r_version", doc: "workflow version"},
			"user":      {expr: "r_user", doc: "end-user / client id"},
			"workspace": {expr: "r_workspace", doc: "workspace id"},
		},
		fields: map[string]queryField{
			"trace_id":      {expr: "trace_id", kind: kindText, doc: "run (trace) id"},
			"start_time_ms": {expr: "r_start", kind: kindNumber, doc: "when the run's first call started, Unix ms"},
			"duration_ms":   {expr: "r_dur", kind: kindNumber, doc: "end-to-end run duration"},
			"cost_usd":      {expr: "r_cost", kind: kindNumber, doc: "the run's USD spend"},
			"call_count":    {expr: "r_calls", kind: kindNumber, doc: "calls in the run"},
			"input_tokens":  {expr: "r_in", kind: kindNumber, doc: "the run's input tokens"},
			"output_tokens": {expr: "r_out", kind: kindNumber, doc: "the run's output tokens"},
			"errored":       {expr: "r_errored", kind: kindNumber, doc: "1 when any call in the run failed, else 0"},
		},
		metrics: map[string]queryField{
			"runs":           {expr: "count()", doc: "number of runs"},
			"errors":         {expr: "countIf(r_errored)", doc: "runs with at least one failed call"},
			"error_rate":     {expr: "avg(r_errored)", doc: "share of runs that failed, 0-1"},
			"cost":           {expr: "sum(r_cost)", doc: "USD spend"},
			"calls":          {expr: "sum(r_calls)", doc: "calls across the runs"},
			"input_tokens":   {expr: "sum(r_in)", doc: "input tokens"},
			"output_tokens":  {expr: "sum(r_out)", doc: "output tokens"},
			"total_tokens":   {expr: "sum(r_in + r_out)", doc: "input + output tokens"},
			"latency_avg_ms": {expr: "avg(r_dur)", doc: "mean end-to-end run duration"},
			"latency_p50_ms": {expr: "quantile(0.5)(r_dur)", doc: "median run duration"},
			"latency_p95_ms": {expr: "quantile(0.95)(r_dur)", doc: "p95 run duration"},
			"latency_p99_ms": {expr: "quantile(0.99)(r_dur)", doc: "p99 run duration"},
		},
	},
}

// relatedFields document the prefixed fields of the calls view.
var relatedFields = []model.QueryField{
	{Name: "run.<field>", Description: "the run the call belongs to: any runs-view dimension or column, e.g. run.cost_usd, run.errored, or run.attr.<key>"},
	{Name: "parent.<column>", Description: "the call's parent span: any stored column or parent.attr.<key>; when there is no parent in the window, text reads empty and numbers 0, so filter parent_span_id not_empty to skip root calls"},
}

// metricFuncs build a metric from any field as <func>:<field>.
var metricFuncs = map[string]struct {
	sql     string
	numeric bool
}{
	"sum":  {"sum(%s)", true},
	"avg":  {"avg(%s)", true},
	"min":  {"min(%s)", true},
	"max":  {"max(%s)", true},
	"p50":  {"quantile(0.5)(%s)", true},
	"p90":  {"quantile(0.9)(%s)", true},
	"p95":  {"quantile(0.95)(%s)", true},
	"p99":  {"quantile(0.99)(%s)", true},
	"uniq": {"uniq(%s)", false},
}

// runsSubquery's first %s takes the run attribute columns, the second the window.
const runsSubquery = `
    SELECT
        trace_id,
        ` + workflowExpr + ` AS r_workflow,
        ` + versionExpr + ` AS r_version,
        any(user_id)      AS r_user,
        any(workspace_id) AS r_workspace,
        min(start_time_ms) AS r_start,
        ` + traceDurationExpr + ` AS r_dur,
        max(` + spanErrored + `) AS r_errored,
        sum(cost_usd)      AS r_cost,
        count()            AS r_calls,
        sum(input_tokens)  AS r_in,
        sum(output_tokens) AS r_out%s
    FROM ` + tableCalls + `
    WHERE %s
    GROUP BY trace_id`

// reconcilable lists the calls-view dimensions the usage-metric rows also
// carry. A query grouped and filtered only by these gets the dashboard's
// reconciled cost; any other dimension falls back to span cost.
var reconcilable = map[string]bool{"user": true, "workspace": true}

var granularities = map[string]time.Duration{"hour": time.Hour, "day": 24 * time.Hour}

var filterOps = []string{"eq", "neq", "contains", "in", "not_in", "gt", "ge", "lt", "le", "is_empty", "not_empty"}

var comparisons = map[string]string{"gt": ">", "ge": ">=", "lt": "<", "le": "<="}

var havingOps = map[string]string{"eq": "=", "neq": "!=", "gt": ">", "ge": ">=", "lt": "<", "le": "<="}

// Validate applies defaults and rejects anything outside the catalog and cols,
// the calls relation's live columns. It must run before Compile.
func (q *Query) Validate(now time.Time, cols Columns) error {
	q.cols = cols
	if q.View == "" {
		q.View = viewCalls
	}
	if _, ok := queryViews[q.View]; !ok {
		return invalid("unknown view %q; valid: %s", q.View, keys(queryViews))
	}
	q.collectRefs()
	rows := len(q.Columns) > 0
	if rows {
		if len(q.Metrics) > 0 || len(q.Dimensions) > 0 || q.Granularity != "" || len(q.Having) > 0 || len(q.Formulas) > 0 {
			return invalid("columns lists %s, so leave metrics, dimensions, granularity, having and formulas empty", q.View)
		}
		if len(q.Columns) > maxRowColumns {
			return invalid("at most %d columns", maxRowColumns)
		}
		if hasDuplicates(q.Columns) {
			return invalid("columns must not repeat")
		}
		for _, c := range q.Columns {
			if _, err := q.field(c); err != nil {
				return err
			}
		}
	} else {
		if len(q.Metrics) == 0 {
			return invalid("at least one metric, or columns to list %s, is required; valid metrics for %s: %s", q.View, q.View, q.validMetrics())
		}
		for _, m := range q.Metrics {
			if _, _, err := q.metric(m); err != nil {
				return err
			}
		}
		if hasDuplicates(q.Metrics) || hasDuplicates(q.Dimensions) {
			return invalid("metrics and dimensions must not repeat")
		}
		if len(q.Dimensions) > 4 {
			return invalid("at most 4 dimensions")
		}
		for _, d := range q.Dimensions {
			if _, err := q.field(d); err != nil {
				return err
			}
		}
		if err := q.validateFormulas(); err != nil {
			return err
		}
		if err := q.validateHaving(); err != nil {
			return err
		}
	}
	if len(q.Filters) > maxFilters {
		return invalid("at most %d filters", maxFilters)
	}
	for _, f := range q.Filters {
		if len(f.Any) == 0 {
			if err := q.validateFilter(f); err != nil {
				return err
			}
			continue
		}
		if f.Field != "" || f.Op != "" || f.Value != "" || len(f.Values) > 0 {
			return invalid("a filter with any takes no field, op, value or values")
		}
		if len(f.Any) > maxFilters {
			return invalid("any takes at most %d filters", maxFilters)
		}
		for _, sub := range f.Any {
			if len(sub.Any) > 0 {
				return invalid("any filters do not nest")
			}
			if err := q.validateFilter(sub); err != nil {
				return err
			}
		}
	}
	if q.Granularity != "" {
		if _, ok := granularities[q.Granularity]; !ok {
			return invalid("unknown granularity %q; valid: hour, day", q.Granularity)
		}
	}

	if q.To.IsZero() || q.To.After(now) {
		q.To = now
	}
	if q.From.IsZero() {
		q.From = q.To.Add(-defaultQueryWindow)
	}
	if !q.From.Before(q.To) {
		return invalid("from must be before to")
	}
	if q.To.Sub(q.From) > maxQueryWindow {
		return invalid("the window must be at most 92 days")
	}

	switch q.Order {
	case "":
		q.Order = "desc"
		if q.Granularity != "" && q.OrderBy == "" {
			q.Order = "asc"
		}
	case "asc", "desc":
	default:
		return invalid("order must be asc or desc")
	}
	if rows {
		// Empty order_by lists rows by start time.
		if q.OrderBy != "" && !slices.Contains(q.Columns, q.OrderBy) {
			return invalid("order_by must be one of the columns")
		}
	} else {
		if q.OrderBy == "" {
			q.OrderBy = q.Metrics[0]
			if q.Granularity != "" {
				q.OrderBy = "time"
			}
		}
		if !slices.Contains(q.Metrics, q.OrderBy) && !slices.Contains(q.Dimensions, q.OrderBy) && q.formula(q.OrderBy) < 0 &&
			(q.OrderBy != "time" || q.Granularity == "") {
			return invalid("order_by must be one of the query's metrics, formulas or dimensions, or time with a granularity")
		}
	}

	limit, maxLimit := defaultQueryLimit, maxQueryLimit
	if rows {
		limit, maxLimit = defaultRowLimit, maxRowLimit
	}
	if q.Limit <= 0 {
		q.Limit = limit
	}
	if q.Limit > maxLimit {
		return invalid("limit must be at most %d", maxLimit)
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Page-1 > maxQueryOffset/q.Limit {
		return invalid("page reaches past row %d; narrow the query with filters or a shorter window", maxQueryOffset)
	}
	return nil
}

func (q Query) validateFilter(f QueryFilter) error {
	r, err := q.field(f.Field)
	if err != nil {
		return err
	}
	switch f.Op {
	case "eq", "neq", "contains":
		if len(f.Values) > 0 {
			return invalid("filter on %s: %s takes value, not values", f.Field, f.Op)
		}
	case "gt", "ge", "lt", "le":
		if r.kind != kindNumber {
			return invalid("filter on %s: %s needs a numeric field", f.Field, f.Op)
		}
		if _, err := strconv.ParseFloat(string(f.Value), 64); err != nil || len(f.Values) > 0 {
			return invalid("filter on %s: %s takes a numeric value", f.Field, f.Op)
		}
	case "is_empty", "not_empty":
		if f.Value != "" || len(f.Values) > 0 {
			return invalid("filter on %s: %s takes no value", f.Field, f.Op)
		}
	case "in", "not_in":
		if len(f.Values) == 0 || len(f.Values) > maxFilterValues {
			return invalid("filter on %s: %s takes 1-%d values", f.Field, f.Op, maxFilterValues)
		}
	default:
		return invalid("filter on %s: unknown op %q; valid: %s", f.Field, f.Op, strings.Join(filterOps, ", "))
	}
	return nil
}

func (q Query) validateFormulas() error {
	if len(q.Formulas) > maxFormulas {
		return invalid("at most %d formulas", maxFormulas)
	}
	for i, f := range q.Formulas {
		if !formulaNamePattern.MatchString(f.Name) {
			return invalid("formula name %q must be letters, digits and underscores", f.Name)
		}
		if f.Name == "time" || slices.Contains(q.Metrics, f.Name) || slices.Contains(q.Dimensions, f.Name) || q.formula(f.Name) != i {
			return invalid("formula name %q is already a column of the query", f.Name)
		}
		if _, err := q.formulaSQL(f.Expr); err != nil {
			return err
		}
	}
	return nil
}

func (q Query) validateHaving() error {
	if len(q.Having) > maxFilters {
		return invalid("at most %d having filters", maxFilters)
	}
	for _, h := range q.Having {
		if !slices.Contains(q.Metrics, h.Field) && q.formula(h.Field) < 0 {
			return invalid("having on %q: field must be one of the query's metrics or formulas", h.Field)
		}
		_, err := strconv.ParseFloat(string(h.Value), 64)
		if _, ok := havingOps[h.Op]; !ok || err != nil || len(h.Values) > 0 || len(h.Any) > 0 {
			return invalid("having on %s: op must be one of %s, with a numeric value", h.Field, keys(havingOps))
		}
	}
	return nil
}

// collectRefs numbers the run attributes and parent span fields the query
// reads. Invalid names are collected too; field rejects them.
func (q *Query) collectRefs() {
	q.runAttrs, q.parentCols = nil, nil
	for _, n := range q.fieldNames() {
		if q.View == viewCalls {
			if rest, ok := strings.CutPrefix(n, parentPrefix); ok {
				if !slices.Contains(q.parentCols, rest) {
					q.parentCols = append(q.parentCols, rest)
				}
				continue
			}
			var ok bool
			if n, ok = strings.CutPrefix(n, runPrefix); !ok {
				continue
			}
		}
		if key, ok := strings.CutPrefix(n, attrPrefix); ok && !slices.Contains(q.runAttrs, key) {
			q.runAttrs = append(q.runAttrs, key)
		}
	}
}

// fieldNames lists every field the query reads.
func (q Query) fieldNames() []string {
	names := slices.Concat(q.Dimensions, q.Columns)
	for _, f := range leaves(q.Filters) {
		names = append(names, f.Field)
	}
	for _, m := range q.Metrics {
		if _, name, ok := strings.Cut(m, ":"); ok {
			names = append(names, name)
		}
	}
	return names
}

func leaves(filters []QueryFilter) []QueryFilter {
	var out []QueryFilter
	for _, f := range filters {
		if len(f.Any) > 0 {
			out = append(out, f.Any...)
		} else {
			out = append(out, f)
		}
	}
	return out
}

// resolved is a validated field's SQL, its bound args and its kind.
type resolved struct {
	expr string
	args []any
	kind string
	join string
}

// field resolves a dimension, a view field, attr.<key>, and on the calls view
// a live column, run.<field> or parent.<column>. Curated dimensions win over a
// column of the same name.
func (q Query) field(name string) (resolved, error) {
	v := queryViews[q.View]
	if d, ok := v.dimensions[name]; ok {
		return resolved{expr: d.expr, kind: kindText, join: d.join}, nil
	}
	if f, ok := v.fields[name]; ok {
		return resolved{expr: f.expr, kind: f.kind}, nil
	}
	if q.View == viewRuns {
		if key, ok := strings.CutPrefix(name, attrPrefix); ok {
			if !attrKeyPattern.MatchString(key) {
				return resolved{}, invalid("invalid attribute key %q", key)
			}
			return resolved{expr: fmt.Sprintf("r_a%d", slices.Index(q.runAttrs, key)), kind: kindText}, nil
		}
	}
	if v.columns {
		if r, err := q.spanField(name); err == nil || strings.HasPrefix(name, attrPrefix) {
			return r, err
		}
		if rest, ok := strings.CutPrefix(name, runPrefix); ok {
			rq := q
			rq.View = viewRuns
			r, err := rq.field(rest)
			if err != nil {
				return resolved{}, invalid("unknown run field %q; valid: %s", rest, strings.Join(rq.validFields(), ", "))
			}
			r.expr, r.join = "tw."+r.expr, joinRun
			return r, nil
		}
		if rest, ok := strings.CutPrefix(name, parentPrefix); ok {
			r, err := q.spanField(rest)
			if err != nil {
				return resolved{}, invalid("unknown parent field %q; valid: a stored column or attr.<key>", rest)
			}
			return resolved{expr: fmt.Sprintf("pp.p_c%d", slices.Index(q.parentCols, rest)), kind: r.kind, join: joinParent}, nil
		}
	}
	return resolved{}, invalid("unknown field %q for view %s; valid: %s", name, q.View, strings.Join(q.validFields(), ", "))
}

// spanField resolves a live column of the calls relation or attr.<key>.
func (q Query) spanField(name string) (resolved, error) {
	if key, ok := strings.CutPrefix(name, attrPrefix); ok {
		if !attrKeyPattern.MatchString(key) {
			return resolved{}, invalid("invalid attribute key %q", key)
		}
		return resolved{expr: "attributes[?]", args: []any{key}, kind: kindText}, nil
	}
	if kind, ok := q.cols[name]; ok {
		return resolved{expr: name, kind: kind}, nil
	}
	return resolved{}, invalid("unknown column %q", name)
}

func (q Query) validFields() []string {
	v := queryViews[q.View]
	out := append(sortedKeys(v.dimensions), sortedKeys(v.fields)...)
	if v.columns {
		for _, c := range sortedKeys(q.cols) {
			if _, shadowed := v.dimensions[c]; !shadowed {
				out = append(out, c)
			}
		}
		return append(out, "attr.<key>", "run.<field>", "parent.<column>")
	}
	return append(out, "attr.<key>")
}

// metric resolves a named metric or <func>:<field> to its SQL and bound args.
func (q Query) metric(m string) (string, []any, error) {
	v := queryViews[q.View]
	if f, ok := v.metrics[m]; ok {
		return f.expr, nil, nil
	}
	fn, name, ok := strings.Cut(m, ":")
	spec, known := metricFuncs[fn]
	if !ok || !known {
		return "", nil, invalid("unknown metric %q for view %s; valid: %s", m, q.View, q.validMetrics())
	}
	r, err := q.field(name)
	if err != nil {
		return "", nil, err
	}
	if spec.numeric && r.kind != kindNumber {
		return "", nil, invalid("metric %s: %s needs a numeric field", m, fn)
	}
	return fmt.Sprintf(spec.sql, r.expr), r.args, nil
}

func (q Query) validMetrics() string {
	return keys(queryViews[q.View].metrics) + ", or <func>:<field> with func one of " + keys(metricFuncs)
}

func (q Query) formula(name string) int {
	return slices.IndexFunc(q.Formulas, func(f QueryFormula) bool { return f.Name == name })
}

// formulaSQL parses expr, arithmetic over the query's metrics and numbers, and
// renders it over the metric columns. Only metric columns, numbers parsed
// back from floats, operators and parentheses reach the SQL.
func (q Query) formulaSQL(expr string) (string, error) {
	fail := invalid("formula %q: use the query's metrics, numbers, + - * / and parentheses", expr)
	if len(expr) > maxFormulaChars {
		return "", fail
	}
	// Longest names first, so a metric that prefixes another does not match it.
	names := slices.Clone(q.Metrics)
	slices.SortFunc(names, func(a, b string) int { return len(b) - len(a) })
	var p formulaParser
	for s := strings.TrimSpace(expr); s != ""; s = strings.TrimSpace(s) {
		if strings.ContainsRune("+-*/()", rune(s[0])) {
			p.toks, s = append(p.toks, s[:1]), s[1:]
			continue
		}
		if i := slices.IndexFunc(names, func(n string) bool { return strings.HasPrefix(s, n) }); i >= 0 {
			p.toks = append(p.toks, fmt.Sprintf("q_m%d", slices.Index(q.Metrics, names[i])))
			s = s[len(names[i]):]
			continue
		}
		n := len(s) - len(strings.TrimLeft(s, "0123456789."))
		f, err := strconv.ParseFloat(s[:n], 64)
		if n == 0 || err != nil {
			return "", fail
		}
		p.toks, s = append(p.toks, strconv.FormatFloat(f, 'g', -1, 64)), s[n:]
	}
	sql, ok := p.sum()
	if !ok || p.i != len(p.toks) {
		return "", fail
	}
	return sql, nil
}

type formulaParser struct {
	toks []string
	i    int
}

func (p *formulaParser) peek() string {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return ""
}

func (p *formulaParser) next() string {
	t := p.peek()
	p.i++
	return t
}

func (p *formulaParser) sum() (string, bool) {
	l, ok := p.product()
	for ok && (p.peek() == "+" || p.peek() == "-") {
		op := p.next()
		var r string
		r, ok = p.product()
		l += " " + op + " " + r
	}
	return l, ok
}

func (p *formulaParser) product() (string, bool) {
	l, ok := p.factor()
	for ok && (p.peek() == "*" || p.peek() == "/") {
		op := p.next()
		var r string
		r, ok = p.factor()
		l += " " + op + " " + r
	}
	return l, ok
}

func (p *formulaParser) factor() (string, bool) {
	switch t := p.next(); t {
	case "-":
		// Parenthesized, so two minuses never render as a SQL comment.
		f, ok := p.factor()
		return "(-" + f + ")", ok
	case "(":
		e, ok := p.sum()
		return "(" + e + ")", ok && p.next() == ")"
	case "", "+", "*", "/", ")":
		return "", false
	default:
		return t, true
	}
}

// compiled is a query ready to run, with the columns it returns in order.
type compiled struct {
	sql        string
	args       []any
	out        []outColumn
	reconciled bool
}

type outColumn struct{ name, kind string }

// Compile renders a validated query. The workspace scope from window() bounds
// every relation the SQL reads, including the joins and the cost union.
func (q Query) Compile() compiled {
	lo, hi := q.From.UnixMilli(), q.To.UnixMilli()
	clause, wargs := window("", q.WorkspaceIDs, lo, hi)

	var c compiled
	var sel []string
	var selArgs []any
	var group []string
	for i, name := range q.Columns {
		r, _ := q.field(name)
		if r.kind == kindNumber {
			sel = append(sel, fmt.Sprintf("toFloat64(%s) AS q_c%d", r.expr, i))
		} else {
			// One extra character tells the scan the text was clipped.
			sel = append(sel, fmt.Sprintf("leftUTF8(toString(%s), %d) AS q_c%d", r.expr, rowTextChars+1, i))
			r.kind = kindText
		}
		selArgs = append(selArgs, r.args...)
		c.out = append(c.out, outColumn{name, r.kind})
	}
	for i, d := range q.Dimensions {
		r, _ := q.field(d)
		sel = append(sel, fmt.Sprintf("toString(%s) AS q_d%d", r.expr, i))
		selArgs = append(selArgs, r.args...)
		group = append(group, fmt.Sprintf("q_d%d", i))
		c.out = append(c.out, outColumn{d, kindText})
	}
	timeCol := "start_time_ms"
	if q.View == viewRuns {
		timeCol = "r_start"
	}
	if g := granularities[q.Granularity]; g > 0 {
		sel = append(sel, fmt.Sprintf("toInt64(intDiv(%s, ?) * ?) AS q_time", timeCol))
		selArgs = append(selArgs, g.Milliseconds(), g.Milliseconds())
		group = append(group, "q_time")
		c.out = append(c.out, outColumn{"time", kindTime})
	}
	for i, m := range q.Metrics {
		expr, args, _ := q.metric(m)
		sel = append(sel, fmt.Sprintf("toFloat64(%s) AS q_m%d", expr, i))
		selArgs = append(selArgs, args...)
		c.out = append(c.out, outColumn{m, kindNumber})
	}
	for _, f := range q.Formulas {
		c.out = append(c.out, outColumn{f.Name, kindNumber})
	}

	var where []string
	var whereArgs []any
	for _, f := range q.Filters {
		cond, args := q.filterSQL(f)
		where = append(where, cond)
		whereArgs = append(whereArgs, args...)
	}

	var from string
	var fromArgs []any
	if q.View == viewRuns {
		from, fromArgs = q.runsFrom(clause, wargs)
	} else {
		from = tableCalls
		if q.needsJoin(joinRun) {
			sub, args := q.runsFrom(clause, wargs)
			from += "\nANY LEFT JOIN " + sub + " AS tw USING (trace_id)"
			fromArgs = append(fromArgs, args...)
		}
		if q.needsJoin(joinParent) {
			sub, args := q.parentFrom(clause, wargs)
			from += "\nANY LEFT JOIN " + sub + " AS pp ON trace_id = pp.p_trace AND parent_span_id = pp.p_span"
			fromArgs = append(fromArgs, args...)
		}
		where = append([]string{clause}, where...)
		whereArgs = append(slices.Clone(wargs), whereArgs...)
	}

	sql := "SELECT " + strings.Join(sel, ", ") + "\nFROM " + from
	if len(where) > 0 {
		sql += "\nWHERE " + strings.Join(where, " AND ")
	}
	if len(group) > 0 {
		sql += "\nGROUP BY " + strings.Join(group, ", ")
	}
	args := append(append(selArgs, fromArgs...), whereArgs...)

	if q.reconciles() {
		sql, args = q.withReconciledCost(sql, args, group, clause, wargs)
		c.reconciled = true
	}
	if len(q.Formulas) > 0 || len(q.Having) > 0 {
		sql, args = q.withFormulas(sql, args)
	}
	c.sql = sql + "\nORDER BY " + q.orderSQL() + "\nLIMIT ? OFFSET ?"
	c.args = append(args, q.Limit+1, (q.Page-1)*q.Limit)
	return c
}

// runsFrom renders the per-run subquery, with a column per run attribute read:
// the first non-empty value among the run's calls.
func (q Query) runsFrom(clause string, wargs []any) (string, []any) {
	var extra string
	var args []any
	for i, key := range q.runAttrs {
		extra += fmt.Sprintf(",\n        argMinIf(attributes[?], start_time_ms, attributes[?] != '') AS r_a%d", i)
		args = append(args, key, key)
	}
	return "(" + fmt.Sprintf(runsSubquery, extra, clause) + "\n)", append(args, wargs...)
}

// parentFrom renders the spans a call's parent is looked up in, with a column
// per parent field read. Only spans some call names as parent are loaded, to
// keep the join's in-memory side small. A parent outside the window is not found.
func (q Query) parentFrom(clause string, wargs []any) (string, []any) {
	sel := []string{"trace_id AS p_trace", "span_id AS p_span"}
	var args []any
	for i, name := range q.parentCols {
		r, _ := q.spanField(name)
		sel = append(sel, fmt.Sprintf("%s AS p_c%d", r.expr, i))
		args = append(args, r.args...)
	}
	sub := fmt.Sprintf("(\n    SELECT %s\n    FROM %s\n    WHERE %s AND span_id IN (SELECT parent_span_id FROM %s WHERE %s)\n)",
		strings.Join(sel, ", "), tableCalls, clause, tableCalls, clause)
	return sub, slices.Concat(args, wargs, wargs)
}

// needsJoin reports whether any field the query reads needs the given join.
func (q Query) needsJoin(join string) bool {
	for _, n := range q.fieldNames() {
		if r, err := q.field(n); err == nil && r.join == join {
			return true
		}
	}
	return false
}

func (q Query) reconciles() bool {
	if q.View != viewCalls || !slices.Contains(q.Metrics, "cost") {
		return false
	}
	for _, d := range q.Dimensions {
		if !reconcilable[d] {
			return false
		}
	}
	for _, f := range leaves(q.Filters) {
		if !reconcilable[f.Field] {
			return false
		}
	}
	return true
}

// withReconciledCost swaps span cost for the dashboard's reconciled cost: span
// and metric cost are reconciled per bucket (costReconcileExpr) on the same
// grid the dashboard uses, then summed per group.
// shortcut: a group whose only rows are usage metrics (no calls) is dropped by
// the LEFT JOIN; drive the join from the cost side if metric-only users matter.
func (q Query) withReconciledCost(main string, mainArgs []any, group []string, clause string, wargs []any) (string, []any) {
	recon := time.Hour
	if q.Granularity == "day" || (q.Granularity == "" && q.To.Sub(q.From) > 24*time.Hour) {
		recon = 24 * time.Hour
	}
	var dimCols, costSel []string
	var costSelArgs []any
	for i, d := range q.Dimensions {
		r, _ := q.field(d)
		dimCols = append(dimCols, fmt.Sprintf("toString(%s) AS q_d%d", r.expr, i))
		costSel = append(costSel, fmt.Sprintf("q_d%d", i))
	}
	inner := append(slices.Clone(costSel), "intDiv(start_time_ms, ?) AS q_rb")
	if g := granularities[q.Granularity]; g > 0 {
		costSel = append(costSel, "toInt64(intDiv(q_rb * ?, ?) * ?) AS q_time")
		costSelArgs = append(costSelArgs, recon.Milliseconds(), g.Milliseconds(), g.Milliseconds())
	}
	// Filters here are on reconcilable dimensions only, which every cost relation carries.
	var fargs []any
	for _, f := range q.Filters {
		cond, args := q.filterSQL(f)
		clause += " AND " + cond
		fargs = append(fargs, args...)
	}
	union, uargs := reconciledCostUnion(dimCols, clause, append(slices.Clone(wargs), fargs...))
	cost := fmt.Sprintf(`SELECT %s
    FROM (
        SELECT %s, %s AS q_bucket_cost
        FROM (%s)
        GROUP BY %s
    )`, strings.Join(append(costSel, "sum(q_bucket_cost) AS q_rcost"), ", "),
		strings.Join(inner, ", "), costReconcileExpr, union,
		strings.Join(append(slices.Clone(group[:len(q.Dimensions)]), "q_rb"), ", "))
	if len(group) > 0 {
		cost += "\n    GROUP BY " + strings.Join(group, ", ")
	}

	var outer []string
	for _, g := range group {
		outer = append(outer, "q_main."+g+" AS "+g)
	}
	for i, m := range q.Metrics {
		if m == "cost" {
			outer = append(outer, fmt.Sprintf("q_cost.q_rcost AS q_m%d", i))
		} else {
			outer = append(outer, fmt.Sprintf("q_main.q_m%d AS q_m%d", i, i))
		}
	}
	join := "CROSS JOIN"
	if len(group) > 0 {
		join = "LEFT JOIN"
	}
	sql := fmt.Sprintf("SELECT %s\nFROM (%s) AS q_main\n%s (%s) AS q_cost", strings.Join(outer, ", "), main, join, cost)
	if len(group) > 0 {
		sql += " USING (" + strings.Join(group, ", ") + ")"
	}
	// Args follow the SQL text: main, then the cost subquery's select, inner
	// select, and the two union branches.
	args := append(slices.Clone(mainArgs), costSelArgs...)
	args = append(args, recon.Milliseconds())
	return sql, append(args, uargs...)
}

// withFormulas wraps the aggregated rows to add the formulas and apply having,
// after cost is reconciled.
func (q Query) withFormulas(inner string, args []any) (string, []any) {
	sel := []string{"*"}
	for i, f := range q.Formulas {
		expr, _ := q.formulaSQL(f.Expr)
		sel = append(sel, fmt.Sprintf("toFloat64(%s) AS q_f%d", expr, i))
	}
	sql := "SELECT " + strings.Join(sel, ", ") + "\nFROM (" + inner + ")"
	var conds []string
	args = slices.Clone(args)
	for _, h := range q.Having {
		n, _ := strconv.ParseFloat(string(h.Value), 64)
		conds = append(conds, fmt.Sprintf("%s %s ?", q.outputColumn(h.Field), havingOps[h.Op]))
		args = append(args, n)
	}
	if len(conds) > 0 {
		sql += "\nWHERE " + strings.Join(conds, " AND ")
	}
	return sql, args
}

// outputColumn names the aggregated column holding a dimension, metric,
// formula or time.
func (q Query) outputColumn(name string) string {
	if name == "time" {
		return "q_time"
	}
	if i := slices.Index(q.Dimensions, name); i >= 0 {
		return fmt.Sprintf("q_d%d", i)
	}
	if i := q.formula(name); i >= 0 {
		return fmt.Sprintf("q_f%d", i)
	}
	return fmt.Sprintf("q_m%d", slices.Index(q.Metrics, name))
}

func (q Query) orderSQL() string {
	dir := strings.ToUpper(q.Order)
	if len(q.Columns) > 0 {
		// The tiebreak keeps pages from overlapping.
		start, tiebreak := "start_time_ms", ", trace_id ASC, span_id ASC"
		if q.View == viewRuns {
			start, tiebreak = "r_start", ", trace_id ASC"
		}
		if i := slices.Index(q.Columns, q.OrderBy); i >= 0 {
			return fmt.Sprintf("q_c%d %s, %s DESC", i, dir, start) + tiebreak
		}
		return start + " " + dir + tiebreak
	}
	parts := []string{q.outputColumn(q.OrderBy) + " " + dir}
	if q.Granularity != "" && q.OrderBy != "time" {
		parts = append(parts, "q_time ASC")
	}
	// A stable tiebreak keeps LIMIT deterministic.
	for i := range q.Dimensions {
		parts = append(parts, fmt.Sprintf("q_d%d ASC", i))
	}
	return strings.Join(parts, ", ")
}

// filterSQL renders a filter and its args, in the order its placeholders appear.
func (q Query) filterSQL(f QueryFilter) (string, []any) {
	if len(f.Any) > 0 {
		var conds []string
		var args []any
		for _, sub := range f.Any {
			cond, a := q.filterSQL(sub)
			conds = append(conds, cond)
			args = append(args, a...)
		}
		return "(" + strings.Join(conds, " OR ") + ")", args
	}
	r, _ := q.field(f.Field)
	expr := r.expr
	args := slices.Clone(r.args)
	switch f.Op {
	case "eq":
		return fmt.Sprintf("toString(%s) = ?", expr), append(args, string(f.Value))
	case "neq":
		return fmt.Sprintf("toString(%s) != ?", expr), append(args, string(f.Value))
	case "contains":
		return fmt.Sprintf("positionCaseInsensitive(toString(%s), ?) > 0", expr), append(args, string(f.Value))
	case "gt", "ge", "lt", "le":
		n, _ := strconv.ParseFloat(string(f.Value), 64)
		return fmt.Sprintf("%s %s ?", expr, comparisons[f.Op]), append(args, n)
	case "is_empty":
		return fmt.Sprintf("toString(%s) = ''", expr), args
	case "not_empty":
		return fmt.Sprintf("toString(%s) != ''", expr), args
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(f.Values)), ",")
	for _, s := range f.Values {
		args = append(args, s)
	}
	not := ""
	if f.Op == "not_in" {
		not = "NOT "
	}
	return fmt.Sprintf("toString(%s) %sIN (%s)", expr, not, ph), args
}

// QueryColumns reads the calls relation's columns from ClickHouse, keeping the
// ones whose type a query can compare or aggregate. attributes is reached
// through attr.<key> instead.
func (r *ClickHouseRepository) QueryColumns(ctx context.Context) (Columns, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	db, table, _ := strings.Cut(tableCalls, ".")
	rows, err := r.db.QueryContext(ctx, `SELECT name, type FROM system.columns WHERE database = ? AND table = ?`, db, table)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: query columns: %w", err)
	}
	defer rows.Close()

	cols := Columns{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, fmt.Errorf("clickhouse: scan query column: %w", err)
		}
		if kind := columnKind(typ); kind != "" && columnPattern.MatchString(name) {
			cols[name] = kind
		}
	}
	return cols, rows.Err()
}

// columnKind maps a ClickHouse type to number or text; "" means unsupported
// (maps, arrays, aggregate states).
func columnKind(typ string) string {
	for _, wrap := range []string{"LowCardinality(", "Nullable("} {
		if inner, ok := strings.CutPrefix(typ, wrap); ok {
			typ = strings.TrimSuffix(inner, ")")
		}
	}
	for _, p := range []string{"Int", "UInt", "Float", "Decimal", "Bool"} {
		if strings.HasPrefix(typ, p) {
			return kindNumber
		}
	}
	for _, p := range []string{"String", "FixedString", "Enum", "UUID", "Date"} {
		if strings.HasPrefix(typ, p) {
			return kindText
		}
	}
	return ""
}

// RunQuery validates nothing: callers Validate first. It runs the compiled
// query under queryTimeout with ClickHouse's memory cap.
func (r *ClickHouseRepository) RunQuery(ctx context.Context, q Query) (model.QueryResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	select {
	case querySlots <- struct{}{}:
		defer func() { <-querySlots }()
	case <-ctx.Done():
		return model.QueryResult{}, fmt.Errorf("clickhouse: custom query: %w", ctx.Err())
	}

	c := q.Compile()
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_memory_usage": 2 << 30,
	}))
	rows, err := r.db.QueryContext(ctx, c.sql, c.args...)
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("clickhouse: custom query: %w", err)
	}
	defer rows.Close()

	res := model.QueryResult{Columns: []string{}, Rows: []map[string]any{}}
	dest := make([]any, len(c.out))
	for i, oc := range c.out {
		res.Columns = append(res.Columns, oc.name)
		switch oc.kind {
		case kindNumber:
			dest[i] = new(float64)
		case kindTime:
			dest[i] = new(int64)
		default:
			dest[i] = new(string)
		}
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return model.QueryResult{}, fmt.Errorf("clickhouse: scan custom query: %w", err)
		}
		if len(res.Rows) == q.Limit {
			res.Meta.Truncated = true
			break
		}
		row := make(map[string]any, len(c.out))
		for i, oc := range c.out {
			switch v := dest[i].(type) {
			case *float64:
				row[oc.name] = sanitize(*v)
			case *int64:
				row[oc.name] = time.UnixMilli(*v).UTC().Format(time.RFC3339)
			case *string:
				row[oc.name] = ClipText(*v)
			}
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return model.QueryResult{}, err
	}

	res.Meta.View = q.View
	res.Meta.From = q.From.UTC().Format(time.RFC3339)
	res.Meta.To = q.To.UTC().Format(time.RFC3339)
	res.Meta.Granularity = q.Granularity
	res.Meta.Page = q.Page
	if res.Meta.Truncated {
		res.Meta.NextPage = q.Page + 1
	}
	if slices.Contains(q.Metrics, "cost") {
		res.Meta.CostSource = "spans"
		if c.reconciled {
			res.Meta.CostSource = "reconciled"
		}
	}
	return res, nil
}

// ClipText cuts s to the characters a query row or MCP span may carry.
func ClipText(s string) string {
	if utf8.RuneCountInString(s) > rowTextChars {
		return string([]rune(s)[:rowTextChars]) + "…[truncated]"
	}
	return s
}

// QueryCatalog describes every view, field, metric, filter op and limit, for
// the schema endpoint and the MCP schema tool to extend with live values.
func QueryCatalog(cols Columns) model.QuerySchema {
	views := make([]model.QueryView, 0, len(queryViews))
	for _, name := range sortedKeys(queryViews) {
		v := queryViews[name]
		out := model.QueryView{Name: name, Description: v.doc, Columns: []model.QueryColumn{}, Related: []model.QueryField{}}
		for _, d := range sortedKeys(v.dimensions) {
			out.Dimensions = append(out.Dimensions, model.QueryField{Name: d, Description: v.dimensions[d].doc})
		}
		for _, m := range sortedKeys(v.metrics) {
			out.Metrics = append(out.Metrics, model.QueryField{Name: m, Description: v.metrics[m].doc})
		}
		for _, f := range sortedKeys(v.fields) {
			out.Columns = append(out.Columns, model.QueryColumn{Name: f, Kind: v.fields[f].kind, Description: v.fields[f].doc})
		}
		if v.columns {
			for _, c := range sortedKeys(cols) {
				if _, shadowed := v.dimensions[c]; !shadowed {
					out.Columns = append(out.Columns, model.QueryColumn{Name: c, Kind: cols[c]})
				}
			}
			out.Related = relatedFields
		}
		views = append(views, out)
	}
	return model.QuerySchema{
		Views:           views,
		MetricFunctions: sortedKeys(metricFuncs),
		FilterOps:       filterOps,
		HavingOps:       sortedKeys(havingOps),
		Granularities:   sortedKeys(granularities),
		MaxWindowDays:   int(maxQueryWindow / (24 * time.Hour)),
		MaxLimit:        maxQueryLimit,
		MaxRowLimit:     maxRowLimit,
	}
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, fmt.Sprintf(format, a...))
}

func keys[V any](m map[string]V) string { return strings.Join(sortedKeys(m), ", ") }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func hasDuplicates(s []string) bool {
	seen := map[string]bool{}
	for _, v := range s {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}
