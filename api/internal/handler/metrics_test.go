package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/testing/mocks"
)

// allowAllAccess is a WorkspaceAccess that grants a fixed workspace, so metrics
// handler tests exercise the query path rather than the access boundary.
type allowAllAccess struct{}

func (allowAllAccess) AllowedIDs(context.Context, string) ([]string, error) {
	return []string{"ws-test"}, nil
}

// newMetricsHandler builds a MetricsHandler wired with a permissive access
// resolver — the drop-in the tests use in place of the production constructor.
func newMetricsHandler(repo query.MetricsRepository) *MetricsHandler {
	return NewMetricsHandler(repo, allowAllAccess{})
}

// authed attaches an authenticated principal to a request, as the middleware
// chain would, so the handler's workspace-access enforcement can resolve it.
func authed(req *http.Request) *http.Request {
	return req.WithContext(middleware.ContextWithPrincipal(
		req.Context(), &model.Principal{UserID: "u-test", TenantID: "t-test", Role: "user"}))
}

// serveAuthed invokes a handler method with an authenticated request.
func serveAuthed(hf http.HandlerFunc, rr http.ResponseWriter, req *http.Request) {
	hf.ServeHTTP(rr, authed(req))
}

// workflowDetailRequest builds a request carrying the {name} path param the way chi
// would, so WorkflowDetail's chi.URLParam("name") resolves in a unit test.
func workflowDetailRequest(name, rawQuery string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/workflows/"+name+"?"+rawQuery, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestMetricsKPIs(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		KPIs: model.KPISet{
			Cost: model.KPI{Value: 2.64, Delta: 0.12, DeltaType: "bad"},
			Runs: model.KPI{Value: 3751, Delta: 0.08, DeltaType: "good"},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.KPIs, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/kpis?range=7d", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got model.KPISet
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Cost.Value != 2.64 || got.Cost.DeltaType != "bad" {
		t.Errorf("cost KPI = %+v, want value 2.64 / bad", got.Cost)
	}
	if got.Runs.Value != 3751 {
		t.Errorf("runs value = %v, want 3751", got.Runs.Value)
	}
}

func TestMetricsErrorSeries(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		Errors: []model.ErrorPoint{
			{BucketMs: 1_700_000_000_000, Errors: 2, Total: 312},
			{BucketMs: 1_700_086_400_000, Errors: 8, Total: 298},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.ErrorSeries, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/error-series?range=7d", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Items []model.ErrorPoint `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 2 || got.Items[1].Errors != 8 || got.Items[1].Total != 298 {
		t.Errorf("items = %+v, want two buckets ending in 8/298", got.Items)
	}
}

func TestMetricsWorkflowsEnvelope(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		WorkflowRows: []model.Workflow{
			{Name: "classify-intent", Calls: 1203, Cost: 0.1204, AvgLatencyMs: 820, ErrorRate: 0.014, Trend: []int64{42, 48, 52}, LastTraceID: "tr_classify_999"},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.Workflows, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/workflows?range=7d", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Items []model.Workflow `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "classify-intent" {
		t.Fatalf("items = %+v, want one classify-intent row", got.Items)
	}
	a := got.Items[0]
	if a.Calls != 1203 || a.AvgLatencyMs != 820 || a.ErrorRate != 0.014 || len(a.Trend) != 3 || a.LastTraceID != "tr_classify_999" {
		t.Errorf("workflow row = %+v, want calls 1203 / latency 820 / err 0.014 / 3 trend points / last trace tr_classify_999", a)
	}
}

func TestMetricsUserUsageEnvelope(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		Users: []model.UserUsage{
			{UserID: "tn_acme", Cost: 12.44, CostPrev: 10.88, Runs: 58022, RunsPrev: 53110, Trend: []float64{0.4, 0.5}},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.UserUsage, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/usage-users?range=30d", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Items []model.UserUsage `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].UserID != "tn_acme" {
		t.Fatalf("items = %+v, want one tn_acme row", got.Items)
	}
	if got.Items[0].CostPrev != 10.88 || got.Items[0].RunsPrev != 53110 {
		t.Errorf("prev-period fields = %+v, want cost_prev 10.88 / runs_prev 53110", got.Items[0])
	}
}

