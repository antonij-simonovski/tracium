#!/usr/bin/env python3
"""
Run a realistic multi-step OpenAI agent and ship the whole trace to the
locally-hosted Tracium stack.

The scenario is a **customer-support triage agent**. A single inbound ticket
flows through four stages, each emitting its own span(s) under one workflow:

    support-ticket-triage            (workflow)
    ├─ classify-ticket               (task)      → 1 LLM call, JSON output
    ├─ resolution-agent              (agent)     → 2-3 LLM calls + tool calls
    │   ├─ lookup_order              (tool)
    │   ├─ check_inventory           (tool)
    │   └─ get_refund_policy         (tool)
    └─ qa-review                     (task)      → 1 LLM call

That's ~4-5 real OpenAI calls plus local tool executions, so a single run
produces a nested trace with multiple gen_ai.* spans for the enrichment
pipeline to price — a much better exercise of the collector than one call.

The Tracium collector (see docker-compose.yml) speaks plain OTLP and reads the
standard OpenTelemetry GenAI semantic conventions (gen_ai.request.model,
gen_ai.usage.input_tokens, ...). OpenLLMetry (Traceloop's SDK) auto-instruments
the OpenAI client and emits exactly those attributes, and its workflow/task/
agent/tool decorators give the surrounding spans their structure. We point it at
the collector's OTLP/HTTP endpoint and let the enrichment pipeline compute cost.

We also enable OpenLLMetry's **metrics**: the gen_ai.client.token.usage
histogram is exported (with DELTA temporality, so each push is a per-interval
increment) to the collector's /v1/metrics endpoint. The collector prices those
points and writes them as source="metric" rows into the same spans table, where
they become the sampling-robust source for cost/token totals. Metrics are
strictly additive: set TRACIUM_ENABLE_METRICS=0 — or just run against a
collector with no metrics pipeline — and everything falls back to span-only,
exactly as before.

Run the stack first:   docker compose up -d
Create an ingest key in the dashboard (or POST /v1/workspaces/{id}/api-keys), then:
                       export OPENAI_API_KEY=sk-...
                       export TRACIUM_API_KEY=trc_...   # authenticates ingest + picks the workspace
                       python examples/openai_to_tracium.py

Install deps:
    pip install -r examples/requirements.txt
"""

from __future__ import annotations

import json
import os
from pathlib import Path

# Load .env from the same directory as this script.
_env_file = Path(__file__).parent / ".env"
if _env_file.exists():
    for _line in _env_file.read_text().splitlines():
        _line = _line.strip()
        if _line and not _line.startswith("#") and "=" in _line:
            _k, _, _v = _line.partition("=")
            os.environ.setdefault(_k.strip(), _v.strip())

from openai import OpenAI

# OpenLLMetry: auto-instruments OpenAI and emits gen_ai.* spans over OTLP.
from traceloop.sdk import Traceloop
from traceloop.sdk.decorators import agent, task, tool, workflow

from opentelemetry import metrics, trace
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.metrics import Counter, Histogram, ObservableCounter
from opentelemetry.sdk.metrics.export import AggregationTemporality


# Collector OTLP/HTTP endpoint. docker-compose exposes 4318 on localhost.
OTLP_ENDPOINT = os.getenv(
    "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
    "http://localhost:4318/v1/traces",
)

METRICS_ENDPOINT = os.getenv(
    "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
    "http://localhost:4318/v1/metrics",
)

USER_ID = os.getenv("TRACIUM_USER_ID", "acme-corp")
WORKFLOW_VERSION = os.getenv("TRACIUM_WORKFLOW_VERSION", "v1")

# Ingest requires a per-workspace API key. It both authenticates the sender and
# decides which workspace these spans land in — so there is no workspace
# attribute to set. Create a key on the workspace's API-keys screen (or via
# POST /v1/workspaces/{id}/api-keys) and export it as TRACIUM_API_KEY.
API_KEY = os.getenv("TRACIUM_API_KEY")
if not API_KEY:
    raise SystemExit("TRACIUM_API_KEY is required — create an ingest key in the dashboard and export it")
OTLP_HEADERS = {"Authorization": f"Bearer {API_KEY}"}

MODEL = os.getenv("OPENAI_MODEL", "gpt-4o-mini")

# Metrics are additive; disable them to fall back to span-only ingestion.
METRICS_ENABLED = (os.getenv("TRACIUM_ENABLE_METRICS") or "1").lower() not in ("0", "false")

