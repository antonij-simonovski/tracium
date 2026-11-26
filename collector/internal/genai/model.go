package genai

// attrLiteLLMProviderModel is the model LiteLLM actually dispatched a call to.
// Through the LiteLLM proxy, gen_ai.request.model holds the caller-facing model
// group name (an alias such as "my-gpt" that no price table knows), and this
// attribute holds the deployment's real model, often provider-prefixed
// ("openai/gpt-4o").
const attrLiteLLMProviderModel = "litellm.provider.model"

// SpanModel returns the model a span is priced and grouped by, reading its
// attributes through get: LiteLLM's dispatched model when present, then
// gen_ai.request.model, then gen_ai.response.model.
func SpanModel(get func(key string) string) string {
	for _, k := range []string{attrLiteLLMProviderModel, attrModelRequest, attrModelResponse} {
		if v := get(k); v != "" {
			return v
		}
	}
	return ""
}
