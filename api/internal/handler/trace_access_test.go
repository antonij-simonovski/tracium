package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/mocks"
)

type traceTestAccess struct {
	ids []string
	err error
}

func (a traceTestAccess) AllowedIDs(context.Context, string) ([]string, error) { return a.ids, a.err }

func TestTraceDetailsEnforceWorkspaceMembership(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scope     []string
		requested string
		want      int
	}{
		{"unrelated account", []string{"other"}, "", 404},
		{"no memberships", nil, "", 404},
		{"explicit forbidden workspace", []string{"other"}, "?workspace_id=victim", 403},
		{"member", []string{"victim"}, "", 200},
		{"member explicit scope", []string{"victim", "other"}, "?workspace_id=victim", 200},
	} {
		for _, suffix := range []string{"", "/spans"} {
			t.Run(tc.name+suffix, func(t *testing.T) {
				repo := mocks.NewMockTraceRepository()
				repo.AddTrace(model.Trace{TraceID: "shared-id", WorkspaceID: "victim"}, []model.Span{
					{TraceID: "shared-id", SpanID: "one", WorkspaceID: "victim", Input: "member-only-content", SchemaVersion: 1},
					{TraceID: "shared-id", SpanID: "two", WorkspaceID: "unrelated", Input: "never-return-this-content", SchemaVersion: 1},
				})
				r := chi.NewRouter()
				r.Use(middleware.Auth(stubAuthenticator{userID: "test-account"}))
				access := traceTestAccess{ids: tc.scope}
				r.Get("/v1/traces/{id}", NewTraceHandler(repo, access).GetTrace)
				r.Get("/v1/traces/{traceId}/spans", NewSpanHandler(repo, access).ListSpans)
				req := httptest.NewRequest("GET", "/v1/traces/shared-id"+suffix+tc.requested, nil)
				req.Header.Set("Authorization", "Bearer test-token")
				rr := httptest.NewRecorder()
				r.ServeHTTP(rr, req)
				if rr.Code != tc.want {
					t.Fatalf("status=%d want=%d body=%s", rr.Code, tc.want, rr.Body.String())
				}
				if strings.Contains(rr.Body.String(), "never-return-this-content") {
					t.Fatal("cross-workspace span leaked")
				}
				if tc.want == 200 && !strings.Contains(rr.Body.String(), "member-only-content") {
					t.Fatal("member cannot read their span")
				}
				if tc.want != 200 && strings.Contains(rr.Body.String(), "member-only-content") {
					t.Fatal("unauthorized content returned")
				}
			})
		}
	}
}
