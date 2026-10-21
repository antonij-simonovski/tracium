package query

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var testColumns = Columns{
	"trace_id": kindText, "model": kindText, "kind": kindText, "error_message": kindText, "input": kindText,
	"cost_usd": kindNumber, "input_tokens": kindNumber,
}

func TestQueryValidate(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for name, tt := range map[string]struct {
		q    Query
		want string
	}{
		"no metrics":          {Query{}, "at least one metric"},
		"unknown view":        {Query{View: "spans", Metrics: []string{"cost"}}, `unknown view "spans"`},
		"unknown metric":      {Query{Metrics: []string{"price"}}, "valid: calls, cost"},
		"runs-only dim":       {Query{Metrics: []string{"cost"}, Dimensions: []string{"model"}, View: "runs"}, `unknown field "model" for view runs`},
		"attr injection runs": {Query{View: "runs", Metrics: []string{"runs"}, Dimensions: []string{"attr.x'] --"}}, "invalid attribute key"},
		"attr injection":      {Query{Metrics: []string{"cost"}, Dimensions: []string{"attr.x'] OR 1=1 --"}}, "invalid attribute key"},
		"bad op":              {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "model", Op: "like", Value: "x"}}}, `unknown op "like"`},
		"in without values":   {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "model", Op: "in"}}}, "takes 1-100 values"},
		"window too long":     {Query{Metrics: []string{"cost"}, From: now.Add(-100 * 24 * time.Hour)}, "at most 92 days"},
		"from after to":       {Query{Metrics: []string{"cost"}, From: now.Add(-time.Hour), To: now.Add(-2 * time.Hour)}, "from must be before to"},
		"order by unknown":    {Query{Metrics: []string{"cost"}, OrderBy: "calls"}, "order_by must be"},
		"time without grain":  {Query{Metrics: []string{"cost"}, OrderBy: "time"}, "order_by must be"},
		"limit too high":      {Query{Metrics: []string{"cost"}, Limit: 5000}, "at most 1000"},
		"page overflows":      {Query{Metrics: []string{"cost"}, Limit: 2, Page: 1 << 62}, "page reaches past"},
		"repeated metric":     {Query{Metrics: []string{"cost", "cost"}}, "must not repeat"},
		"unknown column":      {Query{Metrics: []string{"cost"}, Dimensions: []string{"secret"}}, "valid: error_type"},
		"func on text":        {Query{Metrics: []string{"sum:error_message"}}, "needs a numeric field"},
		"unknown func":        {Query{Metrics: []string{"median:cost_usd"}}, "func one of avg"},
		"func on runs":        {Query{View: "runs", Metrics: []string{"max:r_cost"}}, `unknown field "r_cost" for view runs`},
		"run column on runs":  {Query{View: "runs", Metrics: []string{"runs"}, Dimensions: []string{"model"}}, `unknown field "model"`},
		"unknown run field":   {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "run.model", Op: "eq", Value: "x"}}}, `unknown run field "model"`},
		"nested run":          {Query{Metrics: []string{"cost"}, Dimensions: []string{"run.run.cost_usd"}}, `unknown run field`},
		"unknown parent":      {Query{Metrics: []string{"cost"}, Dimensions: []string{"parent.workflow"}}, `unknown parent field "workflow"`},
		"parent on runs":      {Query{View: "runs", Metrics: []string{"runs"}, Dimensions: []string{"parent.model"}}, `unknown field "parent.model"`},
		"any with field":      {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "model", Op: "eq", Any: []QueryFilter{{Field: "model", Op: "eq"}}}}}, "takes no field"},
		"nested any":          {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Any: []QueryFilter{{Any: []QueryFilter{{Field: "model", Op: "eq"}}}}}}}, "do not nest"},
		"bad any leaf":        {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Any: []QueryFilter{{Field: "model", Op: "gt", Value: "1"}}}}}, "needs a numeric field"},
		"having unknown":      {Query{Metrics: []string{"cost"}, Having: []QueryFilter{{Field: "calls", Op: "gt", Value: "1"}}}, "one of the query's metrics or formulas"},
		"having text":         {Query{Metrics: []string{"cost"}, Having: []QueryFilter{{Field: "cost", Op: "contains", Value: "1"}}}, "op must be one of"},
		"having no number":    {Query{Metrics: []string{"cost"}, Having: []QueryFilter{{Field: "cost", Op: "gt", Value: "x"}}}, "op must be one of"},
		"formula unknown":     {Query{Metrics: []string{"cost"}, Formulas: []QueryFormula{{Name: "f", Expr: "cost / calls"}}}, `formula "cost / calls"`},
		"formula syntax":      {Query{Metrics: []string{"cost"}, Formulas: []QueryFormula{{Name: "f", Expr: "cost * (2"}}}, `formula "cost * (2"`},
		"formula injection":   {Query{Metrics: []string{"cost"}, Formulas: []QueryFormula{{Name: "f", Expr: "cost); DROP TABLE x"}}}, "formula"},
		"formula name":        {Query{Metrics: []string{"cost"}, Formulas: []QueryFormula{{Name: "a b", Expr: "cost"}}}, "letters, digits"},
		"formula clash":       {Query{Metrics: []string{"cost"}, Formulas: []QueryFormula{{Name: "cost", Expr: "cost"}}}, "already a column"},
		"formula repeat":      {Query{Metrics: []string{"cost"}, Formulas: []QueryFormula{{Name: "f", Expr: "cost"}, {Name: "f", Expr: "cost"}}}, "already a column"},
		"formulas on rows":    {Query{Columns: []string{"model"}, Formulas: []QueryFormula{{Name: "f", Expr: "1"}}}, "leave metrics"},
		"gt on text":          {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "model", Op: "gt", Value: "1"}}}, "needs a numeric field"},
		"gt not a number":     {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "cost_usd", Op: "gt", Value: "x"}}}, "takes a numeric value"},
		"is_empty value":      {Query{Metrics: []string{"cost"}, Filters: []QueryFilter{{Field: "model", Op: "is_empty", Value: "x"}}}, "takes no value"},
		"rows with metrics":   {Query{Metrics: []string{"cost"}, Columns: []string{"model"}}, "leave metrics"},
		"rows order":          {Query{Columns: []string{"model"}, OrderBy: "cost_usd"}, "one of the columns"},
		"rows limit":          {Query{Columns: []string{"model"}, Limit: 500}, "at most 200"},
		"page too deep":       {Query{Metrics: []string{"cost"}, Limit: 1000, Page: 102}, "page reaches past row"},
	} {
		t.Run(name, func(t *testing.T) {
			err := tt.q.Validate(now, testColumns)
			if !errors.Is(err, ErrInvalidQuery) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want ErrInvalidQuery containing %q", err, tt.want)
			}
		})
	}

	q := Query{Metrics: []string{"cost"}, Granularity: "day", To: now.Add(time.Hour)}
	if err := q.Validate(now, testColumns); err != nil {
		t.Fatal(err)
	}
	if q.View != "calls" || !q.To.Equal(now) || !q.From.Equal(now.Add(-7*24*time.Hour)) ||
		q.OrderBy != "time" || q.Order != "asc" || q.Limit != 100 {
		t.Fatalf("defaults not applied: %+v", q)
	}
}

