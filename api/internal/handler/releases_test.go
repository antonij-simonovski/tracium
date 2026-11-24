package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/release"
	"github.com/tracium/api/testing/mocks"
)

func releaseRouter(repo *mocks.MockMetricsRepository) http.Handler {
	store := &mocks.MockReleaseStore{Reports: []release.Report{
		{WorkspaceID: "mine", Workflow: "wf", Version: "v2", Status: release.StatusFinal, Verdict: "regressed"},
		{WorkspaceID: "mine", Workflow: "wf", Version: "v3", Status: release.StatusPending},
		{WorkspaceID: "theirs", Workflow: "wf", Version: "v9", Status: release.StatusFinal, Verdict: "ok"},
	}}
	h := NewMetricsHandler(repo, traceTestAccess{ids: []string{"mine"}}, store)
	r := chi.NewRouter()
	r.Get("/v1/metrics/workflows/{name}/versions", h.WorkflowVersions)
	r.Get("/v1/metrics/workflows/{name}/versions/{version}/report", h.ReleaseReport)
	r.Get("/v1/release-reports", h.ReleaseReports)
	return r
}

func TestReleaseReport(t *testing.T) {
	r := releaseRouter(&mocks.MockMetricsRepository{})
	if rec := serveIncident(r, http.MethodGet, "/v1/metrics/workflows/wf/versions/v2/report", ""); rec.Code != http.StatusOK {
		t.Errorf("own report: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/v1/metrics/workflows/wf/versions/v9/report", "/v1/metrics/workflows/wf/versions/v1/report"} {
		if rec := serveIncident(r, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
	}
}

func TestReleaseReports(t *testing.T) {
	r := releaseRouter(&mocks.MockMetricsRepository{})
	rec := serveIncident(r, http.MethodGet, "/v1/release-reports?status=pending", "")
	var got struct {
		Items []release.Report `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Items) != 1 || got.Items[0].Version != "v3" {
		t.Errorf("pending: %d %s", rec.Code, rec.Body)
	}
	for _, q := range []string{"status=done", "limit=0", "limit=101", "workspace_id=theirs"} {
		if rec := serveIncident(r, http.MethodGet, "/v1/release-reports?"+q, ""); rec.Code < 400 {
			t.Errorf("%s: %d, want an error", q, rec.Code)
		}
	}
}

func TestWorkflowVersionsCarryVerdicts(t *testing.T) {
	repo := &mocks.MockMetricsRepository{Versions: []model.WorkflowVersion{{Version: "v3"}, {Version: "v2"}, {Version: "v1"}, {Version: "v9"}}}
	rec := serveIncident(releaseRouter(repo), http.MethodGet, "/v1/metrics/workflows/wf/versions", "")
	var got struct {
		Items []model.WorkflowVersion `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var verdicts []string
	for _, v := range got.Items {
		verdicts = append(verdicts, v.ReportVerdict)
	}
	if want := []string{"pending", "regressed", "", ""}; !slices.Equal(verdicts, want) {
		t.Errorf("verdicts = %q, want %q", verdicts, want)
	}
}