# DELTA temporality for sum/histogram instruments so each export is a
# per-interval increment the collector can sum without double-counting (the OTLP
# default is cumulative, which would inflate totals across exports).
_DELTA_TEMPORALITY = {
    Counter: AggregationTemporality.DELTA,
    Histogram: AggregationTemporality.DELTA,
    ObservableCounter: AggregationTemporality.DELTA,
}


def _build_metrics_exporter() -> "OTLPMetricExporter | None":
    """Construct the OTLP metric exporter, or None to stay span-only.

    Best-effort: a construction failure (bad endpoint, missing dep) is swallowed
    so tracing still initialises. Connection failures at export time happen on a
    background reader thread and never affect the traced call either way.
    """
    if not METRICS_ENABLED:
        return None
    try:
        return OTLPMetricExporter(
            endpoint=METRICS_ENDPOINT,
            headers=OTLP_HEADERS,
            preferred_temporality=_DELTA_TEMPORALITY,
        )
    except Exception as exc:  # noqa: BLE001 — metrics must never break tracing
        print(f"metrics disabled (exporter init failed: {exc}); continuing span-only")
        return None


def configure_tracing() -> bool:
    """Initialise Traceloop. Returns whether metric export is enabled."""
    metrics_exporter = _build_metrics_exporter()
    Traceloop.init(
        app_name="openai-tracium-example",
        exporter=OTLPSpanExporter(endpoint=OTLP_ENDPOINT, headers=OTLP_HEADERS),
        metrics_exporter=metrics_exporter,
        # Carry the user on the OTel resource so it lands on BOTH signals.
        # Metric data points don't inherit per-span attributes, so without this
        # the collector would write metric rows with an empty user_id; the
        # processor reads tracium.user.id from the resource as a fallback.
        # No workspace attribute: the ingest key decides the workspace.
        # tracium.workflow.version on the resource puts every span, and so every
        # run, under one version for the per-version metrics.
        resource_attributes={
            "tracium.user.id": USER_ID,
            "tracium.workflow.version": WORKFLOW_VERSION,
        },
        disable_batch=False,
    )
    return metrics_exporter is not None


def _tag_user() -> None:
    """Stamp the current span with the user so every gen_ai.* span carries it."""
    span = trace.get_current_span()
    span.set_attribute("tracium.user.id", USER_ID)


# --------------------------------------------------------------------------- #
# Fake back-office data + tools the resolution agent can call.
# In a real deployment these would hit a database / order service.
# --------------------------------------------------------------------------- #

_ORDERS = {
    "A1001": {"status": "delivered", "sku": "SKU-RED-42", "total_usd": 89.00, "days_ago": 18},
    "A1002": {"status": "shipped", "sku": "SKU-BLUE-42", "total_usd": 42.50, "days_ago": 3},
}

_INVENTORY = {"SKU-RED-42": 0, "SKU-BLUE-42": 14}

_REFUND_POLICY = (
    "Full refunds within 30 days of delivery. After 30 days, store credit only. "
    "Defective items are always eligible for a full refund or free replacement."
)


@tool(name="lookup_order")
def lookup_order(order_id: str) -> dict:
    _tag_user()
    return _ORDERS.get(order_id, {"error": f"order {order_id} not found"})


@tool(name="check_inventory")
def check_inventory(sku: str) -> dict:
    _tag_user()
    return {"sku": sku, "units_available": _INVENTORY.get(sku, 0)}


@tool(name="get_refund_policy")
def get_refund_policy() -> dict:
    _tag_user()
    return {"policy": _REFUND_POLICY}


_TOOL_IMPLS = {
    "lookup_order": lookup_order,
    "check_inventory": check_inventory,
    "get_refund_policy": get_refund_policy,
}

