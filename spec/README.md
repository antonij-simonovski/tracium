# spec

The single source of truth for all data contracts in the Tracium platform.

This directory holds no application code: only JSON Schemas, YAML attribute
definitions, an OpenAPI specification, a pricing table, and codegen scripts.
Every shared type used across `api`, `collector`, and `dashboard` originates
here.

## Purpose

When you need to answer "what is the canonical shape of a Span?", the answer is
`schemas/span.json`, not a struct in `api` or an interface in `dashboard`.

Change a shared data contract here first, then regenerate the derived types with
the codegen scripts. Never hand-edit generated files.

## Layout

```
spec/
  api/
    openapi.yaml          ← OpenAPI 3.1 spec for api
  schemas/
    span.json             ← JSON Schema (draft-07) for the Span object
    trace.json            ← JSON Schema for the Trace object
    error.json            ← JSON Schema for the ErrorResponse envelope
  pricing/
    pricing.json          ← model pricing table, consumed by collector
    refresh-pricing.sh    ← regenerates pricing.json from LiteLLM
  attributes/
    genai.yaml            ← gen_ai.* OTel SIG attributes Tracium reads
    tracium.yaml          ← tracium.* attributes written by the collector
  codegen/
    gen-ts-types.sh       ← generates TypeScript types in dashboard
  README.md               ← this file
```

## Compatibility Policy

### What is NOT a breaking change

These changes can be merged without a `schema_version` bump:

- Adding a new **optional** field to a response schema
- Adding a new optional query parameter to an API endpoint
- Adding a new endpoint to the API
- Refreshing `pricing.json` from upstream (new models, updated rates)

### What IS a breaking change

These changes require incrementing `schema_version` in `span.json` or
`trace.json`, a [`CHANGELOG.md`](../CHANGELOG.md) entry, and announcement before merge:

- Renaming any field
- Removing any field
- Changing a field's type (e.g. `string` → `integer`)
- Making an optional field required
- Removing an endpoint
- Changing the meaning of a field's values (semantic breaking change)

### Deprecation period

Breaking changes to a schema version that is actively used in production require
a minimum **30-day deprecation notice** before taking effect. During the
deprecation period:

1. Add a `deprecated: true` marker to the field in the schema with a
   `x-deprecated-since` extension noting the date.
2. Add a `Deprecation` entry to `openapi.yaml` on the affected operation if
   it is an API-level change.
3. After 30 days, remove the deprecated field and increment `schema_version`.

## How to Make a Change

### Step 1: Classify the change

Is it additive (new optional field, new model, new endpoint) or breaking
(rename, remove, type change)? Use the compatibility policy above.

### Step 2: Update the relevant schema files

- Field changes: edit `schemas/span.json` or `schemas/trace.json`
- API changes: edit `api/openapi.yaml`
- New/updated model pricing: run `pricing/refresh-pricing.sh` (do not hand-edit)
- New OTel attribute: edit `attributes/genai.yaml` or `attributes/tracium.yaml`

### Step 3: Increment schema_version (breaking changes only)

If your change is breaking, increment the `schema_version` minimum in the
relevant JSON Schema file. Update any example values and comments referencing
the version number.

### Step 4: Run the codegen script

```bash
./codegen/gen-ts-types.sh
```

Commit the generated output changes alongside your schema changes.

### Step 5: Update the root CHANGELOG.md

Add a one-line bullet under `## [Unreleased]`, in `### Added`, `### Changed`,
`### Fixed` or `### Removed`. Keep it an outline: name the change, and mark it
"breaking" when it is. Details belong in the PR, not the changelog.

## Codegen

### TypeScript types (dashboard)

Prerequisites: Node.js and `npx` available on your `$PATH`.

```bash
./codegen/gen-ts-types.sh
```

This runs `openapi-typescript` against `api/openapi.yaml` and writes the output
to `dashboard/src/types/openapi.d.ts`.

The Go models in `api/internal/model` are hand-written; keep them in step with
the spec by hand.

## Pricing

### Format

`pricing/pricing.json` is **generated** by `pricing/refresh-pricing.sh`, which
projects LiteLLM's community pricing table down to the fields Tracium prices on.
It is a JSON object keyed by model id, with **per-token** costs:

```json
{
  "version": 2,
  "source": "litellm@<commit>",
  "updated_at": "YYYY-MM-DD",
  "models": {
    "gpt-4o": {
      "input_cost_per_token": 0.0000025,
      "output_cost_per_token": 0.00001,
      "cache_read_input_token_cost": 0.00000125
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
}
```

- Keys are the full provider model id (Bedrock/Vertex/Azure ids stored verbatim).
  The collector's resolver strips routing (`openai/…`) and cross-region
  (`us.…`) prefixes as a fallback, so no `aliases` list is needed.
- `cache_read_input_token_cost` / `cache_creation_input_token_cost` (optional):
  per-token cost of prompt-cache reads / writes.
- `tiers` (optional): re-price the whole call once input exceeds
  `above_input_tokens` (long-context models). Ordered ascending.
- Costs are in USD per **single** token, matching LiteLLM.

### Updating pricing

Do not hand-edit the file. Regenerate it:

1. Run `./pricing/refresh-pricing.sh` (optionally pass a LiteLLM git ref to pin).
   It records the pinned commit in `source`.
2. Add a `### Changed` entry to [`CHANGELOG.md`](../CHANGELOG.md).
3. Commit. The collector reads this file at startup and picks up changes on
   next restart.
