package pricing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStaticFile_ParsesRatesCacheAndTiers(t *testing.T) {
	const body = `{
	  "version": 2,
	  "models": {
	    "gpt-4o": {
	      "input_cost_per_token": 0.0000025,
	      "output_cost_per_token": 0.00001,
	      "cache_read_input_token_cost": 0.00000125,
	      "max_input_tokens": 128000,
	      "max_output_tokens": 16384
	    },
	    "gemini-2.5-pro": {
	      "input_cost_per_token": 0.00000125,
	      "output_cost_per_token": 0.00001,
	      "tiers": [
	        { "above_input_tokens": 200000,
	          "input_cost_per_token": 0.0000025,
	          "output_cost_per_token": 0.000015 }
	      ]
	    }
	  }
	}`
	path := filepath.Join(t.TempDir(), "pricing.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	prices, err := LoadStaticFile(path)
	if err != nil {
		t.Fatalf("LoadStaticFile: %v", err)
	}

	gpt := prices["gpt-4o"]
	if gpt.InputPerToken != 0.0000025 || gpt.OutputPerToken != 0.00001 || gpt.CacheReadPerToken != 0.00000125 {
		t.Errorf("gpt-4o rates = %+v", gpt.Rates)
	}
	if gpt.MaxInputTokens != 128000 || gpt.MaxOutputTokens != 16384 {
		t.Errorf("gpt-4o limits = %d/%d", gpt.MaxInputTokens, gpt.MaxOutputTokens)
	}
	gem := prices["gemini-2.5-pro"]
	if len(gem.Tiers) != 1 || gem.Tiers[0].AboveInputTokens != 200000 || gem.Tiers[0].InputPerToken != 0.0000025 {
		t.Errorf("gemini tiers = %+v", gem.Tiers)
	}
}

// The parser must accept the canonical table shipped in tracium-spec, which the
// docker/helm deployments mount at /etc/tracium/pricing.json.
func TestLoadStaticFile_ShippedSpecFile(t *testing.T) {
	path := "../../../tracium-spec/pricing/pricing.json"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("spec pricing file not reachable: %v", err)
	}
	prices, err := LoadStaticFile(path)
	if err != nil {
		t.Fatalf("LoadStaticFile(spec): %v", err)
	}
	for _, model := range []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-6", "gemini-2.5-pro"} {
		p, ok := prices[model]
		if !ok {
			t.Errorf("spec pricing missing model %q", model)
			continue
		}
		if p.InputPerToken <= 0 {
			t.Errorf("spec pricing model %q has non-positive input rate %v", model, p.InputPerToken)
		}
	}
}

func TestLoadStaticFile_MissingFileErrors(t *testing.T) {
	if _, err := LoadStaticFile(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// ratesFor takes the last matching tier, so the loader must impose ascending
// order rather than trusting the file's.
func TestLoadStaticFile_SortsTiersAscending(t *testing.T) {
	const body = `{
	  "version": 2,
	  "models": {
	    "m": {
	      "input_cost_per_token": 0.000001,
	      "output_cost_per_token": 0.000002,
	      "tiers": [
	        {"above_input_tokens": 400000, "input_cost_per_token": 0.000004},
	        {"above_input_tokens": 200000, "input_cost_per_token": 0.000002}
	      ]
	    }
	  }
	}`
	path := filepath.Join(t.TempDir(), "pricing.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prices, err := LoadStaticFile(path)
	if err != nil {
		t.Fatalf("LoadStaticFile: %v", err)
	}
	tiers := prices["m"].Tiers
	if len(tiers) != 2 || tiers[0].AboveInputTokens != 200000 || tiers[1].AboveInputTokens != 400000 {
		t.Fatalf("tiers not sorted ascending: %+v", tiers)
	}
	// A 500k-token prompt must bill at the 400k tier, not the 200k one.
	if got := prices["m"].ratesFor(500000).InputPerToken; !approx(got, 0.000004) {
		t.Errorf("ratesFor(500000) input = %v, want 0.000004", got)
	}
}
