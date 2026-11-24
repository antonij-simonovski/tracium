package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/pgtest"
)

func TestStore(t *testing.T) {
	pool := pgtest.NewPool(t)
	s := NewStore(pool)
	ctx := context.Background()
	for _, ws := range []string{"ws-1", "ws-2"} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ($1, 'u', $1, $1, 'prod', 'owner')`, ws); err != nil {
			t.Fatal(err)
		}
	}
	saveSpan := func(ws, trace, span, version, output, corrected string) {
		t.Helper()
		r := &model.Review{TraceID: trace, WorkspaceID: ws, WorkflowName: "extract", WorkflowVersion: version, Output: output, ReviewedBy: "u", Source: model.ReviewSourceUser}
		if span != "" {
			r.SpanID, r.SpanName = span+"-id", span
		}
		r.CorrectedOutput = &corrected
		fields, _ := Grade(output, corrected)
		r.Fields, r.Verdict = fields, Verdict(fields)
		if err := s.Upsert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	save := func(ws, trace, version, output, corrected string) {
		t.Helper()
		saveSpan(ws, trace, "", version, output, corrected)
	}
	save("ws-1", "t1", "v1", `{"a": 1, "b": 2}`, `{"a": 1, "b": 3}`)
	save("ws-1", "t2", "v1", `{"a": 1, "b": 2}`, `{"a": 1, "b": 2}`)
	save("ws-1", "t3", "v2", `{"a": 9}`, `{"a": 1}`)
	save("ws-2", "t4", "v1", `{"a": 5}`, `{"a": 6}`)
	// Re-reviewing replaces the fields instead of adding to them.
	save("ws-1", "t1", "v1", `{"a": 1, "b": 2}`, `{"a": 2, "b": 3}`)
	// A span review of the same run is kept apart from the run review.
	saveSpan("ws-1", "t2", "classify", "v1", `{"a": 1}`, `{"a": 2}`)

	mine := []string{"ws-1"}
	run, classify := "", "classify"
	acc, err := s.Accuracy(ctx, mine, "extract", &run)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.VersionAccuracy{}
	for _, a := range acc {
		got[a.Version] = a
	}
	if a := got["v1"]; a.ReviewedRuns != 2 || a.CorrectRuns != 1 || a.Accuracy != 0.5 {
		t.Errorf("v1 = %+v", a)
	}
	if a := got["v2"]; a.ReviewedRuns != 1 || a.Accuracy != 0 {
		t.Errorf("v2 = %+v", a)
	}

	fields, err := s.FieldAccuracy(ctx, mine, "extract", "", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[0].Path != "a" || fields[0].Wrong != 1 || fields[0].Reviewed != 2 || fields[1].Path != "b" || fields[1].Accuracy != 0.5 {
		t.Errorf("fields = %+v", fields)
	}

	list, total, err := s.List(ctx, ListFilter{WorkspaceIDs: mine, Workflow: "extract", Version: "v1", WrongField: "a", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 || list[0].TraceID != "t1" || len(list[0].Fields) != 2 {
		t.Fatalf("list = %+v, %d, %v", list, total, err)
	}

	spanAcc, err := s.Accuracy(ctx, mine, "extract", &classify)
	if err != nil || len(spanAcc) != 1 || spanAcc[0].ReviewedRuns != 1 || spanAcc[0].Accuracy != 0 || spanAcc[0].Span != "classify" {
		t.Errorf("span accuracy = %+v, %v", spanAcc, err)
	}
	steps, err := s.Accuracy(ctx, mine, "extract", nil)
	if err != nil || len(steps) != 3 {
		t.Fatalf("all steps = %+v, %v", steps, err)
	}
	for _, a := range steps {
		if want := map[string]int64{":v1": 2, ":v2": 1, "classify:v1": 1}[a.Span+":"+a.Version]; a.ReviewedRuns != want {
			t.Errorf("all steps %s:%s = %+v", a.Span, a.Version, a)
		}
	}
	spanFields, err := s.FieldAccuracy(ctx, mine, "extract", "classify", "v1")
	if err != nil || len(spanFields) != 1 || spanFields[0].Wrong != 1 {
		t.Errorf("span fields = %+v, %v", spanFields, err)
	}
	v1 := "v1"
	spans, err := s.Spans(ctx, mine, "extract", &v1)
	if err != nil || len(spans) != 1 || spans[0].Name != "classify" || spans[0].Reviewed != 1 {
		t.Errorf("spans = %+v, %v", spans, err)
	}
	spanList, total, err := s.List(ctx, ListFilter{WorkspaceIDs: mine, Workflow: "extract", Span: "classify", Page: 1, PageSize: 10})
	if err != nil || total != 1 || spanList[0].SpanID != "classify-id" || spanList[0].SpanName != "classify" {
		t.Errorf("span list = %+v, %d, %v", spanList, total, err)
	}
	saveSpan("ws-1", "t3", "classify", "v2", `{"a": 1}`, `{"a": 1}`)
	if all, err := s.Spans(ctx, mine, "extract", nil); err != nil || len(all) != 1 || all[0].Reviewed != 2 {
		t.Errorf("spans of all versions = %+v, %v", all, err)
	}
	save("ws-1", "t5", "v3", `{"a": 1}`, `{"a": 1, "c": 2}`)
	if err := s.Upsert(ctx, &model.Review{TraceID: "t6", WorkspaceID: "ws-1", WorkflowName: "extract", WorkflowVersion: "v3", Verdict: model.VerdictMissing, ReviewedBy: "u", Source: model.ReviewSourceUser}); err != nil {
		t.Fatal(err)
	}
	v3Fields, err := s.FieldAccuracy(ctx, mine, "extract", "", "v3")
	if err != nil || len(v3Fields) != 2 || v3Fields[0].Path != "c" || v3Fields[0].Missing != 1 || v3Fields[1].Missing != 0 {
		t.Errorf("v3 fields = %+v, %v", v3Fields, err)
	}
	for verdicts, want := range map[string]int{"incorrect,missing": 2, "missing": 1, "": 2} {
		f := ListFilter{WorkspaceIDs: mine, Workflow: "extract", Version: "v3", Page: 1, PageSize: 10}
		if verdicts != "" {
			f.Verdicts = strings.Split(verdicts, ",")
		}
		if _, total, err := s.List(ctx, f); err != nil || total != want {
			t.Errorf("verdicts %q: total %d, %v", verdicts, total, err)
		}
	}
	api := &model.Review{TraceID: "t2", WorkspaceID: "ws-1", WorkflowName: "extract", WorkflowVersion: "v1", Verdict: model.VerdictIncorrect, ReviewedBy: "key", Source: model.ReviewSourceAPI}
	if err := s.Upsert(ctx, api); !errors.Is(err, ErrUserReview) {
		t.Errorf("api review over a user review: err = %v", err)
	}
	api.TraceID = "t7"
	if err := s.Upsert(ctx, api); err != nil {
		t.Fatal(err)
	}
	api.Verdict = model.VerdictCorrect
	if err := s.Upsert(ctx, api); err != nil {
		t.Errorf("api review over an api review: %v", err)
	}
	if run, err := s.Get(ctx, "t2", "", mine); err != nil || run.Verdict != model.VerdictCorrect || run.SpanID != "" {
		t.Errorf("run review of t2 = %+v, %v", run, err)
	}
	if err := s.Delete(ctx, "t2", "classify-id", mine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "t2", "", mine); err != nil {
		t.Errorf("deleting the span review removed the run review: %v", err)
	}

	if _, err := s.Get(ctx, "t4", "", mine); !errors.Is(err, ErrNotFound) {
		t.Errorf("other workspace's review: err = %v", err)
	}
	if err := s.Delete(ctx, "t4", "", mine); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted other workspace's review: err = %v", err)
	}
	if err := s.Delete(ctx, "t1", "", mine); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM run_review_fields WHERE trace_id = 't1'`).Scan(&left)
	if left != 0 {
		t.Errorf("fields left after delete: %d", left)
	}
}
