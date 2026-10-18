package pricing

import (
	"context"
	"fmt"
	"strings"
)

// Rates is the per-token USD price for each class of token in a single call.
// A zero cache rate means the provider does not price that class separately, so
// those tokens are billed at InputPerToken. A zero rate on a Tier means "inherit
// the base rate" (see ModelPrice.ratesFor).
type Rates struct {
	InputPerToken         float64
	OutputPerToken        float64
	CacheReadPerToken     float64
	CacheCreationPerToken float64
}

// Tier overrides the base rates once a call's input token count exceeds
// AboveInputTokens. Long-context models (e.g. Gemini, long-context Claude) bill
// the whole call at the higher rate once the prompt crosses the threshold, so a
// tier replaces — not adds to — the base rates for that call.
type Tier struct {
	AboveInputTokens int64
	Rates
}

// ModelPrice is the full price schedule for one model: the base rates plus any
// long-context tiers, ordered ascending by threshold.
type ModelPrice struct {
	Rates
	Tiers []Tier
}

// StaticResolver resolves pricing from an in-memory map loaded at startup.
// It never makes network calls after construction.
type StaticResolver struct {
	prices map[string]ModelPrice
}

// NewStaticResolver constructs a StaticResolver from the given price map. Keys
// are matched against the normalized (lower-cased) model name.
func NewStaticResolver(prices map[string]ModelPrice) *StaticResolver {
	return &StaticResolver{prices: prices}
}

// Resolve returns the USD cost for the given model and token usage, applying the
// cache-token and long-context tier pricing the provider would. Returns an error
// if the model is not found in the price map.
func (s *StaticResolver) Resolve(_ context.Context, model string, u Usage) (float64, error) {
	p, ok := s.lookup(model)
	if !ok {
		return 0, fmt.Errorf("no pricing found for model %q", model)
	}
	billable, promptTokens := u.normalize()
	return p.ratesFor(promptTokens).cost(billable), nil
}

// normalize reconciles the two conventions instrumentations use for
// gen_ai.usage.input_tokens and returns the billable split: a Usage whose Input
// holds only the regular (full-price) tokens, plus the call's total prompt size
// for tier selection.
//
//   - Inclusive (OpenAI-style): input_tokens already counts the cached tokens,
//     so the regular tokens are what remains after subtracting them.
//   - Exclusive (Anthropic-style): input_tokens counts only the uncached tokens,
//     so it is already the regular count and the real prompt is larger than it
//     reports.
//
// The two are distinguishable: cached tokens can only exceed input_tokens under
// the exclusive reading. A tie reads as inclusive — a fully-cached OpenAI prompt
// is routine, whereas exact equality under Anthropic's is coincidence. Negative
// counts from malformed instrumentation are floored at zero so no call can price
// below zero.
func (u Usage) normalize() (billable Usage, promptTokens int64) {
	u.Input = max(u.Input, 0)
	u.Output = max(u.Output, 0)
	u.CacheRead = max(u.CacheRead, 0)
	u.CacheCreation = max(u.CacheCreation, 0)

	cached := u.CacheRead + u.CacheCreation
	if u.Input >= cached {
		return Usage{
			Input: u.Input - cached, Output: u.Output,
			CacheRead: u.CacheRead, CacheCreation: u.CacheCreation,
		}, u.Input
	}
	return u, u.Input + cached
}

// lookup finds a model's price, trying progressively more permissive forms of
// the name so provider-prefixed and cross-region ids (Bedrock, Vertex, Azure, …)
// still resolve to their base model's price.
func (s *StaticResolver) lookup(model string) (ModelPrice, bool) {
	for _, key := range candidateKeys(model) {
		if p, ok := s.prices[key]; ok {
			return p, true
		}
	}
	return ModelPrice{}, false
}

