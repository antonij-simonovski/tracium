package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/review"
	"github.com/tracium/api/testing/mocks"
	"github.com/tracium/api/testing/pgtest"
)

func TestReviewPut(t *testing.T) {
	pool := pgtest.NewPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO workspaces (id, user_id, name, slug, env, role) VALUES ('ws', 'u', 'ws', 'ws', 'prod', 'owner')`); err != nil {
		t.Fatal(err)
	}
	repo := mocks.NewMockTraceRepository()
	repo.AddTrace(model.Trace{TraceID: "run", WorkspaceID: "ws", Name: "extract", WorkflowVersion: "v2"}, []model.Span{
		{TraceID: "run", SpanID: "llm", Name: "parse", ParentSpanID: "root", WorkspaceID: "ws", Output: "draft", StartTimeMs: 5, SchemaVersion: 1},
		{TraceID: "run", SpanID: "root", WorkspaceID: "ws", Output: `{"total": 10, "due": "2026-01-01"}`, SchemaVersion: 1},
	})

	putAs := func(auth middleware.Authenticator, scope []string, path, body string) (int, model.Review) {
		r := chi.NewRouter()
		r.Use(middleware.Auth(auth))
		h := NewReviewHandler(repo, review.NewStore(pool), traceTestAccess{ids: scope})
		r.Put("/v1/traces/{id}/review", h.Put)
		r.Put("/v1/traces/{id}/spans/{spanId}/review", h.Put)
		r.Put("/v1/workflows/{name}/reviews", h.PutByKey)
		req := httptest.NewRequest("PUT", path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer t")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code == 204 && rr.Body.Len() > 0 {
			t.Errorf("%s: a key got a reply body: %s", path, rr.Body)
		}
		var rev model.Review
		_ = json.Unmarshal(rr.Body.Bytes(), &rev)
		return rr.Code, rev
	}
	saved := func(traceID, spanID string) model.Review {
		rev, err := review.NewStore(pool).Get(context.Background(), traceID, spanID, []string{"ws"})
		if err != nil {
			t.Fatalf("saved review %s/%s: %v", traceID, spanID, err)
		}
		return *rev
	}
	put := func(scope []string, body string) (int, model.Review) {
		return putAs(stubAuthenticator{userID: "reviewer"}, scope, "/v1/traces/run/review", body)
	}

	if code, _ := put([]string{"other"}, `{"verdict": "correct"}`); code != 404 {
		t.Errorf("non-member: %d", code)
	}
	if code, _ := put([]string{"ws"}, `{"note": "x"}`); code != 400 {
		t.Errorf("no verdict: %d", code)
	}

	code, rev := put([]string{"ws"}, `{"corrected_output": "{\"total\": 10, \"due\": \"2026-02-01\"}"}`)
	if code != 200 || rev.Verdict != model.VerdictIncorrect || rev.WorkflowVersion != "v2" || rev.WorkflowName != "extract" || rev.ReviewedBy != "reviewer" {
		t.Fatalf("json correction: %d %+v", code, rev)
	}
	if len(rev.Fields) != 2 || rev.Fields[0].Path != "due" || rev.Fields[0].Correct || !rev.Fields[1].Correct {
		t.Errorf("fields = %+v", rev.Fields)
	}

	if _, rev := put([]string{"ws"}, `{"verdict": "incorrect", "corrected_output": " {\"total\": 10, \"due\": \"2026-01-01\"} "}`); rev.Verdict != model.VerdictCorrect {
		t.Errorf("matching correction verdict = %s", rev.Verdict)
	}
	if _, rev := put([]string{"ws"}, `{"verdict": "correct"}`); rev.Verdict != model.VerdictCorrect || len(rev.Fields) != 2 || !rev.Fields[0].Correct || !rev.Fields[1].Correct || rev.CorrectedOutput != nil || rev.Source != model.ReviewSourceUser {
		t.Errorf("verdict only = %+v", rev)
	}

	if _, rev := put([]string{"ws"}, `{"verdict": "missing"}`); rev.Verdict != model.VerdictMissing || len(rev.Fields) != 0 {
		t.Errorf("missing verdict = %+v", rev)
	}
	repo.AddTrace(model.Trace{TraceID: "silent", WorkspaceID: "ws", Name: "extract", WorkflowVersion: "v2"}, []model.Span{{TraceID: "silent", SpanID: "root", WorkspaceID: "ws", SchemaVersion: 1}})
	if _, rev := putAs(stubAuthenticator{userID: "reviewer"}, []string{"ws"}, "/v1/traces/silent/review", `{"corrected_output": "{\"total\": 10}"}`); rev.Verdict != model.VerdictMissing {
		t.Errorf("correction of an empty output = %+v", rev)
	}
	if code, _ := put([]string{"ws"}, `{"verdict": "incorrect"}`); code != 400 {
		t.Errorf("incorrect JSON output without a correction: %d", code)
	}
	if code, _ := put([]string{"ws"}, `{"verdict": "absent"}`); code != 400 {
		t.Errorf("unknown verdict: %d", code)
	}

	key := keyAuthenticator{model.Principal{UserID: "key-owner", WorkspaceID: "ws"}}
	if code, _ := putAs(key, nil, "/v1/traces/run/spans/llm/review", `{"corrected_output": "final"}`); code != 204 {
		t.Errorf("span review by key: %d", code)
	}
	if rev := saved("run", "llm"); rev.SpanName != "parse" || rev.Verdict != model.VerdictIncorrect || rev.Source != model.ReviewSourceAPI {
		t.Errorf("span review by key saved %+v", rev)
	}
	if code, _ := putAs(key, nil, "/v1/traces/run/spans/nope/review", `{"verdict": "correct"}`); code != 404 {
		t.Errorf("unknown span: %d", code)
	}
	if code, _ := putAs(keyAuthenticator{model.Principal{UserID: "key-owner", WorkspaceID: "other"}}, nil, "/v1/traces/run/review", `{"verdict": "correct"}`); code != 404 {
		t.Errorf("key of another workspace: %d", code)
	}

	if code, _ := putAs(key, nil, "/v1/traces/run/review", `{"verdict": "correct"}`); code != 409 {
		t.Errorf("key over a user review: %d", code)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM run_reviews WHERE trace_id = 'run' AND span_id = ''`); err != nil {
		t.Fatal(err)
	}

	repo.TraceAttributes = map[string]map[string]string{"run": {"invoice_id": "INV-1"}}
	byKey := func(query, body string) (int, model.Review) {
		return putAs(key, nil, "/v1/workflows/extract/reviews?"+query, body)
	}
	if code, _ := byKey("key=invoice_id&value=INV-1", `{"corrections": {"due": "2026-02-01"}, "note": "ap clerk"}`); code != 204 {
		t.Fatalf("by key with corrections: %d", code)
	}
	rev = saved("run", "")
	if rev.Verdict != model.VerdictIncorrect || rev.Source != model.ReviewSourceAPI || rev.Note != "ap clerk" {
		t.Fatalf("by key with corrections saved %+v", rev)
	}
	if len(rev.Fields) != 2 || rev.Fields[0].Path != "due" || rev.Fields[0].Correct || !rev.Fields[1].Correct {
		t.Errorf("patched fields = %+v", rev.Fields)
	}
	if code, _ := byKey("key=invoice_id&value=INV-1&span=parse", `{"verdict": "correct"}`); code != 204 || saved("run", "llm").Verdict != model.VerdictCorrect {
		t.Errorf("span by name: %d", code)
	}
	if byKey("key=invoice_id&value=INV-1", `{"corrections": {}}`); saved("run", "").Verdict != model.VerdictCorrect {
		t.Errorf("empty corrections verdict = %s", saved("run", "").Verdict)
	}
	for _, tc := range []struct {
		path, query, body string
		want              int
	}{
		{"extract", "key=invoice_id&value=INV-2", `{"verdict": "correct"}`, 404},
		{"other", "key=invoice_id&value=INV-1", `{"verdict": "correct"}`, 404},
		{"extract", "key=invoice_id", `{"verdict": "correct"}`, 400},
		{"extract", "key=invoice_id&value=INV-1&span=nope", `{"verdict": "correct"}`, 404},
		{"extract", "key=invoice_id&value=INV-1&span=parse", `{"corrections": {"a": 1}}`, 400},
		{"extract", "key=invoice_id&value=INV-1", `{"corrections": {"a": 1}, "corrected_output": "{}"}`, 400},
	} {
		if code, _ := putAs(key, nil, "/v1/workflows/"+tc.path+"/reviews?"+tc.query, tc.body); code != tc.want {
			t.Errorf("%s?%s %s: %d, want %d", tc.path, tc.query, tc.body, code, tc.want)
		}
	}
}

type keyAuthenticator struct{ p model.Principal }

func (a keyAuthenticator) Authenticate(context.Context, string) (*model.Principal, error) {
	return &a.p, nil
}
