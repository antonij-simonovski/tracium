#!/usr/bin/env bash
#
# Regenerates pricing/pricing.json from LiteLLM's community-maintained pricing
# table — the same source Traceloop/tokencost price against — so Tracium's
# computed cost matches upstream for every major provider.
#
# It projects LiteLLM's model_prices_and_context_window down to only the fields
# Tracium prices on (per-token input/output cost, prompt-cache read/write cost,
# the >N-token tier overrides some long-context models carry, and the input and
# output token limits). Everything else (capability flags, provider metadata)
# is dropped so the vendored file stays small and the parser
# (internal/pricing/file.go) stays trivial.
#
# Usage:  ./refresh-pricing.sh [ref]
#   ref   LiteLLM git ref to pin to (branch, tag, or commit). Default: main.
#
# The pinned ref is recorded in the output's "source" field for reproducibility.
set -euo pipefail

REF="${1:-main}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="${DIR}/pricing.json"
URL="https://raw.githubusercontent.com/BerriAI/litellm/${REF}/litellm/model_prices_and_context_window_backup.json"

echo "Fetching LiteLLM pricing @ ${REF}..." >&2
RAW="$(curl -fsSL "$URL")"

# Resolve the ref to a concrete commit so "source" pins an immutable snapshot.
COMMIT="$(curl -fsSL "https://api.github.com/repos/BerriAI/litellm/commits/${REF}" \
  | jq -r '.sha' 2>/dev/null || echo "$REF")"

echo "Projecting to Tracium pricing shape..." >&2
printf '%s' "$RAW" | jq \
  --arg source "litellm@${COMMIT}" \
  --arg date "$(date -u +%Y-%m-%d)" '
  # Build the tier list for one model object from its *_above_<N>k?_tokens keys,
  # grouping the per-metric overrides that share a threshold into one tier.
  def tiers($m):
    [ $m | to_entries[]
      | select(.key | test("_above_[0-9]+k?_tokens$"))
      | (.key | capture("^(?<field>input_cost_per_token|output_cost_per_token|cache_read_input_token_cost|cache_creation_input_token_cost)_above_(?<num>[0-9]+)(?<k>k?)_tokens$")) as $c
      | { threshold: (($c.num | tonumber) * (if $c.k == "k" then 1000 else 1 end)),
          field: $c.field, value: .value }
    ]
    | group_by(.threshold)
    | map({ above_input_tokens: .[0].threshold } + (map({ (.field): .value }) | add))
    | sort_by(.above_input_tokens);

  {
    version: 2,
    source: $source,
    updated_at: $date,
    models: (
      to_entries
      | map(select(.key != "sample_spec"))
      | map(
          .value as $m
          | { key: (.key | ascii_downcase),
              value: (
                { input_cost_per_token:            $m.input_cost_per_token,
                  output_cost_per_token:           $m.output_cost_per_token,
                  cache_read_input_token_cost:     $m.cache_read_input_token_cost,
                  cache_creation_input_token_cost: $m.cache_creation_input_token_cost,
                  max_input_tokens:                $m.max_input_tokens,
                  max_output_tokens:               $m.max_output_tokens,
                  tiers: (tiers($m) | if length == 0 then null else . end) }
                | with_entries(select(.value != null))
              ) }
        )
      # Keep only entries that actually carry a per-token price.
      | map(select(.value.input_cost_per_token != null or .value.output_cost_per_token != null))
      | from_entries
    )
  }
' > "$OUT"

COUNT="$(jq '.models | length' "$OUT")"
echo "Wrote ${COUNT} models to ${OUT} (source litellm@${COMMIT:0:12})" >&2