// Every relation a compiled query reads must carry the workspace scope, or a
// caller could read another workspace's spans.
func TestCompiledQueriesScopeEveryRelation(t *testing.T) {
	now := time.Now()
	for name, q := range map[string]Query{
		"plain":           {Metrics: []string{"cost", "calls"}, Dimensions: []string{"model"}},
		"reconciled":      {Metrics: []string{"cost"}, Dimensions: []string{"user"}, Granularity: "day"},
		"reconciled flat": {Metrics: []string{"cost", "errors"}, Filters: []QueryFilter{{Field: "user", Op: "eq", Value: "u"}}},
		"joined":          {Metrics: []string{"cost"}, Dimensions: []string{"workflow", "model"}},
		"attribute":       {Metrics: []string{"cost"}, Dimensions: []string{"attr.team"}, Filters: []QueryFilter{{Field: "attr.env", Op: "in", Values: []string{"prod"}}}},
		"runs":            {View: "runs", Metrics: []string{"runs", "error_rate"}, Dimensions: []string{"workflow"}, Granularity: "hour"},
		"runs fields":     {View: "runs", Metrics: []string{"p90:duration_ms", "uniq:attr.customer"}, Dimensions: []string{"attr.team"}, Filters: []QueryFilter{{Field: "cost_usd", Op: "gt", Value: "1"}, {Field: "attr.env", Op: "eq", Value: "prod"}}},
		"runs rows":       {View: "runs", Columns: []string{"trace_id", "cost_usd", "attr.team"}, OrderBy: "cost_usd"},
		"run and parent":  {Metrics: []string{"cost"}, Dimensions: []string{"parent.kind", "run.attr.team"}, Filters: []QueryFilter{{Field: "run.errored", Op: "eq", Value: "1"}, {Field: "parent.attr.tool", Op: "not_empty"}}},
		"parent rows":     {Columns: []string{"trace_id", "parent.model", "run.cost_usd"}, Filters: []QueryFilter{{Field: "run.duration_ms", Op: "gt", Value: "1000"}}},
		"any, having":     {Metrics: []string{"cost", "calls"}, Dimensions: []string{"user"}, Filters: []QueryFilter{{Any: []QueryFilter{{Field: "user", Op: "eq", Value: "a"}, {Field: "workspace", Op: "in", Values: []string{"ws-1"}}}}}, Formulas: []QueryFormula{{Name: "per_call", Expr: "cost / calls"}}, Having: []QueryFilter{{Field: "per_call", Op: "gt", Value: "0.1"}}, OrderBy: "per_call"},
		"any on attrs":    {Metrics: []string{"calls"}, Filters: []QueryFilter{{Any: []QueryFilter{{Field: "attr.a", Op: "eq", Value: "1"}, {Field: "run.attr.b", Op: "eq", Value: "2"}}}}},
		"generic metrics": {Metrics: []string{"cost", "max:cost_usd", "uniq:attr.customer"}, Dimensions: []string{"kind"}, Filters: []QueryFilter{{Field: "input_tokens", Op: "ge", Value: "100"}}},
		"rows":            {Columns: []string{"trace_id", "cost_usd", "attr.team", "workflow"}, Filters: []QueryFilter{{Field: "error_message", Op: "not_empty"}}, OrderBy: "cost_usd"},
	} {
		t.Run(name, func(t *testing.T) {
			q.WorkspaceIDs = []string{"ws-1"}
			if err := q.Validate(now, testColumns); err != nil {
				t.Fatal(err)
			}
			c := q.Compile()
			reads := strings.Count(c.sql, "FROM tracium.")
			scoped := strings.Count(c.sql, "workspace_id = ?")
			if reads == 0 || reads != scoped {
				t.Fatalf("%d relations read, %d scoped:\n%s", reads, scoped, c.sql)
			}
			if got := strings.Count(c.sql, "?"); got != len(c.args) {
				t.Fatalf("%d placeholders, %d args:\n%s", got, len(c.args), c.sql)
			}
			for _, a := range c.args {
				if a == "ws-1" {
					return
				}
			}
			t.Fatal("workspace id not bound")
		})
	}
}

