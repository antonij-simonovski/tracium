package pricing

import "context"

// Usage carries the token counts needed to price one LLM call, exactly as the
// instrumentation reported them. Output is the provider total (already including
// any reasoning tokens, by OTel convention). CacheRead and CacheCreation are the
// tokens served from / written to a provider-managed prompt cache, which most
// providers bill at rates different from ordinary input tokens.
//
// Whether Input counts the cached tokens depends on the provider: OpenAI's
// prompt_tokens includes them, Anthropic's input_tokens does not. Resolvers must
// not assume either — StaticResolver reconciles the two in Usage.normalize.
type Usage struct {
	Input         int64
	Output        int64
	CacheRead     int64
	CacheCreation int64
}

// Resolver computes the USD cost of a single span given the model and token
// usage. Implementations must be safe for concurrent use.
type Resolver interface {
	// Resolve returns the cost in USD for the given model and token usage, or an
	// error when the model cannot be priced.
	Resolve(ctx context.Context, model string, usage Usage) (float64, error)
}
