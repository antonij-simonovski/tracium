package genai

import "testing"

func TestMetricModelPrecedence(t *testing.T) {
	for _, tc := range []struct {
		attrs map[string]string
		want  string
	}{
		{map[string]string{"gen_ai.response.model": "a", "llm.response.model": "b", "gen_ai.request.model": "c"}, "a"},
		{map[string]string{"llm.response.model": "b", "gen_ai.request.model": "c"}, "b"},
		{map[string]string{"gen_ai.request.model": "c"}, "c"},
		{map[string]string{}, ""},
	} {
		if got := MetricModel(TokenUsageMetric, func(k string) string { return tc.attrs[k] }); got != tc.want {
			t.Errorf("MetricModel(%v) = %q, want %q", tc.attrs, got, tc.want)
		}
	}
}

func TestIsOutputTokenType(t *testing.T) {
	for v, want := range map[string]bool{"output": true, "OUTPUT": true, "input": false, "": false} {
		if got := IsOutputTokenType(v); got != want {
			t.Errorf("IsOutputTokenType(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestMetricModelClaudeCode(t *testing.T) {
	attrs := map[string]string{"model": "claude-haiku-4-5"}
	get := func(k string) string { return attrs[k] }
	if got := MetricModel(ClaudeCodeTokenUsageMetric, get); got != "claude-haiku-4-5" {
		t.Errorf("claude_code model = %q", got)
	}
	// The bare "model" key is Claude Code's alone.
	if got := MetricModel(TokenUsageMetric, get); got != "" {
		t.Errorf("gen_ai metric read bare model: %q", got)
	}
}

func TestPointTokenClass(t *testing.T) {
	for _, tc := range []struct {
		metric, key, value string
		want               TokenClass
	}{
		{ClaudeCodeTokenUsageMetric, "type", "input", TokenInput},
		{ClaudeCodeTokenUsageMetric, "type", "output", TokenOutput},
		{ClaudeCodeTokenUsageMetric, "type", "cacheRead", TokenCacheRead},
		{ClaudeCodeTokenUsageMetric, "type", "cacheCreation", TokenCacheWrite},
		{TokenUsageMetric, AttrTokenType, "output", TokenOutput},
		{TokenUsageMetric, AttrTokenType, "input", TokenInput},
		// gen_ai.client.token.usage has no cache classes; a stray "type" is ignored.
		{TokenUsageMetric, "type", "cacheRead", TokenInput},
	} {
		attrs := map[string]string{tc.key: tc.value}
		if got := PointTokenClass(tc.metric, func(k string) string { return attrs[k] }); got != tc.want {
			t.Errorf("PointTokenClass(%s, %s=%s) = %v, want %v", tc.metric, tc.key, tc.value, got, tc.want)
		}
	}
	if !IsTokenUsageMetric(ClaudeCodeTokenUsageMetric) || !IsTokenUsageMetric(TokenUsageMetric) || IsTokenUsageMetric("claude_code.cost.usage") {
		t.Error("IsTokenUsageMetric")
	}
}
