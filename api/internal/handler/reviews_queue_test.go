package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/review"
	"github.com/tracium/api/testing/mocks"
	"github.com/tracium/api/testing/pgtest"
)

type fakeReviewed struct {
	review.Store
	done  map[string]bool
	calls int
}

func (f *fakeReviewed) Reviewed(_ context.Context, _, ids []string) (map[string]bool, error) {
	f.calls++
	out := map[string]bool{}
	for _, id := range ids {
		if f.done[id] {
			out[id] = true
		}
	}
	return out, nil
}

func items(prefix, reason string, n int) []model.QueueItem {
	out := make([]model.QueueItem, n)
	for i := range out {
		out[i] = model.QueueItem{Trace: model.Trace{TraceID: fmt.Sprintf("%s%d", prefix, i)}, Reason: reason}
	}
	return out
}

func reasons(q []model.QueueItem) []string {
	out := make([]string, len(q))
	for i, it := range q {
		out[i] = it.Reason
	}
	return out
}

func TestBuildQueueMixesOneSampleInFive(t *testing.T) {
	q := buildQueue(items("f", "truncated", 30), items("s", "sample", 30), 20)
	if len(q) != 20 {
		t.Fatalf("len = %d", len(q))
	}
	n := 0
	for i, it := range q {
		if it.Reason == "sample" {
			n++
			if i%5 != 4 {
				t.Errorf("sample at %d", i)
			}
		}
	}
	if n != 4 {
		t.Errorf("samples = %d, want 4 of 20", n)
	}
	if q[0].TraceID != "f0" || q[1].TraceID != "f1" || q[5].TraceID != "f4" {
		t.Errorf("flagged order lost: %v", q)
	}
	if got := reasons(buildQueue(items("f", "refused", 2), items("s", "sample", 5), 5)); !slices.Equal(got, []string{"refused", "refused", "sample", "sample", "sample"}) {
		t.Errorf("few flagged: %v", got)
	}
	if got := buildQueue(items("f", "refused", 7), nil, 5); len(got) != 5 {
		t.Errorf("no samples: %d", len(got))
	}
	if got := buildQueue(nil, nil, 20); got == nil || len(got) != 0 {
		t.Errorf("empty = %#v", got)
	}
}

