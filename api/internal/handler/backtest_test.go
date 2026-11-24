package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/incident"
)

type stubBacktests struct {
	workspace string
	err       error
}

func (s *stubBacktests) Backtest(_ context.Context, workspaceID, rng string) (*incident.BacktestResult, error) {
	s.workspace = workspaceID
	return &incident.BacktestResult{Range: rng}, s.err
}

func TestBacktestHandler(t *testing.T) {
	cases := []struct {
		query string
		err   error
		want  int
	}{
		{"?workspace_id=mine&range=30d", nil, http.StatusOK},
		{"?workspace_id=mine&range=7d", nil, http.StatusOK},
		{"?workspace_id=mine&range=90d", nil, http.StatusBadRequest},
		{"?range=30d", nil, http.StatusBadRequest},
		{"?workspace_id=theirs&range=30d", nil, http.StatusForbidden},
		{"?workspace_id=mine", context.DeadlineExceeded, http.StatusGatewayTimeout},
	}
	for _, tc := range cases {
		stub := &stubBacktests{err: tc.err}
		r := chi.NewRouter()
		r.Get("/v1/incidents/backtest", NewBacktestHandler(stub, traceTestAccess{ids: []string{"mine"}}).Get)
		rec := serveIncident(r, http.MethodGet, "/v1/incidents/backtest"+tc.query, "")
		if rec.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d: %s", tc.query, rec.Code, tc.want, rec.Body)
		}
		if tc.want == http.StatusForbidden && stub.workspace != "" {
			t.Fatalf("%s: ran a backtest for %q", tc.query, stub.workspace)
		}
	}
}
