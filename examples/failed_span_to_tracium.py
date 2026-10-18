#!/usr/bin/env python3
"""
Emit a trace containing a **failed** LLM span and ship it to the locally-hosted
Tracium stack — so you can see how the collector and UI handle errors, not just
the happy path.

The scenario is a realistic production incident. A nightly **invoice-extraction
job** reads scanned invoices and asks an LLM to pull out structured fields. Two
documents come through fine, but the third trips a failure that actually happens
in the wild: an operator pinned the model name from a config file, and that
value has a typo (``gpt-4o-miini``). OpenAI rejects the request with a 404
``model_not_found`` error, the OpenLLMetry instrumentation records the exception
on the gen_ai span and sets its status to ERROR, and the surrounding task/
workflow spans fail with it.

    invoice-extraction-batch          (workflow)  → ends ERROR
    ├─ extract-invoice  (INV-9001)    (task)      → ok
    ├─ extract-invoice  (INV-9002)    (task)      → ok
    └─ extract-invoice  (INV-9003)    (task)      → ERROR  ← the failed span
        └─ openai.chat                (LLM)       → ERROR  (404 model_not_found)

So a single run produces a trace where a real gen_ai.* span carries
exception.type / exception.message events and status_code=ERROR, exactly the
shape the enrichment pipeline and the UI's error views need to be exercised
against. The failing call never reaches token billing (OpenAI rejects it before
inference), so this also checks that a priced-zero, errored span is handled
gracefully.

The failure is genuine — we send a malformed request to the real API rather
than faking a status — because the whole point is to verify end-to-end error
propagation through the collector. The first two calls are real, cheap
gpt-4o-mini requests; only the third is intentionally broken.

Run the stack first:   docker compose up -d
Create an ingest key in the dashboard (or POST /v1/workspaces/{id}/api-keys), then:
                       export OPENAI_API_KEY=sk-...
                       export TRACIUM_API_KEY=trc_...   # authenticates ingest + picks the workspace
                       python examples/failed_span_to_tracium.py

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

# OpenLLMetry: auto-instruments OpenAI and emits gen_ai.* spans over OTLP. When
# the underlying call raises, it records the exception and marks the span ERROR.
from traceloop.sdk import Traceloop
from traceloop.sdk.decorators import task, workflow

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.trace import Status, StatusCode


OTLP_ENDPOINT = os.getenv(
    "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
    "http://localhost:4318/v1/traces",
)

USER_ID = os.getenv("TRACIUM_USER_ID", "acme-corp")

# Ingest requires a per-workspace API key. It both authenticates the sender and
# decides which workspace these spans land in — so there is no workspace
# attribute to set. Create a key on the workspace's API-keys screen (or via
# POST /v1/workspaces/{id}/api-keys) and export it as TRACIUM_API_KEY.
API_KEY = os.getenv("TRACIUM_API_KEY")
if not API_KEY:
    raise SystemExit("TRACIUM_API_KEY is required — create an ingest key in the dashboard and export it")
OTLP_HEADERS = {"Authorization": f"Bearer {API_KEY}"}

# The model an operator *meant* to use for the healthy calls.
MODEL = os.getenv("OPENAI_MODEL", "gpt-4o-mini")

# The typo'd value that slipped into production config — note the doubled "i".
# This is what makes the third span fail with a real 404 model_not_found.
BROKEN_MODEL = os.getenv("OPENAI_BROKEN_MODEL", "gpt-4o-miini")


def configure_tracing() -> None:
    """Initialise Traceloop pointed at the local collector."""
    Traceloop.init(
        app_name="invoice-extraction-example",
        exporter=OTLPSpanExporter(endpoint=OTLP_ENDPOINT, headers=OTLP_HEADERS),
        # Carry the user on the OTel resource so it lands on every span.
        # No workspace attribute: the ingest key decides the workspace.
        resource_attributes={"tracium.user.id": USER_ID},
        disable_batch=False,
    )


def _tag_user() -> None:
    """Stamp the current span with the user so every gen_ai.* span carries it."""
    span = trace.get_current_span()
    span.set_attribute("tracium.user.id", USER_ID)


# --------------------------------------------------------------------------- #
# A tiny batch of "scanned" invoices to extract. In a real job these would be
# OCR'd PDF blobs pulled from object storage.
# --------------------------------------------------------------------------- #

_INVOICES = [
    {
        "invoice_id": "INV-9001",
        "raw_text": (
            "ACME WIDGETS LLC  Invoice #INV-9001  Date: 2026-05-02\n"
            "Bill to: Globex Corp\nQty 3 x Red Widget @ $29.00 ... Total due: $87.00\n"
            "Terms: Net 30"
        ),
    },
    {
        "invoice_id": "INV-9002",
        "raw_text": (
            "ACME WIDGETS LLC  Invoice #INV-9002  Date: 2026-05-04\n"
            "Bill to: Initech\nQty 1 x Blue Widget @ $42.50 ... Total due: $42.50\n"
            "Terms: Due on receipt"
        ),
    },
    {
        "invoice_id": "INV-9003",
        "raw_text": (
            "ACME WIDGETS LLC  Invoice #INV-9003  Date: 2026-05-06\n"
            "Bill to: Hooli\nQty 5 x Green Widget @ $15.00 ... Total due: $75.00\n"
            "Terms: Net 15"
        ),
    },
]


@task(name="extract-invoice")
def extract_invoice(client: OpenAI, invoice: dict, model: str) -> dict:
    """Extract structured fields from one invoice.

    Raises on the broken model so the span fails. We let the exception
    propagate: OpenLLMetry records it on the gen_ai span (exception event +
    status ERROR), and the @task span fails with it.
    """
    _tag_user()
    span = trace.get_current_span()
    span.set_attribute("invoice.id", invoice["invoice_id"])

    resp = client.chat.completions.create(
        model=model,
        response_format={"type": "json_object"},
        messages=[
            {
                "role": "system",
                "content": (
                    "Extract invoice fields as JSON: "
                    '{"invoice_id": string, "bill_to": string, '
                    '"total_usd": number, "terms": string}.'
                ),
            },
            {"role": "user", "content": invoice["raw_text"]},
        ],
    )
    return json.loads(resp.choices[0].message.content)


@workflow(name="invoice-extraction-batch")
def run_batch(client: OpenAI) -> list[dict]:
    """Process every invoice; the third one fails on the typo'd model name."""
    _tag_user()
    results: list[dict] = []
    failures = 0

    for invoice in _INVOICES:
        invoice_id = invoice["invoice_id"]
        # Simulate the production config bug: the last invoice gets the broken
        # model. In real life this would hit *every* call — here we isolate it
        # so the trace shows both healthy and failed spans side by side.
        model = BROKEN_MODEL if invoice_id == "INV-9003" else MODEL
        try:
            fields = extract_invoice(client, invoice, model)
            results.append({"invoice_id": invoice_id, "status": "ok", "fields": fields})
            print(f"[ok]   {invoice_id}: {json.dumps(fields)}")
        except Exception as exc:  # noqa: BLE001 — record + continue the batch
            failures += 1
            results.append({"invoice_id": invoice_id, "status": "error", "error": str(exc)})
            print(f"[FAIL] {invoice_id}: {type(exc).__name__}: {exc}")

    # Mark the workflow span as failed so the trace's root reflects the incident.
    if failures:
        span = trace.get_current_span()
        span.set_status(Status(StatusCode.ERROR, f"{failures} invoice(s) failed to extract"))

    return results


def main() -> None:
    if not os.getenv("OPENAI_API_KEY"):
        raise SystemExit("Set OPENAI_API_KEY before running this script.")

    configure_tracing()

    results = run_batch(OpenAI())

    ok = sum(1 for r in results if r["status"] == "ok")
    failed = sum(1 for r in results if r["status"] == "error")
    print(f"\n=== batch summary: {ok} ok, {failed} failed ===")

    # Flush spans before the process exits.
    trace.get_tracer_provider().force_flush()
    print(f"\nTrace (with failed span) exported to {OTLP_ENDPOINT} (user={USER_ID}; workspace resolved from the ingest key).")


if __name__ == "__main__":
    main()
