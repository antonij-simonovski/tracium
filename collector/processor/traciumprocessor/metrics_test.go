package traciumprocessor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tracium/collector/internal/genai"
	"github.com/tracium/collector/internal/ingest"
	"github.com/tracium/collector/internal/pricing"
	"github.com/tracium/collector/internal/user"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// newMetricsProcessor wires the processor with the OSS-style static pricing and
// passthrough user resolution used in production.
func newMetricsProcessor() *metricsProcessor {
	return &metricsProcessor{
		logger:  zap.NewNop(),
		pricing: pricing.NewStaticResolver(pricing.DefaultPrices()),
		user:    user.Passthrough{},
	}
}

// metricsCtx is an authenticated ingest context: the traciumauth extension has
// attached a verified key's workspace. processMetrics now requires it before it
// will price or store any token-usage point (see the keyless backstop), so every
// enrichment test runs under it.
func metricsCtx() context.Context { return ctxWithAuth(fakeAuth{workspace: "ws-1"}) }

// addUsagePoint appends one gen_ai.client.token.usage histogram point.
func addUsagePoint(metrics pmetric.MetricSlice, tokenType, model string, tokens float64) pmetric.HistogramDataPoint {
	m := metrics.AppendEmpty()
	m.SetName(genai.TokenUsageMetric)
	dp := m.SetEmptyHistogram().DataPoints().AppendEmpty()
	dp.SetSum(tokens)
	dp.Attributes().PutStr(genai.AttrTokenType, tokenType)
	dp.Attributes().PutStr(attrModelResponse, model)
	return dp
}

// The processor prices input/output token-usage points independently and stamps
// cost, normalised model, and the resolved user onto each.
func TestProcessMetrics_PricesAndStampsTokenUsage(t *testing.T) {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr(attrUserID, "acme-corp")
	metrics := rm.ScopeMetrics().AppendEmpty().Metrics()

	// gpt-4o: $2.50/1M in, $10/1M out (DefaultPrices). Model arrives mixed-case.
	in := addUsagePoint(metrics, "input", "GPT-4o", 1000)
	out := addUsagePoint(metrics, "output", "GPT-4o", 500)

	if _, err := newMetricsProcessor().processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	assertCost(t, in, 0.0025) // 1000 * 0.0000025
	assertCost(t, out, 0.005) // 500 * 0.00001
	assertStr(t, in, attrModelNormalized, "gpt-4o")
	assertStr(t, in, attrUserID, "acme-corp")
	assertStr(t, out, attrUserID, "acme-corp")
	// The ingest key decides the workspace, exactly as for spans.
	assertStr(t, in, attrWorkspaceID, "ws-1")
	assertStr(t, out, attrWorkspaceID, "ws-1")
}

