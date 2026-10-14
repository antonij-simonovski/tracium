package mocks

import (
	"context"
	"slices"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// MockTraceRepository is an in-memory implementation of query.TraceRepository
// for use in handler unit tests. It never touches a real database.
type MockTraceRepository struct {
	Traces   []model.Trace
	SpansMap map[string][]model.Span

	// Configurable errors — set these to simulate failures.
	ListErr error
	GetErr  error
	SpanErr error

	// Call counters — inspect these in tests.
	ListCallCount int
	GetCallCount  int
	SpanCallCount int
}

// NewMockTraceRepository constructs an empty MockTraceRepository.
func NewMockTraceRepository() *MockTraceRepository {
	return &MockTraceRepository{
		SpansMap: make(map[string][]model.Span),
	}
}

// AddTrace adds a trace (and its spans) to the mock store.
func (m *MockTraceRepository) AddTrace(trace model.Trace, spans []model.Span) {
	m.Traces = append(m.Traces, trace)
	m.SpansMap[trace.TraceID] = spans
}

// ListTraces returns all stored traces that match the filter's UserID, along
// with the total number of matches. The mock does not paginate, so the total
// equals the number of returned traces.
func (m *MockTraceRepository) ListTraces(_ context.Context, filter query.TraceFilter) ([]model.Trace, int64, error) {
	m.ListCallCount++
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}

	var result []model.Trace
	for _, t := range m.Traces {
		if t.UserID == filter.UserID {
			result = append(result, t)
		}
	}
	return result, int64(len(result)), nil
}

// GetTrace returns the trace with the given ID, or ErrNotFound.
func (m *MockTraceRepository) GetTrace(_ context.Context, traceID string, workspaceIDs []string) (*model.Trace, error) {
	m.GetCallCount++
	if m.GetErr != nil {
		return nil, m.GetErr
	}

	for _, t := range m.Traces {
		if t.TraceID == traceID && slices.Contains(workspaceIDs, t.WorkspaceID) {
			cp := t
			return &cp, nil
		}
	}
	return nil, query.ErrNotFound
}

// GetSpans returns all spans for the given trace ID, or ErrNotFound.
func (m *MockTraceRepository) GetSpans(_ context.Context, traceID string, workspaceIDs []string) ([]model.Span, error) {
	m.SpanCallCount++
	if m.SpanErr != nil {
		return nil, m.SpanErr
	}

	spans, ok := m.SpansMap[traceID]
	if !ok {
		return nil, query.ErrNotFound
	}
	var scoped []model.Span
	for _, span := range spans {
		if slices.Contains(workspaceIDs, span.WorkspaceID) {
			scoped = append(scoped, span)
		}
	}
	if len(scoped) == 0 {
		return nil, query.ErrNotFound
	}
	return scoped, nil
}

// MockMetricsRepository is an in-memory query.MetricsRepository for handler
// tests. Each method returns its preset field (and the shared Err), so tests
// can assert serialization without a database.
type MockMetricsRepository struct {
	KPIs            model.KPISet
	Cost            []model.CostPoint
	Latency         []model.LatencyPoint
	Errors          []model.ErrorPoint
	Tokens          []model.TokenPoint
	Workflows       []model.WorkflowCost
	WorkflowRows    []model.Workflow
	WorkflowDetailV model.WorkflowDetail
	Versions        []model.WorkflowVersion
	Comparison      model.VersionComparison
	FailureItems    []model.Failure
	FailuresTotal   int64
	Models          []model.ModelCost
	Users           []model.UserUsage
	WorkflowUsages  []model.WorkflowUsage
	AttrKeys        []string
	AttrUsage       []model.AttributeUsage
	AnomalyItems    []model.Anomaly
	Checks          []model.SetupCheck

	Err error
}

func (m *MockMetricsRepository) OverviewKPIs(_ context.Context, _ query.MetricsFilter) (model.KPISet, error) {
	return m.KPIs, m.Err
}

func (m *MockMetricsRepository) CostSeries(_ context.Context, _ query.MetricsFilter) ([]model.CostPoint, error) {
	return m.Cost, m.Err
}

func (m *MockMetricsRepository) LatencySeries(_ context.Context, _ query.MetricsFilter) ([]model.LatencyPoint, error) {
	return m.Latency, m.Err
}

func (m *MockMetricsRepository) ErrorSeries(_ context.Context, _ query.MetricsFilter) ([]model.ErrorPoint, error) {
	return m.Errors, m.Err
}

func (m *MockMetricsRepository) TokenSeries(_ context.Context, _ query.MetricsFilter) ([]model.TokenPoint, error) {
	return m.Tokens, m.Err
}

func (m *MockMetricsRepository) TopWorkflows(_ context.Context, _ query.MetricsFilter, _ int) ([]model.WorkflowCost, error) {
	return m.Workflows, m.Err
}

func (m *MockMetricsRepository) ListWorkflows(_ context.Context, _ query.MetricsFilter, _ int) ([]model.Workflow, error) {
	return m.WorkflowRows, m.Err
}

func (m *MockMetricsRepository) WorkflowDetail(_ context.Context, _ query.MetricsFilter) (model.WorkflowDetail, error) {
	return m.WorkflowDetailV, m.Err
}

func (m *MockMetricsRepository) WorkflowVersions(_ context.Context, _ query.MetricsFilter, _ int) ([]model.WorkflowVersion, error) {
	return m.Versions, m.Err
}

func (m *MockMetricsRepository) CompareVersions(_ context.Context, _ query.MetricsFilter, _, _ string) (model.VersionComparison, error) {
	return m.Comparison, m.Err
}

func (m *MockMetricsRepository) Failures(_ context.Context, _ query.MetricsFilter, _ int) ([]model.Failure, int64, error) {
	return m.FailureItems, m.FailuresTotal, m.Err
}

func (m *MockMetricsRepository) ModelCosts(_ context.Context, _ query.MetricsFilter, _ int) ([]model.ModelCost, error) {
	return m.Models, m.Err
}

func (m *MockMetricsRepository) UserUsage(_ context.Context, _ query.MetricsFilter, _ int) ([]model.UserUsage, error) {
	return m.Users, m.Err
}

func (m *MockMetricsRepository) WorkflowUsage(_ context.Context, _ query.MetricsFilter, _ int) ([]model.WorkflowUsage, error) {
	return m.WorkflowUsages, m.Err
}

func (m *MockMetricsRepository) AttributeKeys(_ context.Context, _ query.MetricsFilter, _ int) ([]string, error) {
	return m.AttrKeys, m.Err
}

func (m *MockMetricsRepository) UsageByAttribute(_ context.Context, _ query.MetricsFilter, _ string, _ int) ([]model.AttributeUsage, error) {
	return m.AttrUsage, m.Err
}

func (m *MockMetricsRepository) Anomalies(_ context.Context, _ query.MetricsFilter) ([]model.Anomaly, error) {
	return m.AnomalyItems, m.Err
}

func (m *MockMetricsRepository) SetupChecks(_ context.Context, _ query.MetricsFilter) ([]model.SetupCheck, error) {
	return m.Checks, m.Err
}
