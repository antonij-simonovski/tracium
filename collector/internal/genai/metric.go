package genai

import "strings"

const (
	// TokenUsageMetric is the GenAI client token-usage instrument.
	TokenUsageMetric = "gen_ai.client.token.usage"
	// AttrTokenType distinguishes input from output token-usage points.
	AttrTokenType = "gen_ai.token.type"
)

// OpenLLMetry names the model llm.response.model on metric points.
var metricModelKeys = []string{attrModelResponse, "llm.response.model", attrModelRequest}

// MetricModel returns the model a token-usage data point names, reading its
// attributes through get.
func MetricModel(get func(key string) string) string {
	for _, k := range metricModelKeys {
		if v := get(k); v != "" {
			return v
		}
	}
	return ""
}

// IsOutputTokenType reports whether a gen_ai.token.type value counts output
// tokens. Anything else, including an absent type, counts as input.
func IsOutputTokenType(tokenType string) bool {
	return strings.EqualFold(tokenType, "output")
}
