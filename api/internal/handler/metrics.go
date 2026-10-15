package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// overviewLimit caps the top-workflows and failures lists; the overview shows five of each.
const overviewLimit = 5

// usageLimit caps the usage-page breakdown lists (models, users, workflows).
const usageLimit = 50

// workflowsLimit caps the Workflows page, which lists every active workflow.
const workflowsLimit = 200

// MetricsHandler serves the aggregated metrics powering the dashboard overview.
type MetricsHandler struct {
	repo   query.MetricsRepository
	access WorkspaceAccess
	now    func() time.Time
}

// NewMetricsHandler constructs a MetricsHandler scoped by the caller's workspace access.
func NewMetricsHandler(repo query.MetricsRepository, access WorkspaceAccess) *MetricsHandler {
	return &MetricsHandler{repo: repo, access: access, now: time.Now}
}

type filterFunc func(http.ResponseWriter, *http.Request) (query.MetricsFilter, bool)

// baseFilter resolves the range, user_id and workspace_id query params, writing
// the error response and returning ok=false when they are invalid.
func (h *MetricsHandler) baseFilter(w http.ResponseWriter, r *http.Request) (query.MetricsFilter, bool) {
	q := r.URL.Query()
	rng := q.Get("range")
	if rng == "" {
		rng = "7d"
	}
	f, err := query.ParseRange(rng, h.now())
	if err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return query.MetricsFilter{}, false
	}
	f.UserID = q.Get("user_id")
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return query.MetricsFilter{}, false
	}
	f.WorkspaceIDs = scope
	return f, true
}

// filter is baseFilter for endpoints that cannot narrow to one workflow or version.
func (h *MetricsHandler) filter(w http.ResponseWriter, r *http.Request) (query.MetricsFilter, bool) {
	q := r.URL.Query()
	if q.Get("workflow") != "" || q.Get("version") != "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "workflow and version filtering are only supported by the series endpoints")
		return query.MetricsFilter{}, false
	}
	return h.baseFilter(w, r)
}

// rawFilter is filter for endpoints that only read raw spans and have no rollup path.
func (h *MetricsHandler) rawFilter(w http.ResponseWriter, r *http.Request) (query.MetricsFilter, bool) {
	f, ok := h.filter(w, r)
	if ok && f.UseRollup() {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "this endpoint is available for ranges up to 30d")
		return query.MetricsFilter{}, false
	}
	return f, ok
}

// seriesFilter is baseFilter plus the optional workflow and version params.
func (h *MetricsHandler) seriesFilter(w http.ResponseWriter, r *http.Request) (query.MetricsFilter, bool) {
	f, ok := h.baseFilter(w, r)
	if !ok {
		return f, false
	}
	f.Workflow = r.URL.Query().Get("workflow")
	f.Version = r.URL.Query().Get("version")
	return f, true
}

// workflowFilter is filter plus the {name} path param, naming the workflow.
func workflowFilter(filter filterFunc) filterFunc {
	return func(w http.ResponseWriter, r *http.Request) (query.MetricsFilter, bool) {
		name := chi.URLParam(r, "name")
		if name == "" {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "workflow name is required")
			return query.MetricsFilter{}, false
		}
		f, ok := filter(w, r)
		f.Workflow = name
		return f, ok
	}
}

func serveList[T any](w http.ResponseWriter, r *http.Request, filter filterFunc, what string, load func(context.Context, query.MetricsFilter) ([]T, error)) {
	f, ok := filter(w, r)
	if !ok {
		return
	}
	items, err := load(r.Context(), f)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load "+what)
		return
	}
	respondPage(w, items, len(items), 1, len(items))
}

func withLimit[T any](load func(context.Context, query.MetricsFilter, int) ([]T, error), limit int) func(context.Context, query.MetricsFilter) ([]T, error) {
	return func(ctx context.Context, f query.MetricsFilter) ([]T, error) {
		return load(ctx, f, limit)
	}
}

var validAnomalyMetrics = map[string]bool{"cost": true, "error_rate": true, "runs": true}
var validAnomalySeverities = map[string]bool{"info": true, "warning": true, "critical": true}

// Anomalies handles GET /v1/metrics/anomalies. Detection is daily, so ranges
// shorter than 7d are rejected. Optional `metric` and `min_severity` narrow the results.
func (h *MetricsHandler) Anomalies(w http.ResponseWriter, r *http.Request) {
	f, ok := h.filter(w, r)
	if !ok {
		return
	}
	if f.Bucket < 24*time.Hour {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "anomaly detection requires a range of 7d or longer")
		return
	}
	if m := r.URL.Query().Get("metric"); m != "" {
		if !validAnomalyMetrics[m] {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "metric must be one of cost, error_rate, runs")
			return
		}
		f.AnomalyMetric = m
	}
	if s := r.URL.Query().Get("min_severity"); s != "" {
		if !validAnomalySeverities[s] {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "min_severity must be one of info, warning, critical")
			return
		}
		f.AnomalyMinSeverity = s
	}
	anomalies, err := h.repo.Anomalies(r.Context(), f)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to detect anomalies")
		return
	}
	respondPage(w, anomalies, len(anomalies), 1, len(anomalies))
}