// A request that reached the processor without a verified ingest key has every
// token-usage point dropped — the same fail-closed backstop the traces path
// applies to keyless spans. Without a key the workspace is unknown, so the usage
// must not be priced or stored even if the receiver authenticator is absent.
func TestProcessMetrics_DropsUnauthenticatedTokenUsage(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	p := newMetricsProcessor()
	p.logger = zap.New(core)

	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	addUsagePoint(metrics, "input", "gpt-4o", 1000)

	// context.Background(): no verified key on the request.
	if _, err := p.processMetrics(context.Background(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	if metrics.Len() != 0 {
		t.Errorf("keyless token-usage metric survived: %d metrics left", metrics.Len())
	}
	if logs.Len() != 1 {
		t.Fatalf("got %d warnings, want exactly 1 — the drop must be visible", logs.Len())
	}
}

// Non-token-usage metrics are passed through untouched (no tracium.* stamps).
func TestProcessMetrics_IgnoresOtherInstruments(t *testing.T) {
	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	m := metrics.AppendEmpty()
	m.SetName("gen_ai.client.operation.duration")
	dp := m.SetEmptyHistogram().DataPoints().AppendEmpty()
	dp.SetSum(1.5)

	if _, err := newMetricsProcessor().processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	if _, ok := dp.Attributes().Get(attrCostUSD); ok {
		t.Error("non-token-usage metric must not be priced")
	}
}

// An unknown model is non-fatal: cost is stamped as 0 (matching the span path),
// the point is still enriched rather than dropped.
func TestProcessMetrics_UnknownModelCostsZero(t *testing.T) {
	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	dp := addUsagePoint(metrics, "input", "mystery-model", 1000)

	if _, err := newMetricsProcessor().processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	assertCost(t, dp, 0)
	assertStr(t, dp, attrModelNormalized, "mystery-model")
}

// addUsageSum appends one gen_ai.client.token.usage sum metric with an explicit
// temporality — the shape a plain OTel SDK counter exports.
func addUsageSum(metrics pmetric.MetricSlice, temp pmetric.AggregationTemporality, tokens int64) pmetric.Metric {
	m := metrics.AppendEmpty()
	m.SetName(genai.TokenUsageMetric)
	sum := m.SetEmptySum()
	sum.SetAggregationTemporality(temp)
	dp := sum.DataPoints().AppendEmpty()
	dp.SetIntValue(tokens)
	dp.Attributes().PutStr(genai.AttrTokenType, "input")
	dp.Attributes().PutStr(attrModelResponse, "gpt-4o")
	return m
}

// BUGS.md 1f. Cumulative points are running totals; summing one row per export
// inflates usage by (N+1)/2. They are dropped, not enriched — and the drop is
// logged so the gap is visible rather than silent.
func TestProcessMetrics_DropsCumulativeTokenUsage(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	p := newMetricsProcessor()
	p.logger = zap.New(core)

	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	addUsageSum(metrics, pmetric.AggregationTemporalityCumulative, 1000)

	if _, err := p.processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	if metrics.Len() != 0 {
		t.Errorf("cumulative token-usage metric survived: %d metrics left", metrics.Len())
	}
	if logs.Len() != 1 {
		t.Fatalf("got %d warnings, want exactly 1 — the rejection must be visible", logs.Len())
	}
	if got := logs.All()[0].ContextMap()["dropped_data_points"]; got != int64(1) {
		t.Errorf("dropped_data_points = %v, want 1", got)
	}
}

// Delta points are per-interval increments: kept and priced as usual.
func TestProcessMetrics_KeepsDeltaTokenUsage(t *testing.T) {
	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	m := addUsageSum(metrics, pmetric.AggregationTemporalityDelta, 1000)

	if _, err := newMetricsProcessor().processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	if metrics.Len() != 1 {
		t.Fatalf("delta token-usage metric was dropped")
	}
	// gpt-4o: $2.50/1M input tokens.
	v, ok := m.Sum().DataPoints().At(0).Attributes().Get(attrCostUSD)
	if !ok || v.Double() != 0.0025 {
		t.Errorf("cost = %v (present=%v), want 0.0025", v.Double(), ok)
	}
}

// A cumulative histogram — OpenLLMetry's instrument shape — is rejected too.
func TestProcessMetrics_DropsCumulativeHistogram(t *testing.T) {
	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	addUsagePoint(metrics, "input", "gpt-4o", 1000)
	metrics.At(0).Histogram().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

	if _, err := newMetricsProcessor().processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	if metrics.Len() != 0 {
		t.Error("cumulative token-usage histogram survived")
	}
}

func TestProcessMetrics_DropsExponentialHistogram(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	p := newMetricsProcessor()
	p.logger = zap.New(core)

	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	m := metrics.AppendEmpty()
	m.SetName(genai.TokenUsageMetric)
	m.SetEmptyExponentialHistogram().DataPoints().AppendEmpty().SetSum(1000)

	if _, err := p.processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	if metrics.Len() != 0 {
		t.Error("exponential-histogram token-usage metric survived")
	}
	if logs.Len() != 1 {
		t.Fatalf("got %d warnings, want exactly 1", logs.Len())
	}
}

type failingUserResolver struct{}

func (failingUserResolver) Resolve(context.Context, string) (string, error) {
	return "", errors.New("lookup down")
}

func TestProcessMetrics_DropsPointOnUserLookupFailure(t *testing.T) {
	p := newMetricsProcessor()
	p.user = failingUserResolver{}

	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	dp := addUsagePoint(metrics, "input", "gpt-4o", 1000)
	dp.Attributes().PutStr(attrUserID, "acme")

	if _, err := p.processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}
	if got := metrics.At(0).Histogram().DataPoints().Len(); got != 0 {
		t.Fatalf("point with failed user lookup survived: %d points left", got)
	}
}

// A token-usage point above the ingest ceiling is dropped, not priced: the
// metrics path must apply the same bound as the span chain, or a single point
// with 1e15 tokens would be priced at billions and corrupt every cost aggregate.
func TestProcessMetrics_DropsTokenCountOutOfRange(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	p := newMetricsProcessor()
	p.logger = zap.New(core)

	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	// 1e15 tokens for gpt-4o — the report's reproduction.
	addUsagePoint(metrics, "input", "gpt-4o", 1e15)

	if _, err := p.processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	// The metric stays but its lone out-of-range point is removed.
	if got := metrics.At(0).Histogram().DataPoints().Len(); got != 0 {
		t.Fatalf("out-of-range point survived: %d points left", got)
	}
	if logs.Len() != 1 {
		t.Fatalf("got %d warnings, want exactly 1 — the drop must be visible", logs.Len())
	}
}

// An oversized, control-character-laden user label is sanitized and length-capped
// before it is stamped, matching the span chain's UserEnricher.
func TestProcessMetrics_CapsOversizedUserLabel(t *testing.T) {
	md := pmetric.NewMetrics()
	metrics := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics()
	dp := addUsagePoint(metrics, "input", "gpt-4o", 1000)
	dp.Attributes().PutStr(attrUserID, strings.Repeat("a", 4096))

	if _, err := newMetricsProcessor().processMetrics(metricsCtx(), md); err != nil {
		t.Fatalf("processMetrics: %v", err)
	}

	v, ok := dp.Attributes().Get(attrUserID)
	if !ok {
		t.Fatal("user id not stamped")
	}
	if len(v.AsString()) > ingest.MaxUserIDBytes {
		t.Errorf("user label = %d bytes, want <= %d", len(v.AsString()), ingest.MaxUserIDBytes)
	}
}

func assertCost(t *testing.T, dp pmetric.HistogramDataPoint, want float64) {
	t.Helper()
	v, ok := dp.Attributes().Get(attrCostUSD)
	if !ok {
		t.Fatalf("%s not stamped", attrCostUSD)
	}
	if got := v.Double(); got != want {
		t.Errorf("cost = %v, want %v", got, want)
	}
}

func assertStr(t *testing.T, dp pmetric.HistogramDataPoint, key, want string) {
	t.Helper()
	v, ok := dp.Attributes().Get(key)
	if !ok {
		t.Fatalf("%s not stamped", key)
	}
	if v.AsString() != want {
		t.Errorf("%s = %q, want %q", key, v.AsString(), want)
	}
}