func TestMetricsWorkflowDetail(t *testing.T) {
	p95 := 6.4
	repo := &mocks.MockMetricsRepository{
		WorkflowDetailV: model.WorkflowDetail{
			Name: "rewrite-message", Calls: 156, Cost: 0.3104,
			AvgLatencyMs: 4800, P95LatencyMs: &p95, ErrorRate: 0.083,
			InputTokens: 41000, OutputTokens: 9000,
			Model: "claude-haiku-4-5", Provider: "anthropic",
			Tools:       []model.AvailableTool{{Name: "tone.classify", Used: true, Description: "scores tone"}},
			LastTraceID: "t_eb7c",
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.WorkflowDetail, rr, workflowDetailRequest("rewrite-message", "range=7d"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got model.WorkflowDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "rewrite-message" || got.Calls != 156 || got.Provider != "anthropic" {
		t.Errorf("detail = %+v, want rewrite-message / 156 / anthropic", got)
	}
	if got.P95LatencyMs == nil || *got.P95LatencyMs != 6.4 {
		t.Errorf("p95 = %v, want 6.4", got.P95LatencyMs)
	}
	if len(got.Tools) != 1 || !got.Tools[0].Used {
		t.Errorf("tools = %+v, want one used tool", got.Tools)
	}
}

func TestMetricsWorkflowDetailNotFound(t *testing.T) {
	repo := &mocks.MockMetricsRepository{Err: query.ErrNotFound}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.WorkflowDetail, rr, workflowDetailRequest("ghost-workflow", "range=7d"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var body model.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "WORKFLOW_NOT_FOUND" {
		t.Errorf("code = %q, want WORKFLOW_NOT_FOUND", body.Code)
	}
}

// Per-workflow latency can't come from the daily rollup, so workflow-scoped requests
// over rollup ranges (>30d) are rejected rather than silently degraded.
func TestMetricsWorkflowDetailRejectsRollupRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})

	rr := httptest.NewRecorder()
	serveAuthed(h.WorkflowDetail, rr, workflowDetailRequest("rewrite-message", "range=1y"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

// The workflow query param on the series endpoints is likewise a raw-window feature.
func TestMetricsSeriesWorkflowRejectsRollupRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})

	rr := httptest.NewRecorder()
	serveAuthed(h.CostSeries, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/cost-series?range=90d&workflow=rewrite-message", nil))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestMetricsBadRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})

	rr := httptest.NewRecorder()
	serveAuthed(h.KPIs, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/kpis?range=nonsense", nil))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestMetricsFailuresEnvelope(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		FailureItems:  []model.Failure{{Workflow: "rewrite-message", Count: 8, Pct: 0.083, TopError: "rate_limit_exceeded"}},
		FailuresTotal: 12,
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.Failures, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/failures?range=24h", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	// Total reflects every failed run, not just the listed workflows.
	var got struct {
		Items []model.Failure `json:"items"`
		Total int             `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 12 {
		t.Errorf("total = %d, want 12", got.Total)
	}
	if len(got.Items) != 1 || got.Items[0].Workflow != "rewrite-message" {
		t.Errorf("items = %+v, want one rewrite-message row", got.Items)
	}
}

func TestMetricsAnomalies(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		AnomalyItems: []model.Anomaly{
			{Metric: "cost", Scope: "workflow", Workflow: "planner", BucketMs: 1_700_000_000_000,
				Observed: 120, Expected: 10, Deviation: 110, Score: 8.1,
				Direction: "spike", Severity: "critical", Summary: "Workflow \"planner\" cost rose to $120.00."},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.Anomalies, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/anomalies?range=30d", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Items []model.Anomaly `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Severity != "critical" || got.Items[0].Workflow != "planner" {
		t.Errorf("items = %+v, want one critical anomaly for planner", got.Items)
	}
}

func TestMetricsAnomaliesRejectsSubDailyRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})
	rr := httptest.NewRecorder()
	serveAuthed(h.Anomalies, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/anomalies?range=24h", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a 24h range", rr.Code)
	}
}

func TestMetricsSetupChecks(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		Checks: []model.SetupCheck{{
			SetupIssue: model.SetupIssue{Code: "unmetered", Severity: "warning", Message: "Streamed calls arrived without token usage."},
			Spans:      3, Share: 0.5, ExampleTraceID: "t1",
		}},
	}
	rr := httptest.NewRecorder()
	serveAuthed(newMetricsHandler(repo).SetupChecks, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/setup-checks?range=24h", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Items []model.SetupCheck `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Code != "unmetered" || got.Items[0].ExampleTraceID != "t1" {
		t.Errorf("items = %+v, want the unmetered check", got.Items)
	}
}

func TestMetricsSetupChecksRejectsRollupRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})
	rr := httptest.NewRecorder()
	serveAuthed(h.SetupChecks, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/setup-checks?range=90d", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a 90d range", rr.Code)
	}
}

func TestMetricsRejectsWorkflowOnUnscopedEndpoints(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})
	for path, hf := range map[string]http.HandlerFunc{
		"/v1/metrics/kpis":           h.KPIs,
		"/v1/metrics/top-workflows":  h.TopWorkflows,
		"/v1/metrics/failures":       h.Failures,
		"/v1/metrics/usage-users":    h.UserUsage,
		"/v1/metrics/attribute-keys": h.AttributeKeys,
	} {
		for _, param := range []string{"workflow=planner", "version=v2"} {
			rr := httptest.NewRecorder()
			serveAuthed(hf, rr, httptest.NewRequest(http.MethodGet, path+"?range=7d&"+param, nil))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s?%s: status = %d, want 400", path, param, rr.Code)
			}
		}
	}
}

func TestMetricsSeriesAcceptsWorkflowAndVersion(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})
	for _, q := range []string{"workflow=planner", "version=v2", "workflow=planner&version=v2"} {
		rr := httptest.NewRecorder()
		serveAuthed(h.ErrorSeries, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/error-series?range=7d&"+q, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", q, rr.Code)
		}
	}
}

func TestMetricsSeriesVersionRejectsRollupRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})

	rr := httptest.NewRecorder()
	serveAuthed(h.LatencySeries, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/latency-series?range=90d&version=v2", nil))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestMetricsWorkflowVersions(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		Versions: []model.WorkflowVersion{
			{Version: "v12", Source: "explicit", Calls: 40, Cost: 2, CostPerRun: 0.05},
			{Version: "", Calls: 3},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.WorkflowVersions, rr, workflowDetailRequest("planner", "range=1y"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Items []model.WorkflowVersion `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].Version != "v12" || got.Items[0].Source != "explicit" {
		t.Errorf("items = %+v, want v12 (explicit) first of 2", got.Items)
	}
}

func TestMetricsCompareVersions(t *testing.T) {
	repo := &mocks.MockMetricsRepository{
		Comparison: model.VersionComparison{
			Base:       model.WorkflowVersion{Version: "v11"},
			Candidate:  model.WorkflowVersion{Version: "v12"},
			CostPerRun: model.KPI{Value: 0.07, Delta: 0.4, DeltaType: "bad"},
		},
	}
	h := newMetricsHandler(repo)

	rr := httptest.NewRecorder()
	serveAuthed(h.CompareVersions, rr, workflowDetailRequest("planner", "range=7d&base=v11&candidate=v12"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got model.VersionComparison
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Candidate.Version != "v12" || got.CostPerRun.DeltaType != "bad" {
		t.Errorf("comparison = %+v, want v12 with a bad cost-per-run delta", got)
	}
}

func TestMetricsCompareVersionsErrors(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		err         error
		status      int
	}{
		{"missing candidate", "range=7d&base=v11", nil, http.StatusBadRequest},
		{"unknown version", "range=7d&base=v11&candidate=v99", query.ErrNotFound, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMetricsHandler(&mocks.MockMetricsRepository{Err: tc.err})
			rr := httptest.NewRecorder()
			serveAuthed(h.CompareVersions, rr, workflowDetailRequest("planner", tc.query))
			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d", rr.Code, tc.status)
			}
		})
	}
}

func TestMetricsAttributeEndpointsRejectRollupRange(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})
	for path, hf := range map[string]http.HandlerFunc{
		"/v1/metrics/attribute-keys?range=1y":               h.AttributeKeys,
		"/v1/metrics/usage-by-attribute?range=90d&key=team": h.UsageByAttribute,
	} {
		rr := httptest.NewRecorder()
		serveAuthed(hf, rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, rr.Code)
		}
	}
}

func TestMetricsAnomaliesRejectsBadFilters(t *testing.T) {
	h := newMetricsHandler(&mocks.MockMetricsRepository{})
	for _, q := range []string{"range=30d&metric=latency", "range=30d&min_severity=urgent"} {
		rr := httptest.NewRecorder()
		serveAuthed(h.Anomalies, rr, httptest.NewRequest(http.MethodGet, "/v1/metrics/anomalies?"+q, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, rr.Code)
		}
	}
}
