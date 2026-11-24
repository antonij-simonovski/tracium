package pricing

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// fileTable is the on-disk JSON shape of the pricing table, matching the
// canonical tracium-spec/pricing/pricing.json produced by refresh-pricing.sh
// from LiteLLM's community table. Models are keyed by their full provider id;
// each entry carries per-token costs and, for long-context models, tier
// overrides that apply once the prompt exceeds a threshold.
type fileTable struct {
	Version int                  `json:"version"`
	Models  map[string]fileEntry `json:"models"`
}

// fileEntry is one model's pricing entry. Cost fields are per single token
// (USD). Absent cache/tier fields are left zero and handled by the resolver.
type fileEntry struct {
	InputPerToken         float64    `json:"input_cost_per_token"`
	OutputPerToken        float64    `json:"output_cost_per_token"`
	CacheReadPerToken     float64    `json:"cache_read_input_token_cost"`
	CacheCreationPerToken float64    `json:"cache_creation_input_token_cost"`
	MaxInputTokens        int64      `json:"max_input_tokens"`
	MaxOutputTokens       int64      `json:"max_output_tokens"`
	Tiers                 []fileTier `json:"tiers"`
}

// fileTier is one long-context tier: the per-token overrides that apply once a
// call's input token count exceeds AboveInputTokens.
type fileTier struct {
	AboveInputTokens      int64   `json:"above_input_tokens"`
	InputPerToken         float64 `json:"input_cost_per_token"`
	OutputPerToken        float64 `json:"output_cost_per_token"`
	CacheReadPerToken     float64 `json:"cache_read_input_token_cost"`
	CacheCreationPerToken float64 `json:"cache_creation_input_token_cost"`
}

// LoadStaticFile reads the JSON price table from path and flattens it into a
// model-name → ModelPrice map, keyed by the lower-cased model id so lookups
// match the normalized model name. Callers may fall back to DefaultPrices when
// the file is absent.
func LoadStaticFile(path string) (map[string]ModelPrice, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pricing: read %s: %w", path, err)
	}
	var table fileTable
	if err := json.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("pricing: parse %s: %w", path, err)
	}
	prices := make(map[string]ModelPrice, len(table.Models))
	for id, e := range table.Models {
		if id == "" {
			return nil, fmt.Errorf("pricing: parse %s: model entry with empty id", path)
		}
		prices[strings.ToLower(id)] = e.toModelPrice()
	}
	return prices, nil
}

func (e fileEntry) toModelPrice() ModelPrice {
	p := ModelPrice{Rates: Rates{
		InputPerToken:         e.InputPerToken,
		OutputPerToken:        e.OutputPerToken,
		CacheReadPerToken:     e.CacheReadPerToken,
		CacheCreationPerToken: e.CacheCreationPerToken,
	}, MaxInputTokens: e.MaxInputTokens, MaxOutputTokens: e.MaxOutputTokens}
	for _, t := range e.Tiers {
		p.Tiers = append(p.Tiers, Tier{
			AboveInputTokens: t.AboveInputTokens,
			Rates: Rates{
				InputPerToken:         t.InputPerToken,
				OutputPerToken:        t.OutputPerToken,
				CacheReadPerToken:     t.CacheReadPerToken,
				CacheCreationPerToken: t.CacheCreationPerToken,
			},
		})
	}
	// ratesFor takes the last matching tier, so ascending order is part of its
	// contract. refresh-pricing.sh already sorts; sorting here too keeps a
	// hand-edited or third-party table from mispricing silently.
	sort.Slice(p.Tiers, func(i, j int) bool {
		return p.Tiers[i].AboveInputTokens < p.Tiers[j].AboveInputTokens
	})
	return p
}
