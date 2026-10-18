package traciumprocessor

import (
	"context"
	"strings"

	"github.com/tracium/collector/internal/genai"
	"github.com/tracium/collector/internal/ingest"
	"github.com/tracium/collector/internal/pricing"
	"github.com/tracium/collector/internal/user"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// metricsProcessor enriches OpenLLMetry's gen_ai.client.token.usage data points
// with the same user resolution, model normalisation, and pricing the traces
// chain applies to spans. It stays in pmetric form and stamps the results as
// data-point attributes; the clickhousespan exporter turns each enriched data
// point into a synthetic span row (source="metric").
//
// Cost is computed per data point. The static price table is linear in tokens,
// so pricing input-only and output-only points separately and summing
// downstream gives the same total as pricing them together.
type metricsProcessor struct {
	logger  *zap.Logger
	pricing pricing.Resolver
	user    user.Resolver
}

// processMetrics resolves user/model/cost for every token-usage data point and
// drops the token-usage metrics and points it cannot store correctly.
// Non-token-usage metrics pass through untouched; the exporter ignores them.
func (p *metricsProcessor) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	scope := ingestScope(ctx)

	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		resourceAttrs := rm.Resource().Attributes()
		sms := rm.ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			sms.At(j).Metrics().RemoveIf(func(m pmetric.Metric) bool {
				if m.Name() != genai.TokenUsageMetric {
					return false
				}
				switch {
				case !scope.authenticated:
					p.logger.Warn("dropped token-usage metric: ingest requires a verified API key",
						zap.String("metric", m.Name()))
				case m.Type() != pmetric.MetricTypeHistogram && m.Type() != pmetric.MetricTypeSum:
					p.logger.Warn("dropped token-usage metric: only histogram and sum instruments are supported",
						zap.String("metric", m.Name()),
						zap.String("type", m.Type().String()))
				case isCumulative(m):
					p.rejectCumulative(m)
				default:
					p.enrichPoints(ctx, m, resourceAttrs, scope.workspace)
					return false
				}
				return true
			})
		}
	}
	return md, nil
}

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

// rejectCumulative logs a dropped cumulative token-usage metric. Every point is
// stored as its own row and summed, which is only correct for DELTA points; a
// running total summed over N exports reports (N+1)/2 times the real usage.
// Converting would need per-series state that is lost on restart and split
// across replicas, so clients are asked to export DELTA instead. Unspecified
// temporality is treated as delta.
func (p *metricsProcessor) rejectCumulative(m pmetric.Metric) {
	points := 0
	switch m.Type() {
	case pmetric.MetricTypeHistogram:
		points = m.Histogram().DataPoints().Len()
	case pmetric.MetricTypeSum:
		points = m.Sum().DataPoints().Len()
	}
	p.logger.Warn("dropped cumulative token-usage metric: only DELTA temporality can be summed correctly",
		zap.String("metric", m.Name()),
		zap.String("temporality", "cumulative"),
		zap.Int("dropped_data_points", points),
		zap.String("fix", "configure the OTel SDK metric exporter for DELTA temporality on sum/histogram instruments"),
	)
}

// enrichPoints enriches every data point of a token-usage metric, removing the
// points enrichPoint rejects.
func (p *metricsProcessor) enrichPoints(ctx context.Context, m pmetric.Metric, resourceAttrs pcommon.Map, workspace string) {
	switch m.Type() {
	case pmetric.MetricTypeHistogram:
		m.Histogram().DataPoints().RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
			return !p.enrichPoint(ctx, m.Name(), dp.Attributes(), int64(dp.Sum()), resourceAttrs, workspace)
		})
	case pmetric.MetricTypeSum:
		m.Sum().DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			return !p.enrichPoint(ctx, m.Name(), dp.Attributes(), dataPointValue(dp), resourceAttrs, workspace)
		})
	}
}

// dataPointValue reads a numeric sum data point as int64 regardless of whether
// it carries an int or double value.
func dataPointValue(dp pmetric.NumberDataPoint) int64 {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeInt:
		return dp.IntValue()
	case pmetric.NumberDataPointValueTypeDouble:
		return int64(dp.DoubleValue())
	default:
		return 0
	}
}

// enrichPoint stamps tracium.* attributes onto a single token-usage point and
// reports whether to keep it. Points with an out-of-range token count, which
// the span chain would also reject, or an unresolvable user are dropped.
func (p *metricsProcessor) enrichPoint(
	ctx context.Context,
	metricName string,
	attrs pcommon.Map,
	tokens int64,
	resourceAttrs pcommon.Map,
	workspace string,
) bool {
	if !ingest.TokensInRange(tokens) {
		p.logger.Warn("dropped token-usage data point: token count out of range",
			zap.String("metric", metricName),
			zap.Int64("tokens", tokens),
			zap.Int64("limit", ingest.MaxTokensPerCall),
		)
		return false
	}
	userID := metricUser(attrs, resourceAttrs)
	if p.user != nil && userID != "" {
		resolved, err := p.user.Resolve(ctx, userID)
		if err != nil {
			p.logger.Warn("dropped token-usage data point: user lookup failed",
				zap.String("metric", metricName), zap.Error(err))
			return false
		}
		userID = resolved
	}
	userID = ingest.SanitizeIdentifier(userID, ingest.MaxUserIDBytes)
	model := genai.MetricModel(func(k string) string { return strAttr(attrs, k) })
	modelNormalized := ingest.SanitizeIdentifier(strings.ToLower(model), ingest.MaxModelNameBytes)

	var cost float64
	if p.pricing != nil && modelNormalized != "" {
		var u pricing.Usage
		if genai.IsOutputTokenType(strAttr(attrs, genai.AttrTokenType)) {
			u.Output = tokens
		} else {
			u.Input = tokens
		}
		if c, err := p.pricing.Resolve(ctx, modelNormalized, u); err == nil {
			cost = c
		}
	}

	attrs.PutDouble(attrCostUSD, cost)
	attrs.PutStr(attrModelNormalized, modelNormalized)
	if userID != "" {
		attrs.PutStr(attrUserID, userID)
	}
	attrs.PutStr(attrWorkspaceID, workspace)
	return true
}

// metricUser reads the user from the data point, falling back to the
// resource — the same precedence spans use.
func metricUser(attrs, resourceAttrs pcommon.Map) string {
	if v := strAttr(attrs, attrUserID); v != "" {
		return v
	}
	return strAttr(resourceAttrs, attrUserID)
}

func strAttr(attrs pcommon.Map, key string) string {
	if v, ok := attrs.Get(key); ok {
		return v.AsString()
	}
	return ""
}
