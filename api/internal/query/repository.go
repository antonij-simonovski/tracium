package query

import (
	"context"
	"errors"

	"github.com/tracium/api/internal/model"
)

// TraceRepository is the primary data access interface for the API.
// All handlers depend on this interface, never on a concrete database type.
type TraceRepository interface {
	ListTraces(ctx context.Context, filter TraceFilter) ([]model.Trace, int64, error)
	GetTrace(ctx context.Context, traceID string, workspaceIDs []string) (*model.Trace, error)
	GetSpans(ctx context.Context, traceID string, workspaceIDs []string) ([]model.Span, error)
}

// MetricsRepository serves the aggregated metrics powering the dashboard
// overview. Like TraceRepository, handlers depend only on this interface.
type MetricsRepository interface {
	OverviewKPIs(ctx context.Context, f MetricsFilter) (model.KPISet, error)
	CostSeries(ctx context.Context, f MetricsFilter) ([]model.CostPoint, error)
	LatencySeries(ctx context.Context, f MetricsFilter) ([]model.LatencyPoint, error)
	ErrorSeries(ctx context.Context, f MetricsFilter) ([]model.ErrorPoint, error)
	TokenSeries(ctx context.Context, f MetricsFilter) ([]model.TokenPoint, error)
	TopWorkflows(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowCost, error)
	ListWorkflows(ctx context.Context, f MetricsFilter, limit int) ([]model.Workflow, error)
	// WorkflowDetail serves one workflow's detail page (f.Workflow names it). Returns
	// ErrNotFound when that workflow has no runs in the window.
	WorkflowDetail(ctx context.Context, f MetricsFilter) (model.WorkflowDetail, error)
	// WorkflowVersions lists the versions of f.Workflow that ran in the window.
	WorkflowVersions(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowVersion, error)
	// CompareVersions compares two versions of f.Workflow over the window.
	// Returns ErrNotFound when either version has no runs in it.
	CompareVersions(ctx context.Context, f MetricsFilter, base, candidate string) (model.VersionComparison, error)
	Failures(ctx context.Context, f MetricsFilter, limit int) ([]model.Failure, int64, error)
	ModelCosts(ctx context.Context, f MetricsFilter, limit int) ([]model.ModelCost, error)
	UserUsage(ctx context.Context, f MetricsFilter, limit int) ([]model.UserUsage, error)
	WorkflowUsage(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowUsage, error)
	// AttributeKeys lists the distinct custom-attribute keys present in the
	// window, so a caller can discover which dimensions it can allocate by.
	AttributeKeys(ctx context.Context, f MetricsFilter, limit int) ([]string, error)
	// UsageByAttribute groups spend/usage by the value of one custom attribute
	// key — the allocation primitive: cost by team, by user, by environment, …
	UsageByAttribute(ctx context.Context, f MetricsFilter, key string, limit int) ([]model.AttributeUsage, error)
	// Anomalies detects statistically significant deviations (cost, error rate,
	// run volume) over the window, workspace-wide and per busy workflow. Daily and
	// rollup-backed; f.AnomalyMetric / f.AnomalyMinSeverity narrow the output.
	Anomalies(ctx context.Context, f MetricsFilter) ([]model.Anomaly, error)
	SetupChecks(ctx context.Context, f MetricsFilter) ([]model.SetupCheck, error)
}

// Repository is the full data-access surface a backend must provide. Concrete
// stores (ClickHouse, the no-op stub) implement it; handlers depend only on the
// narrower TraceRepository / MetricsRepository interfaces.
type Repository interface {
	TraceRepository
	MetricsRepository
}

// ErrNotFound is the sentinel error returned when a requested resource does not exist.
// Handlers check for this specifically to return 404 vs 500.
var ErrNotFound = errors.New("not found")
