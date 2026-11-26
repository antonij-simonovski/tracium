package genai

import "testing"

func TestSpanModelPrecedence(t *testing.T) {
	for _, tc := range []struct {
		attrs map[string]string
		want  string
	}{
		// LiteLLM proxy: the request model is the alias, the provider model is real.
		{map[string]string{attrLiteLLMProviderModel: "openai/gpt-4o", attrModelRequest: "my-gpt", attrModelResponse: "gpt-4o-2024-08-06"}, "openai/gpt-4o"},
		{map[string]string{attrModelRequest: "gpt-4o", attrModelResponse: "gpt-4o-2024-08-06"}, "gpt-4o"},
		{map[string]string{attrModelResponse: "gpt-4o-2024-08-06"}, "gpt-4o-2024-08-06"},
		{map[string]string{}, ""},
	} {
		if got := SpanModel(func(k string) string { return tc.attrs[k] }); got != tc.want {
			t.Errorf("SpanModel(%v) = %q, want %q", tc.attrs, got, tc.want)
		}
	}
}
