package clickhousespanexporter

import (
	"context"

	"github.com/tracium/collector/internal/genai"
	"github.com/tracium/collector/internal/ingest"
	"github.com/tracium/collector/pkg/spanmodel"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// pushMetrics turns each enriched gen_ai.client.token.usage data point into a
// synthetic span row (source="metric") and writes them in one batch to the same
// tracium.spans table as real spans. Each data point measures either input or
// output tokens (per gen_ai.token.type); cost was priced accordingly upstream,
// so summing rows downstream yields correct per-window totals. Non-token-usage
// metrics are ignored.
func (e *chExporter) pushMetrics(ctx context.Context, md pmetric.Metrics) error {
	var rows []*spanmodel.Span

	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			metrics := sms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				m := metrics.At(k)
				if m.Name() != genai.TokenUsageMetric || isCumulative(m) {
					continue
				}
				rows = append(rows, usageRows(m)...)
			}
		}
	}

	return e.writer.WriteBatch(ctx, rows)
}

// isCumulative reports whether a metric's points are running totals, which
// summed per row would count every earlier interval again. The tracium
// processor drops and logs these; this guards pipelines without it.
func isCumulative(m pmetric.Metric) bool {
	switch m.Type() {
	case pmetric.MetricTypeHistogram:
		return m.Histogram().AggregationTemporality() == pmetric.AggregationTemporalityCumulative
	case pmetric.MetricTypeSum:
		return m.Sum().AggregationTemporality() == pmetric.AggregationTemporalityCumulative
	default:
		return false
	}
}

// usageRows builds one synthetic span per data point of a token-usage metric,
// handling both the histogram (OpenLLMetry) and sum shapes. Data points whose
// token count is out of range are skipped — a defense-in-depth guard mirroring
// the span chain's ceiling, so the exporter never writes a row priced on an
// absurd token count even if it runs in a pipeline without the tracium processor
// (the processor already drops these upstream).
func usageRows(m pmetric.Metric) []*spanmodel.Span {
	var rows []*spanmodel.Span
	switch m.Type() {
	case pmetric.MetricTypeHistogram:
		dps := m.Histogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dp := dps.At(i)
			if row := usageRow(dp.Attributes(), int64(dp.Sum()), dp.Timestamp()); row != nil {
				rows = append(rows, row)
			}
		}
	case pmetric.MetricTypeSum:
		dps := m.Sum().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dp := dps.At(i)
			if row := usageRow(dp.Attributes(), numberValue(dp), dp.Timestamp()); row != nil {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

// usageRow assembles the synthetic span for one token-usage data point. Input
// vs output tokens land in the matching field so the query layer can sum each
// independently. trace/span IDs and name are left empty — metric rows are
// aggregates, never shown as individual traces (source-aware queries exclude
// them from trace-shaped aggregates).
//
// Returns nil (the point is skipped) when the token count is out of range, and
// length-caps the client-controlled identifiers, so a row written by the
// exporter is bounded the same way a span row is regardless of what ran upstream.
func usageRow(attrs pcommon.Map, tokens int64, ts pcommon.Timestamp) *spanmodel.Span {
	if !ingest.TokensInRange(tokens) {
		return nil
	}

	bucketMs := ts.AsTime().UnixMilli()
	model := genai.MetricModel(func(k string) string { return strAttr(attrs, k) })

	span := &spanmodel.Span{
		Source:          "metric",
		StartTimeMs:     bucketMs,
		EndTimeMs:       bucketMs,
		Model:           ingest.SanitizeIdentifier(model, ingest.MaxModelNameBytes),
		ModelNormalized: ingest.SanitizeIdentifier(strAttr(attrs, attrModelNormalized), ingest.MaxModelNameBytes),
		CostUSD:         floatAttr(attrs, attrCostUSD),
		UserID:          ingest.SanitizeIdentifier(strAttr(attrs, attrUserID), ingest.MaxUserIDBytes),
		WorkspaceID:     ingest.SanitizeIdentifier(strAttr(attrs, attrWorkspaceID), ingest.MaxUserIDBytes),
		SchemaVersion:   1,
	}
	if genai.IsOutputTokenType(strAttr(attrs, genai.AttrTokenType)) {
		span.OutputTokens = tokens
	} else {
		span.InputTokens = tokens
	}
	return span
}

func numberValue(dp pmetric.NumberDataPoint) int64 {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeInt:
		return dp.IntValue()
	case pmetric.NumberDataPointValueTypeDouble:
		return int64(dp.DoubleValue())
	default:
		return 0
	}
}
