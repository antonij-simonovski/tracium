package pricing

import (
	"context"
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestStaticResolver_Cost(t *testing.T) {
	prices := map[string]ModelPrice{
		"gpt-4o": {Rates: Rates{
			InputPerToken: 0.0000025, OutputPerToken: 0.00001, CacheReadPerToken: 0.00000125,
		}},
		// No cache rate: cached tokens must fall back to the input rate.
		"plain": {Rates: Rates{InputPerToken: 0.000001, OutputPerToken: 0.000002}},
		// Long-context model: whole call re-priced above the threshold.
		"gemini": {
			Rates: Rates{InputPerToken: 0.00000125, OutputPerToken: 0.00001},
			Tiers: []Tier{{AboveInputTokens: 200000, Rates: Rates{
				InputPerToken: 0.0000025, OutputPerToken: 0.000015,
			}}},
		},
	}
	r := NewStaticResolver(prices)
	ctx := context.Background()

	tests := []struct {
		name  string
		model string
		usage Usage
		want  float64
	}{
		{
			name:  "plain input+output",
			model: "gpt-4o",
			usage: Usage{Input: 1000, Output: 500},
			want:  1000*0.0000025 + 500*0.00001,
		},
		{
			name:  "cached read billed at cache rate, remainder at input rate",
			model: "gpt-4o",
			// Input=1000 total, of which 800 served from cache.
			usage: Usage{Input: 1000, Output: 200, CacheRead: 800},
			want:  200*0.0000025 + 800*0.00000125 + 200*0.00001,
		},
		{
			name:  "cache falls back to input rate when unpriced",
			model: "plain",
			usage: Usage{Input: 1000, Output: 0, CacheRead: 400},
			want:  1000 * 0.000001, // 600 regular + 400 cached, both at input rate
		},
		{
			name:  "below tier threshold uses base rates",
			model: "gemini",
			usage: Usage{Input: 100000, Output: 1000},
			want:  100000*0.00000125 + 1000*0.00001,
		},
		{
			name:  "above tier threshold re-prices the whole call",
			model: "gemini",
			usage: Usage{Input: 300000, Output: 1000},
			want:  300000*0.0000025 + 1000*0.000015,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Resolve(ctx, tt.model, tt.usage)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !approx(got, tt.want) {
				t.Errorf("cost = %v, want %v", got, tt.want)
			}
		})
	}
}

// Instrumentations disagree on whether gen_ai.usage.input_tokens counts cached
// tokens: OpenAI's prompt_tokens does, Anthropic's input_tokens does not. Both
// readings must price to the same real-world cost.
func TestStaticResolver_CacheTokenConventions(t *testing.T) {
	// Rates mirror Claude Sonnet 4.5: a >200k tier that doubles the whole call.
	const in, out, read, write = 0.000003, 0.000015, 0.0000003, 0.00000375
	const tierIn, tierOut, tierRead = 0.000006, 0.0000225, 0.0000006
	prices := map[string]ModelPrice{"claude": {
		Rates: Rates{InputPerToken: in, OutputPerToken: out,
			CacheReadPerToken: read, CacheCreationPerToken: write},
		Tiers: []Tier{{AboveInputTokens: 200000, Rates: Rates{
			InputPerToken: tierIn, OutputPerToken: tierOut,
			CacheReadPerToken: tierRead, CacheCreationPerToken: 0.0000075,
		}}},
	}}
	r := NewStaticResolver(prices)
	ctx := context.Background()

	tests := []struct {
		name  string
		usage Usage
		want  float64
	}{
		{
			// OpenAI-style: Input is the whole prompt, cached tokens included.
			name:  "inclusive convention subtracts cached from input",
			usage: Usage{Input: 2059, Output: 100, CacheRead: 2048},
			want:  11*in + 2048*read + 100*out,
		},
		{
			// Anthropic-style: Input counts only the 10 uncached tokens.
			name:  "exclusive convention keeps input as regular tokens",
			usage: Usage{Input: 10, Output: 100, CacheCreation: 2051},
			want:  10*in + 2051*write + 100*out,
		},
		{
			// A fully-cached prompt: ambiguous, but reads as inclusive.
			name:  "cached equal to input reads as inclusive",
			usage: Usage{Input: 2048, Output: 100, CacheRead: 2048},
			want:  2048*read + 100*out,
		},
		{
			// The prompt really is 250,010 tokens, so the >200k tier applies —
			// even though input_tokens alone reports only 10.
			name:  "tier fires on total prompt size, not uncached input",
			usage: Usage{Input: 10, Output: 1000, CacheRead: 250000},
			want:  10*tierIn + 250000*tierRead + 1000*tierOut,
		},
		{
			name:  "negative counts floor at zero rather than crediting cost",
			usage: Usage{Input: -5, Output: -10, CacheRead: -1},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Resolve(ctx, "claude", tt.usage)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !approx(got, tt.want) {
				t.Errorf("cost = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStaticResolver_UnknownModelErrors(t *testing.T) {
	r := NewStaticResolver(map[string]ModelPrice{"gpt-4o": {Rates: Rates{InputPerToken: 1}}})
	if _, err := r.Resolve(context.Background(), "mystery", Usage{Input: 10}); err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestStaticResolver_Limits(t *testing.T) {
	r := NewStaticResolver(map[string]ModelPrice{
		"gpt-4o":  {MaxInputTokens: 128000, MaxOutputTokens: 16384},
		"no-size": {Rates: Rates{InputPerToken: 1}},
	})
	if in, out, ok := r.Limits("openai/gpt-4o"); !ok || in != 128000 || out != 16384 {
		t.Errorf("Limits(openai/gpt-4o) = %d, %d, %v", in, out, ok)
	}
	for _, m := range []string{"no-size", "mystery"} {
		if _, _, ok := r.Limits(m); ok {
			t.Errorf("Limits(%q) ok, want unknown", m)
		}
	}
}

func TestUsage_PromptTokens(t *testing.T) {
	for _, tt := range []struct {
		name string
		u    Usage
		want int64
	}{
		{"openai input includes cache", Usage{Input: 1000, CacheRead: 800}, 1000},
		{"anthropic input excludes cache", Usage{Input: 50, CacheRead: 800, CacheCreation: 150}, 1000},
		{"no cache", Usage{Input: 300}, 300},
	} {
		if got := tt.u.PromptTokens(); got != tt.want {
			t.Errorf("%s: PromptTokens = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestStaticResolver_CandidateKeyMatching(t *testing.T) {
	prices := map[string]ModelPrice{
		"gpt-4o":                           {Rates: Rates{InputPerToken: 0.0000025}},
		"anthropic.claude-3-5-sonnet-v2:0": {Rates: Rates{InputPerToken: 0.000003}},
	}
	r := NewStaticResolver(prices)
	ctx := context.Background()

	cases := []struct {
		model string
		want  float64
	}{
		{"gpt-4o", 1000 * 0.0000025},
		{"openai/gpt-4o", 1000 * 0.0000025},                           // provider routing prefix
		{"us.anthropic.claude-3-5-sonnet-v2:0", 1000 * 0.000003},      // Bedrock cross-region prefix
		{"bedrock/anthropic.claude-3-5-sonnet-v2:0", 1000 * 0.000003}, // provider prefix
	}
	for _, c := range cases {
		got, err := r.Resolve(ctx, c.model, Usage{Input: 1000})
		if err != nil {
			t.Errorf("Resolve(%q): %v", c.model, err)
			continue
		}
		if !approx(got, c.want) {
			t.Errorf("Resolve(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}
