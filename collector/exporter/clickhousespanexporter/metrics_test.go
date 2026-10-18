package clickhousespanexporter

import (
	"testing"
	"time"

	"github.com/tracium/collector/internal/genai"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// usageRows turns each enriched token-usage data point into a synthetic
// source="metric" span, routing tokens into the input or output field per
// gen_ai.token.type and carrying the cost the processor priced.
func TestUsageRows_BuildsSyntheticSpans(t *testing.T) {
	bucket := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

	m := pmetric.NewMetric()
	m.SetName(genai.TokenUsageMetric)
	h := m.SetEmptyHistogram()

	in := h.DataPoints().AppendEmpty()
	in.SetSum(1000)
	in.SetTimestamp(pcommon.NewTimestampFromTime(bucket))
	in.Attributes().PutStr(genai.AttrTokenType, "input")
	in.Attributes().PutStr(attrModelResponse, "gpt-4o")
	in.Attributes().PutStr(attrModelNormalized, "gpt-4o")
	in.Attributes().PutStr(attrUserID, "acme-corp")
	in.Attributes().PutDouble(attrCostUSD, 0.005)

	out := h.DataPoints().AppendEmpty()
	out.SetSum(500)
	out.SetTimestamp(pcommon.NewTimestampFromTime(bucket))
	out.Attributes().PutStr(genai.AttrTokenType, "output")
	out.Attributes().PutStr(attrModelResponse, "gpt-4o")
	out.Attributes().PutStr(attrModelNormalized, "gpt-4o")
	out.Attributes().PutDouble(attrCostUSD, 0.0075)

	rows := usageRows(m)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	inRow := rows[0]
	if inRow.Source != "metric" {
		t.Errorf("source = %q, want metric", inRow.Source)
	}
	if inRow.InputTokens != 1000 || inRow.OutputTokens != 0 {
		t.Errorf("input row tokens = (%d in, %d out), want (1000, 0)", inRow.InputTokens, inRow.OutputTokens)
	}
	if inRow.CostUSD != 0.005 {
		t.Errorf("input cost = %v, want 0.005", inRow.CostUSD)
	}
	if inRow.ModelNormalized != "gpt-4o" || inRow.UserID != "acme-corp" {
		t.Errorf("input row = %+v, want model gpt-4o user acme-corp", inRow)
	}
	if inRow.StartTimeMs != bucket.UnixMilli() {
		t.Errorf("start_time_ms = %d, want %d", inRow.StartTimeMs, bucket.UnixMilli())
	}

	outRow := rows[1]
	if outRow.OutputTokens != 500 || outRow.InputTokens != 0 {
		t.Errorf("output row tokens = (%d in, %d out), want (0, 500)", outRow.InputTokens, outRow.OutputTokens)
	}
	if outRow.CostUSD != 0.0075 {
		t.Errorf("output cost = %v, want 0.0075", outRow.CostUSD)
	}
}

// Sum-typed token-usage points are handled alongside the histogram shape.
func TestUsageRows_HandlesSumType(t *testing.T) {
	m := pmetric.NewMetric()
	m.SetName(genai.TokenUsageMetric)
	dp := m.SetEmptySum().DataPoints().AppendEmpty()
	dp.SetIntValue(250)
	dp.Attributes().PutStr(genai.AttrTokenType, "input")
	dp.Attributes().PutStr(attrModelNormalized, "gpt-4o-mini")

	rows := usageRows(m)
	if len(rows) != 1 || rows[0].InputTokens != 250 {
		t.Fatalf("got %+v, want one row with 250 input tokens", rows)
	}
}

// BUGS.md 1f. Cumulative points are running totals, so one row per export sums
// to (N+1)/2 times the real usage. The processor drops and logs them upstream;
// isCumulative is the exporter's own guard for pipelines without it.
func TestIsCumulative(t *testing.T) {
	cumulativeSum := pmetric.NewMetric()
	cumulativeSum.SetEmptySum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

	deltaHist := pmetric.NewMetric()
	deltaHist.SetEmptyHistogram().SetAggregationTemporality(pmetric.AggregationTemporalityDelta)

	// Unspecified (temporality never set) is treated as delta — see isCumulative.
	unset := pmetric.NewMetric()
	unset.SetEmptyHistogram()

	for _, tc := range []struct {
		name string
		m    pmetric.Metric
		want bool
	}{
		{"cumulative sum", cumulativeSum, true},
		{"delta histogram", deltaHist, false},
		{"unspecified", unset, false},
	} {
		if got := isCumulative(tc.m); got != tc.want {
			t.Errorf("%s: isCumulative = %v, want %v", tc.name, got, tc.want)
		}
	}
}