// SetupChecks handles GET /v1/metrics/setup-checks.
func (h *MetricsHandler) SetupChecks(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.rawFilter, "setup checks", h.repo.SetupChecks)
}

// KPIs handles GET /v1/metrics/kpis.
func (h *MetricsHandler) KPIs(w http.ResponseWriter, r *http.Request) {
	f, ok := h.filter(w, r)
	if !ok {
		return
	}
	kpis, err := h.repo.OverviewKPIs(r.Context(), f)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to compute kpis")
		return
	}
	respondJSON(w, http.StatusOK, kpis)
}

// CostSeries handles GET /v1/metrics/cost-series.
func (h *MetricsHandler) CostSeries(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.seriesFilter, "cost series", h.repo.CostSeries)
}

// LatencySeries handles GET /v1/metrics/latency-series.
func (h *MetricsHandler) LatencySeries(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.seriesFilter, "latency series", h.repo.LatencySeries)
}

// ErrorSeries handles GET /v1/metrics/error-series.
func (h *MetricsHandler) ErrorSeries(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.seriesFilter, "error series", h.repo.ErrorSeries)
}

// TokenSeries handles GET /v1/metrics/token-series.
func (h *MetricsHandler) TokenSeries(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.seriesFilter, "token series", h.repo.TokenSeries)
}

// TopWorkflows handles GET /v1/metrics/top-workflows.
func (h *MetricsHandler) TopWorkflows(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.filter, "top workflows", withLimit(h.repo.TopWorkflows, overviewLimit))
}

// Workflows handles GET /v1/metrics/workflows, the Workflows page's activity list.
func (h *MetricsHandler) Workflows(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.filter, "workflows", withLimit(h.repo.ListWorkflows, workflowsLimit))
}

// WorkflowDetail handles GET /v1/metrics/workflows/{name}.
func (h *MetricsHandler) WorkflowDetail(w http.ResponseWriter, r *http.Request) {
	f, ok := workflowFilter(h.filter)(w, r)
	if !ok {
		return
	}
	detail, err := h.repo.WorkflowDetail(r.Context(), f)
	if err != nil {
		if errors.Is(err, query.ErrNotFound) {
			respondError(w, http.StatusNotFound, "WORKFLOW_NOT_FOUND", "workflow not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load workflow")
		return
	}
	respondJSON(w, http.StatusOK, detail)
}

// WorkflowVersions handles GET /v1/metrics/workflows/{name}/versions.
func (h *MetricsHandler) WorkflowVersions(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, workflowFilter(h.filter), "workflow versions", withLimit(h.repo.WorkflowVersions, workflowsLimit))
}

// Failures handles GET /v1/metrics/failures. Total counts every failed run in
// the window, not just the listed workflows.
func (h *MetricsHandler) Failures(w http.ResponseWriter, r *http.Request) {
	f, ok := h.filter(w, r)
	if !ok {
		return
	}
	failures, total, err := h.repo.Failures(r.Context(), f, overviewLimit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load failures")
		return
	}
	respondPage(w, failures, int(total), 1, len(failures))
}

// ModelCosts handles GET /v1/metrics/model-costs.
func (h *MetricsHandler) ModelCosts(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.filter, "model costs", withLimit(h.repo.ModelCosts, usageLimit))
}

// UserUsage handles GET /v1/metrics/usage-users.
func (h *MetricsHandler) UserUsage(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.filter, "user usage", withLimit(h.repo.UserUsage, usageLimit))
}

// WorkflowUsage handles GET /v1/metrics/usage-workflows.
func (h *MetricsHandler) WorkflowUsage(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.filter, "workflow usage", withLimit(h.repo.WorkflowUsage, usageLimit))
}

// AttributeKeys handles GET /v1/metrics/attribute-keys.
func (h *MetricsHandler) AttributeKeys(w http.ResponseWriter, r *http.Request) {
	serveList(w, r, h.rawFilter, "attribute keys", withLimit(h.repo.AttributeKeys, workflowsLimit))
}

// UsageByAttribute handles GET /v1/metrics/usage-by-attribute?key=team.
func (h *MetricsHandler) UsageByAttribute(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "the 'key' query parameter is required")
		return
	}
	serveList(w, r, h.rawFilter, "usage by attribute", func(ctx context.Context, f query.MetricsFilter) ([]model.AttributeUsage, error) {
		return h.repo.UsageByAttribute(ctx, f, key, usageLimit)
	})
}