_TOOL_SCHEMAS = [
    {
        "type": "function",
        "function": {
            "name": "lookup_order",
            "description": "Look up an order's status, SKU, total and age in days.",
            "parameters": {
                "type": "object",
                "properties": {"order_id": {"type": "string"}},
                "required": ["order_id"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "check_inventory",
            "description": "Return how many units of a SKU are in stock.",
            "parameters": {
                "type": "object",
                "properties": {"sku": {"type": "string"}},
                "required": ["sku"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "get_refund_policy",
            "description": "Return the current refund policy text.",
            "parameters": {"type": "object", "properties": {}},
        },
    },
]


# --------------------------------------------------------------------------- #
# Stage 1 — classify the ticket (one LLM call, JSON output).
# --------------------------------------------------------------------------- #

@task(name="classify-ticket")
def classify_ticket(client: OpenAI, ticket: str) -> dict:
    _tag_user()
    resp = client.chat.completions.create(
        model=MODEL,
        response_format={"type": "json_object"},
        messages=[
            {
                "role": "system",
                "content": (
                    "Classify the support ticket. Respond with JSON: "
                    '{"intent": one of ["refund","order_status","product_question","other"], '
                    '"priority": one of ["low","medium","high"], '
                    '"order_id": the order id mentioned or null}.'
                ),
            },
            {"role": "user", "content": ticket},
        ],
    )
    return json.loads(resp.choices[0].message.content)


# --------------------------------------------------------------------------- #
# Stage 2 — resolution agent: a tool-calling loop (multiple LLM calls).
# --------------------------------------------------------------------------- #

@agent(name="resolution-agent")
def resolve_ticket(client: OpenAI, ticket: str, classification: dict) -> str:
    _tag_user()
    messages = [
        {
            "role": "system",
            "content": (
                "You are a customer-support agent. Use the available tools to "
                "gather facts before answering. Once you have enough information, "
                "write a concise, friendly resolution for the customer. Do not "
                "invent order details — look them up."
            ),
        },
        {
            "role": "user",
            "content": f"Ticket: {ticket}\n\nClassification: {json.dumps(classification)}",
        },
    ]

    # Bounded loop: the model calls tools until it produces a final answer.
    for _ in range(4):
        resp = client.chat.completions.create(
            model=MODEL,
            messages=messages,
            tools=_TOOL_SCHEMAS,
        )
        msg = resp.choices[0].message
        if not msg.tool_calls:
            return msg.content

        messages.append(msg)
        for call in msg.tool_calls:
            impl = _TOOL_IMPLS[call.function.name]
            args = json.loads(call.function.arguments or "{}")
            result = impl(**args)
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": call.id,
                    "content": json.dumps(result),
                }
            )

    return "I'm sorry — I couldn't fully resolve this automatically; escalating to a human agent."


# --------------------------------------------------------------------------- #
# Stage 3 — QA review of the drafted resolution (one LLM call).
# --------------------------------------------------------------------------- #

@task(name="qa-review")
def qa_review(client: OpenAI, ticket: str, draft: str) -> dict:
    _tag_user()
    resp = client.chat.completions.create(
        model=MODEL,
        response_format={"type": "json_object"},
        messages=[
            {
                "role": "system",
                "content": (
                    "You are a QA reviewer. Given the customer ticket and a draft "
                    "reply, decide if the reply is accurate, on-policy and friendly. "
                    'Respond with JSON: {"approved": bool, "notes": string, '
                    '"final_reply": the reply to send (revised if needed)}.'
                ),
            },
            {"role": "user", "content": f"Ticket: {ticket}\n\nDraft reply: {draft}"},
        ],
    )
    return json.loads(resp.choices[0].message.content)


@workflow(name="support-ticket-triage")
def triage(client: OpenAI, ticket: str) -> dict:
    _tag_user()
    classification = classify_ticket(client, ticket)
    draft = resolve_ticket(client, ticket, classification)
    review = qa_review(client, ticket, draft)
    return {"classification": classification, "draft": draft, "review": review}


def main() -> None:
    if not os.getenv("OPENAI_API_KEY"):
        raise SystemExit("Set OPENAI_API_KEY before running this script.")

    metrics_enabled = configure_tracing()

    ticket = os.getenv(
        "SUPPORT_TICKET",
        "Hi, I ordered the red widget (order A1001) almost three weeks ago. "
        "It arrived broken. Can I get a refund or a replacement?",
    )

    result = triage(OpenAI(), ticket)

    print("=== classification ===")
    print(json.dumps(result["classification"], indent=2))
    print("\n=== agent draft ===")
    print(result["draft"])
    print("\n=== QA review ===")
    review = result["review"]
    print(f"approved: {review.get('approved')}  notes: {review.get('notes')}")
    print("\n=== final reply to customer ===")
    print(review.get("final_reply", result["draft"]))

    # Flush spans (and metrics, if enabled) before the process exits.
    trace.get_tracer_provider().force_flush()
    print(f"\nTrace exported to {OTLP_ENDPOINT} (user={USER_ID}; workspace resolved from the ingest key).")
    if metrics_enabled:
        metrics.get_meter_provider().force_flush()
        print(f"Metrics exported to {METRICS_ENDPOINT} (user={USER_ID}).")


if __name__ == "__main__":
    main()
