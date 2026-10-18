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
		if got := MetricModel(func(k string) string { return tc.attrs[k] }); got != tc.want {
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