func TestFormulaSQL(t *testing.T) {
	q := Query{Metrics: []string{"cost", "calls", "uniq:attr.a-b", "cost_usd"}}
	for expr, want := range map[string]string{
		"cost / calls":             "q_m0 / q_m1",
		"cost_usd*2":               "q_m3 * 2",
		"-(cost - -calls) / 0.5":   "(-(q_m0 - (-q_m1))) / 0.5",
		"uniq:attr.a-b - 1":        "q_m2 - 1",
		"(cost + calls) * 100 / 3": "(q_m0 + q_m1) * 100 / 3",
	} {
		got, err := q.formulaSQL(expr)
		if err != nil || got != want {
			t.Errorf("formulaSQL(%q) = %q, %v; want %q", expr, got, err, want)
		}
	}
	for _, expr := range []string{"", "cost +", "cost calls", "(cost", "cost)", "1.2.3", "cost -- x", "cost; select", "costs"} {
		if got, err := q.formulaSQL(expr); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("formulaSQL(%q) = %q, want ErrInvalidQuery", expr, got)
		}
	}
}

func TestCompiledQueryWithoutWorkspacesMatchesNothing(t *testing.T) {
	q := Query{Metrics: []string{"cost"}, Dimensions: []string{"user"}}
	if err := q.Validate(time.Now(), testColumns); err != nil {
		t.Fatal(err)
	}
	c := q.Compile()
	if strings.Count(c.sql, "1 = 0") != strings.Count(c.sql, "FROM tracium.") {
		t.Fatalf("empty scope must match nothing in every relation:\n%s", c.sql)
	}
}

func TestRowQueryDefaults(t *testing.T) {
	q := Query{Columns: []string{"trace_id", "input"}}
	if err := q.Validate(time.Now(), testColumns); err != nil {
		t.Fatal(err)
	}
	if q.Limit != 50 || q.Order != "desc" {
		t.Fatalf("defaults not applied: %+v", q)
	}
	if c := q.Compile(); !strings.Contains(c.sql, "ORDER BY start_time_ms DESC") || !strings.Contains(c.sql, "leftUTF8(toString(input), 2001)") {
		t.Fatalf("unexpected sql:\n%s", c.sql)
	}
}

func TestFilterValueAcceptsNumbers(t *testing.T) {
	var f QueryFilter
	if err := json.Unmarshal([]byte(`{"field":"cost_usd","op":"gt","value":0.5}`), &f); err != nil || f.Value != "0.5" {
		t.Fatalf("value = %q, err = %v", f.Value, err)
	}
}

func TestColumnKind(t *testing.T) {
	for typ, want := range map[string]string{
		"Int64": kindNumber, "UInt8": kindNumber, "Float64": kindNumber, "Nullable(Decimal(18, 4))": kindNumber,
		"String": kindText, "LowCardinality(String)": kindText, "LowCardinality(Nullable(String))": kindText, "DateTime": kindText,
		"Map(String, String)": "", "Array(String)": "", "AggregateFunction(sum, Float64)": "",
	} {
		if got := columnKind(typ); got != want {
			t.Errorf("columnKind(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestRunQueryWaitsForASlot(t *testing.T) {
	for range cap(querySlots) {
		querySlots <- struct{}{}
	}
	defer func() {
		for range cap(querySlots) {
			<-querySlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := (&ClickHouseRepository{}).RunQuery(ctx, Query{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query ran with every slot taken: %v", err)
	}
}
