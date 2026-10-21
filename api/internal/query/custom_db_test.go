package query

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

type querySpan struct {
	trace, parent, model, user, workspace, workflow, errorType, source string
	cost                                                               float64
	attrs                                                              map[string]string
}

func seedQuerySpans(t *testing.T, r *ClickHouseRepository, at int64, spans []querySpan) {
	t.Helper()
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO tracium.spans (trace_id, span_id, parent_span_id, name, start_time_ms, end_time_ms,
    duration_ms, model, cost_usd, input_tokens, output_tokens, user_id, workspace_id, workflow_name, error_type, source, attributes)`)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range spans {
		if s.attrs == nil {
			s.attrs = map[string]string{}
		}
		start := at + int64(i)
		if _, err := stmt.Exec(s.trace, fmt.Sprint(i), s.parent, "call", start, start+100, int64(100), s.model, s.cost,
			int64(10), int64(1), s.user, s.workspace, s.workflow, s.errorType, s.source, s.attrs); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestRunQueryFromClickHouse(t *testing.T) {
	r, ws := newTestRepository(t)
	other := ws + "-other"
	now := time.Now().UTC()
	at := now.Truncate(24 * time.Hour).Add(-12 * time.Hour).UnixMilli()
	seedQuerySpans(t, r, at, []querySpan{
		{trace: ws + "-a", model: "gpt-4o", user: "u1", workspace: ws, workflow: "checkout", source: "span", cost: 1, attrs: map[string]string{"team": "core"}},
		{trace: ws + "-a", parent: "0", model: "claude", user: "u1", workspace: ws, errorType: "rate_limit", source: "span", cost: 0.5},
		{trace: ws + "-b", model: "gpt-4o", user: "u2", workspace: ws, workflow: "search", source: "span", cost: 2, attrs: map[string]string{"team": "growth"}},
		{model: "gpt-4o", user: "u1", workspace: ws, source: "metric", cost: 5},
		{trace: other + "-c", model: "gpt-4o", user: "u1", workspace: other, workflow: "checkout", source: "span", cost: 100},
	})

	run := func(t *testing.T, q Query) (rows []map[string]any, costSource string, truncated bool) {
		t.Helper()
		if q.WorkspaceIDs == nil {
			q.WorkspaceIDs = []string{ws}
		}
		q.From = time.UnixMilli(at).Add(-time.Hour)
		cols, err := r.QueryColumns(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Validate(now, cols); err != nil {
			t.Fatal(err)
		}
		res, err := r.RunQuery(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		return res.Rows, res.Meta.CostSource, res.Meta.Truncated
	}
	same := func(t *testing.T, got []map[string]any, want ...map[string]any) {
		t.Helper()
		if want == nil {
			want = []map[string]any{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("rows = %v\nwant   %v", got, want)
		}
	}

	t.Run("by model uses span cost", func(t *testing.T) {
		rows, src, _ := run(t, Query{Metrics: []string{"cost", "calls", "errors"}, Dimensions: []string{"model"}})
		same(t, rows,
			map[string]any{"model": "gpt-4o", "cost": 3.0, "calls": 2.0, "errors": 0.0},
			map[string]any{"model": "claude", "cost": 0.5, "calls": 1.0, "errors": 1.0})
		if src != "spans" {
			t.Fatalf("cost_source = %q", src)
		}
	})
	t.Run("total reconciles with usage metrics", func(t *testing.T) {
		rows, src, _ := run(t, Query{Metrics: []string{"cost", "calls"}})
		same(t, rows, map[string]any{"cost": 5.0, "calls": 3.0})
		if src != "reconciled" {
			t.Fatalf("cost_source = %q", src)
		}
	})
	t.Run("by user reconciles per user", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"user"}})
		same(t, rows, map[string]any{"user": "u1", "cost": 5.0}, map[string]any{"user": "u2", "cost": 2.0})
	})
	t.Run("filtered reconciled cost", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "user", Op: "eq", Value: "u2"}}})
		same(t, rows, map[string]any{"cost": 2.0})
	})
	t.Run("daily reconciled series", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Granularity: "day"})
		day := time.UnixMilli(at).UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
		same(t, rows, map[string]any{"time": day, "cost": 5.0})
	})
	t.Run("runs by workflow", func(t *testing.T) {
		rows, _, _ := run(t, Query{View: "runs", Metrics: []string{"runs", "error_rate", "cost"}, Dimensions: []string{"workflow"}})
		same(t, rows,
			map[string]any{"workflow": "checkout", "runs": 1.0, "error_rate": 1.0, "cost": 1.5},
			map[string]any{"workflow": "search", "runs": 1.0, "error_rate": 0.0, "cost": 2.0})
	})
	t.Run("calls by workflow and model", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"workflow", "model"}})
		same(t, rows,
			map[string]any{"workflow": "search", "model": "gpt-4o", "cost": 2.0},
			map[string]any{"workflow": "checkout", "model": "gpt-4o", "cost": 1.0},
			map[string]any{"workflow": "checkout", "model": "claude", "cost": 0.5})
	})
	t.Run("custom attribute", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"attr.team"},
			Filters: []QueryFilter{{Field: "attr.team", Op: "neq", Value: ""}}})
		same(t, rows, map[string]any{"attr.team": "growth", "cost": 2.0}, map[string]any{"attr.team": "core", "cost": 1.0})
	})
	t.Run("lists calls by any column", func(t *testing.T) {
		rows, _, _ := run(t, Query{Columns: []string{"model", "cost_usd", "attr.team"},
			Filters: []QueryFilter{{Field: "cost_usd", Op: "gt", Value: "0.75"}}, OrderBy: "cost_usd"})
		same(t, rows,
			map[string]any{"model": "gpt-4o", "cost_usd": 2.0, "attr.team": "growth"},
			map[string]any{"model": "gpt-4o", "cost_usd": 1.0, "attr.team": "core"})
	})
	t.Run("lists calls with their run's workflow", func(t *testing.T) {
		rows, _, _ := run(t, Query{Columns: []string{"workflow", "error_type"}, Filters: []QueryFilter{{Field: "error_type", Op: "not_empty"}}})
		same(t, rows, map[string]any{"workflow": "checkout", "error_type": "rate_limit"})
	})
	t.Run("metric functions on columns", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"max:cost_usd", "uniq:attr.team"}, Dimensions: []string{"model"}})
		same(t, rows,
			map[string]any{"model": "gpt-4o", "max:cost_usd": 2.0, "uniq:attr.team": 2.0},
			map[string]any{"model": "claude", "max:cost_usd": 0.5, "uniq:attr.team": 1.0})
	})
	t.Run("lists runs filtered on their totals", func(t *testing.T) {
		rows, _, _ := run(t, Query{View: "runs", Columns: []string{"workflow", "cost_usd", "errored"}, OrderBy: "cost_usd"})
		same(t, rows,
			map[string]any{"workflow": "search", "cost_usd": 2.0, "errored": 0.0},
			map[string]any{"workflow": "checkout", "cost_usd": 1.5, "errored": 1.0})
		rows, _, _ = run(t, Query{View: "runs", Columns: []string{"workflow"}, Filters: []QueryFilter{{Field: "cost_usd", Op: "gt", Value: "1.75"}}})
		same(t, rows, map[string]any{"workflow": "search"})
	})
	t.Run("runs by attribute", func(t *testing.T) {
		rows, _, _ := run(t, Query{View: "runs", Metrics: []string{"cost", "max:duration_ms"}, Dimensions: []string{"attr.team"}})
		same(t, rows,
			map[string]any{"attr.team": "growth", "cost": 2.0, "max:duration_ms": 100.0},
			map[string]any{"attr.team": "core", "cost": 1.5, "max:duration_ms": 101.0})
	})
	t.Run("calls in failed runs", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"calls"}, Dimensions: []string{"model", "run.attr.team"},
			Filters: []QueryFilter{{Field: "run.errored", Op: "eq", Value: "1"}}})
		same(t, rows,
			map[string]any{"model": "claude", "run.attr.team": "core", "calls": 1.0},
			map[string]any{"model": "gpt-4o", "run.attr.team": "core", "calls": 1.0})
	})
	t.Run("calls with their parent", func(t *testing.T) {
		rows, _, _ := run(t, Query{Columns: []string{"model", "parent.model", "parent.attr.team"},
			Filters: []QueryFilter{{Field: "parent.model", Op: "not_empty"}}})
		same(t, rows, map[string]any{"model": "claude", "parent.model": "gpt-4o", "parent.attr.team": "core"})
	})
	t.Run("any filter", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"model"},
			Filters: []QueryFilter{{Any: []QueryFilter{{Field: "model", Op: "eq", Value: "claude"}, {Field: "attr.team", Op: "eq", Value: "growth"}}}}})
		same(t, rows, map[string]any{"model": "gpt-4o", "cost": 2.0}, map[string]any{"model": "claude", "cost": 0.5})
	})
	t.Run("formula and having", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost", "calls"}, Dimensions: []string{"model"},
			Formulas: []QueryFormula{{Name: "per_call", Expr: "cost / calls"}},
			Having:   []QueryFilter{{Field: "per_call", Op: "gt", Value: "1"}}})
		same(t, rows, map[string]any{"model": "gpt-4o", "cost": 3.0, "calls": 2.0, "per_call": 1.5})
	})
	t.Run("formula over reconciled cost", func(t *testing.T) {
		rows, src, _ := run(t, Query{Metrics: []string{"cost", "calls"},
			Formulas: []QueryFormula{{Name: "per_call", Expr: "cost / calls"}},
			Having:   []QueryFilter{{Field: "cost", Op: "ge", Value: "5"}}})
		same(t, rows, map[string]any{"cost": 5.0, "calls": 3.0, "per_call": 5.0 / 3})
		if src != "reconciled" {
			t.Fatalf("cost_source = %q", src)
		}
	})
	t.Run("pages through calls without overlap", func(t *testing.T) {
		var seen []any
		for page := 1; page <= 4; page++ {
			res, err := func() (model.QueryResult, error) {
				q := Query{Columns: []string{"cost_usd"}, OrderBy: "cost_usd", Limit: 1, Page: page, WorkspaceIDs: []string{ws}, From: time.UnixMilli(at).Add(-time.Hour)}
				cols, _ := r.QueryColumns(context.Background())
				if err := q.Validate(now, cols); err != nil {
					return model.QueryResult{}, err
				}
				return r.RunQuery(context.Background(), q)
			}()
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range res.Rows {
				seen = append(seen, row["cost_usd"])
			}
			if wantNext := page < 3; (res.Meta.NextPage == page+1) != wantNext || res.Meta.Page != page {
				t.Fatalf("page %d meta = %+v", page, res.Meta)
			}
		}
		if !reflect.DeepEqual(seen, []any{2.0, 1.0, 0.5}) {
			t.Fatalf("pages returned %v", seen)
		}
	})
	t.Run("truncates at the limit", func(t *testing.T) {
		rows, _, truncated := run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"model"}, Limit: 1})
		if len(rows) != 1 || !truncated {
			t.Fatalf("rows = %v truncated = %v", rows, truncated)
		}
	})
	t.Run("other workspace stays invisible", func(t *testing.T) {
		rows, _, _ := run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"workflow"}, WorkspaceIDs: []string{other}})
		same(t, rows, map[string]any{"workflow": "checkout", "cost": 100.0})
		rows, _, _ = run(t, Query{Metrics: []string{"cost"}, Dimensions: []string{"model"}, WorkspaceIDs: []string{}})
		same(t, rows)
	})
}