func TestReviewQueue(t *testing.T) {
	repo := mocks.NewMockTraceRepository()
	add := func(id, ws, wf string, flags ...string) {
		repo.AddTrace(model.Trace{TraceID: id, WorkspaceID: ws, Name: wf, SilentFailures: append([]string{}, flags...)}, nil)
	}
	add("inc-run", "mine", "wf", "truncated")
	add("trunc", "mine", "wf", "truncated")
	add("tool", "mine", "wf", "tool_error")
	add("done", "mine", "wf", "refused")
	add("other-wf", "mine", "other", "refused")
	add("clean-1", "mine", "wf")
	add("clean-2", "mine", "wf")
	add("theirs-run", "theirs", "wf", "truncated")
	incidents := &mocks.MockIncidentStore{Incidents: []incident.Incident{
		{ID: "inc-1", WorkspaceID: "mine", Scope: "workflow", Subject: "wf", Status: incident.StatusOpen,
			Signals: []incident.Signal{{Metric: "truncation_rate", TraceIDs: []string{"inc-run", "expired"}}}},
		{ID: "inc-2", WorkspaceID: "theirs", Scope: "workflow", Subject: "wf", Status: incident.StatusOpen,
			Signals: []incident.Signal{{Metric: "truncation_rate", TraceIDs: []string{"theirs-run"}}}},
		{ID: "inc-3", WorkspaceID: "mine", Scope: "workflow", Subject: "wf", Status: incident.StatusResolved,
			Signals: []incident.Signal{{Metric: "truncation_rate", TraceIDs: []string{"clean-1"}}}},
	}}
	reviews := &fakeReviewed{done: map[string]bool{"done": true}}
	h := NewReviewQueueHandler(repo, reviews, incidents, traceTestAccess{ids: []string{"mine"}})
	get := func(query string) (int, []model.QueueItem) {
		req := httptest.NewRequest("GET", "/v1/reviews/queue"+query, nil)
		req = req.WithContext(middleware.ContextWithPrincipal(req.Context(), &model.Principal{UserID: "u1"}))
		rec := httptest.NewRecorder()
		h.Queue(rec, req)
		var out []model.QueueItem
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, q := get("?workflow=wf")
	if code != 200 {
		t.Fatalf("code = %d", code)
	}
	var ids []string
	for _, it := range q {
		ids = append(ids, it.TraceID)
	}
	if len(q) != 5 || q[0].TraceID != "inc-run" || q[0].Reason != "incident" || q[0].IncidentID != "inc-1" {
		t.Fatalf("queue = %v", ids)
	}
	if q[1].TraceID != "trunc" || q[1].Reason != "truncated" || q[2].TraceID != "tool" || q[2].Reason != "tool_error" {
		t.Errorf("flagged order = %v", ids)
	}
	if !slices.Equal(reasons(q[3:]), []string{"sample", "sample"}) {
		t.Errorf("samples = %v", reasons(q))
	}
	for _, id := range ids {
		if id == "done" || id == "theirs-run" || id == "other-wf" {
			t.Errorf("%s must not be queued: %v", id, ids)
		}
	}
	if reviews.calls != 1 {
		t.Errorf("reviewed lookups = %d, want 1", reviews.calls)
	}
	if f := repo.LastFilter; !slices.Equal(f.WorkspaceIDs, []string{"mine"}) || f.StartAfter.IsZero() {
		t.Errorf("listing not scoped and bounded: %+v", f)
	}

	if _, q := get("?workflow=nothing"); len(q) != 0 {
		t.Errorf("empty workflow queue = %v", q)
	}
	if code, _ := get("?limit=101"); code != 400 {
		t.Errorf("limit 101 = %d", code)
	}
	if _, q := get("?limit=2"); len(q) != 2 {
		t.Errorf("limit 2 = %d items", len(q))
	}
}

func TestReviewPutReason(t *testing.T) {
	pool := pgtest.NewPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ('ws', 'u', 'ws', 'ws', 'prod', 'owner')`); err != nil {
		t.Fatal(err)
	}
	repo := mocks.NewMockTraceRepository()
	repo.AddTrace(model.Trace{TraceID: "flagged", WorkspaceID: "ws", Name: "wf"}, []model.Span{{TraceID: "flagged", SpanID: "s", WorkspaceID: "ws", SilentFailure: "refused"}})
	repo.AddTrace(model.Trace{TraceID: "clean", WorkspaceID: "ws", Name: "wf"}, []model.Span{{TraceID: "clean", SpanID: "s", WorkspaceID: "ws"}})
	h := NewReviewHandler(repo, review.NewStore(pool), traceTestAccess{ids: []string{"ws"}})
	r := chi.NewRouter()
	r.Use(middleware.Auth(stubAuthenticator{userID: "u"}))
	r.Put("/v1/traces/{id}/review", h.Put)
	put := func(trace, body string) (int, model.Review) {
		req := httptest.NewRequest("PUT", "/v1/traces/"+trace+"/review", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var rev model.Review
		_ = json.Unmarshal(rec.Body.Bytes(), &rev)
		return rec.Code, rev
	}
	if code, _ := put("clean", `{"verdict": "correct", "reason": "bogus"}`); code != 400 {
		t.Errorf("bad reason = %d", code)
	}
	for trace, c := range map[string]struct{ body, want string }{
		"flagged": {`{"verdict": "incorrect"}`, "refused"},
		"clean":   {`{"verdict": "correct", "reason": "sample"}`, "sample"},
	} {
		if code, rev := put(trace, c.body); code != 200 || rev.Reason != c.want {
			t.Errorf("%s: %d reason %q, want %q", trace, code, rev.Reason, c.want)
		}
	}
}
