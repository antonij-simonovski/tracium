package genai

import "strings"

const (
	// TokenUsageMetric is the GenAI client token-usage instrument.
	TokenUsageMetric = "gen_ai.client.token.usage"
	// AttrTokenType distinguishes input from output token-usage points.
	AttrTokenType = "gen_ai.token.type"

	// ClaudeCodeTokenUsageMetric is Claude Code's (and the Claude Agent SDK's)
	// token counter. Its points name the model under "model" and the token
	// class under "type": input, output, cacheRead or cacheCreation. Claude
	// Code exports it with DELTA temporality by default.
	ClaudeCodeTokenUsageMetric = "claude_code.token.usage"

	claudeCodeAttrModel = "model"
	claudeCodeAttrType  = "type"
)

// TokenClass is the billing class a token-usage data point counts.
type TokenClass int

// Token classes, priced at the model's matching rate.
const (
	TokenInput TokenClass = iota
	TokenOutput
	TokenCacheRead
	TokenCacheWrite
)

// IsTokenUsageMetric reports whether Tracium prices a metric's data points.
func IsTokenUsageMetric(name string) bool {
	return name == TokenUsageMetric || name == ClaudeCodeTokenUsageMetric
}

// OpenLLMetry names the model llm.response.model on metric points.
var metricModelKeys = []string{attrModelResponse, "llm.response.model", attrModelRequest}

// MetricModel returns the model a token-usage data point of the named metric
// names, reading its attributes through get.
func MetricModel(metric string, get func(key string) string) string {
	for _, k := range metricModelKeys {
		if v := get(k); v != "" {
			return v
		}
	}
	if metric == ClaudeCodeTokenUsageMetric {
		return get(claudeCodeAttrModel)
	}
	return ""
}

// PointTokenClass returns the token class a data point of the named metric
// counts. A gen_ai.client.token.usage point is output when gen_ai.token.type
// says so and input otherwise. A claude_code.token.usage point carries one of
// four classes; an unknown class counts as input.
func PointTokenClass(metric string, get func(key string) string) TokenClass {
	if metric == ClaudeCodeTokenUsageMetric {
		switch strings.ToLower(get(claudeCodeAttrType)) {
		case "output":
			return TokenOutput
		case "cacheread":
			return TokenCacheRead
		case "cachecreation":
			return TokenCacheWrite
		}
		return TokenInput
	}
	if IsOutputTokenType(get(AttrTokenType)) {
		return TokenOutput
	}
	return TokenInput
}

// IsOutputTokenType reports whether a gen_ai.token.type value counts output
// tokens. Anything else, including an absent type, counts as input.
func IsOutputTokenType(tokenType string) bool {
	return strings.EqualFold(tokenType, "output")
}