// candidateKeys expands a model name into the ordered list of keys to try
// against the price table, most specific first. The table already stores full
// provider ids (e.g. "anthropic.claude-3-5-sonnet-20241022-v2:0"), so most
// lookups hit on the first candidate; the fallbacks cover routing prefixes and
// cross-region inference profiles the raw table does not key on.
func candidateKeys(model string) []string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	add := func(k string) {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	add(model)
	// "provider/model" routing prefix → bare model (openai/gpt-4o → gpt-4o).
	if i := strings.Index(model, "/"); i >= 0 {
		add(model[i+1:])
	}
	// Bedrock cross-region inference prefix (us./eu./apac./us-gov.) → bare id.
	for _, region := range []string{"us.", "eu.", "apac.", "us-gov."} {
		if rest := strings.TrimPrefix(model, region); rest != model {
			add(rest)
			break
		}
	}
	return keys
}

// ratesFor returns the effective rates for a call whose total prompt is
// promptTokens, applying the highest tier whose threshold the prompt exceeds.
// The threshold is measured against the whole prompt — cached tokens included,
// since providers size the long-context premium on what the model actually reads.
// Tiers are stored ascending, so the last matching tier wins; a tier's
// zero-valued fields inherit the base rate.
func (p ModelPrice) ratesFor(promptTokens int64) Rates {
	r := p.Rates
	for _, t := range p.Tiers {
		if promptTokens > t.AboveInputTokens {
			r = mergeRates(p.Rates, t.Rates)
		}
	}
	return r
}

// mergeRates overlays a tier's non-zero rates onto the base rates, leaving base
// values in place where the tier does not override.
func mergeRates(base, over Rates) Rates {
	if over.InputPerToken == 0 {
		over.InputPerToken = base.InputPerToken
	}
	if over.OutputPerToken == 0 {
		over.OutputPerToken = base.OutputPerToken
	}
	if over.CacheReadPerToken == 0 {
		over.CacheReadPerToken = base.CacheReadPerToken
	}
	if over.CacheCreationPerToken == 0 {
		over.CacheCreationPerToken = base.CacheCreationPerToken
	}
	return over
}

// cost prices one call at the given rates. u.Input must already hold only the
// regular (non-cached) tokens — see Usage.normalize. Cached tokens are billed at
// their own rate, falling back to the input rate when the model prices no cache
// class. Reasoning tokens are already counted within Output by convention, so
// they need no separate handling here.
func (r Rates) cost(u Usage) float64 {
	readRate := r.CacheReadPerToken
	if readRate == 0 {
		readRate = r.InputPerToken
	}
	writeRate := r.CacheCreationPerToken
	if writeRate == 0 {
		writeRate = r.InputPerToken
	}
	return float64(u.Input)*r.InputPerToken +
		float64(u.CacheRead)*readRate +
		float64(u.CacheCreation)*writeRate +
		float64(u.Output)*r.OutputPerToken
}

// DefaultPrices returns a small per-token price map used as an offline fallback
// when no pricing file is configured. Production deployments load the full
// table from tracium-spec/pricing/pricing.json (see LoadStaticFile), which is
// generated from LiteLLM and covers every major provider.
func DefaultPrices() map[string]ModelPrice {
	return map[string]ModelPrice{
		"gpt-4o": {Rates: Rates{
			InputPerToken: 0.0000025, OutputPerToken: 0.00001, CacheReadPerToken: 0.00000125,
		}},
		"gpt-4o-mini": {Rates: Rates{
			InputPerToken: 0.00000015, OutputPerToken: 0.0000006, CacheReadPerToken: 0.000000075,
		}},
		"gpt-4-turbo": {Rates: Rates{
			InputPerToken: 0.00001, OutputPerToken: 0.00003,
		}},
		"gpt-3.5-turbo": {Rates: Rates{
			InputPerToken: 0.0000005, OutputPerToken: 0.0000015,
		}},
		"claude-sonnet-4-6": {Rates: Rates{
			InputPerToken: 0.000003, OutputPerToken: 0.000015,
			CacheReadPerToken: 0.0000003, CacheCreationPerToken: 0.00000375,
		}},
		"claude-haiku-4-5": {Rates: Rates{
			InputPerToken: 0.000001, OutputPerToken: 0.000005,
			CacheReadPerToken: 0.0000001, CacheCreationPerToken: 0.00000125,
		}},
		"claude-3-opus": {Rates: Rates{
			InputPerToken: 0.000015, OutputPerToken: 0.000075,
		}},
	}
}
