#!/usr/bin/env python3
"""
Simulate a realistic Tracium customer with the OpenTelemetry SDK: backdated
history, then live traffic with incidents injected on a timeline.

The company runs two workspaces:

    workspace A (KEYA)
      support-agent          service support-api   gpt-4o with a cached system prompt, tools
                             lookup_order, search_kb, crm_lookup (an MCP server), issue_refund.
                             Office-hours clients globex and initech, 24/7 client umbrella,
                             weekly client hooli (Mondays). v4, v5 shipped 12 days ago.
      ticket-classifier      service support-api   gpt-4o-mini, one short JSON label per ticket.
                             v5; v6 ships live and is healthy.
      rag-answerer           service kb-search     public help-centre widget, 24/7 with a daytime
                             peak: retrieve_documents, rerank, one gpt-4o-mini call. v1, v2 shipped
                             10 days ago; v3 ships live and regresses truncation and cost.
      nightly-etl-summariser service etl-worker    a 02:00 UTC batch, claude-sonnet-4-6.
      invoice-reconciler     service billing-sync  every 10 minutes, 24/7.
    workspace B (KEYB)
      contract-reviewer      service legal-api     gpt-4o with long prompts, office hours.
      intake-bot             service legal-api     gpt-4o-mini, office hours.

Background noise everywhere: about 1.5% errored runs, 0.5% refusals, 0.5%
truncations, 1% MCP errors, weekday/weekend and day/night traffic.

    seed --days 21    history up to now. Client umbrella goes quiet 7h before
                      now and service billing-sync stops 4h before now.
    live --minutes 50 streams now, printing each injection as it starts:
       2m  rag-answerer v3 (30% truncated, about 2x cost); ticket-classifier v6 (healthy)
       8m  support-agent requests gpt-4o, gets gpt-4o-mini for 70% of calls   until 35m
      12m  one support-agent run loops: 45 LLM calls, 20 x search_kb
      15m  a quarter of support-agent runs never finish (no root span)        until 25m
      18m  crm_lookup returns MCP isError payloads for 40% of calls           until 38m
      20m  workspace B uploads a contract batch: 40 contract-reviewer runs
      30m  umbrella and billing-sync come back; rag-answerer rolls back to v2

    pip install opentelemetry-sdk opentelemetry-exporter-otlp-proto-http
    KEYA=trc_... KEYB=trc_... python examples/customer_simulator.py seed
    KEYA=trc_... KEYB=trc_... python examples/customer_simulator.py live
"""

from __future__ import annotations

import argparse
import json
import os
import random
import time

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Status, StatusCode

ENDPOINT = os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318/v1/traces")
KEYS = {"A": os.getenv("KEYA") or exit("KEYA is required"), "B": os.getenv("KEYB") or exit("KEYB is required")}
HOUR, DAY = 3600, 86400
rng = random.Random(int(os.getenv("SIM_SEED", "7")))

providers: dict[tuple, TracerProvider] = {}


def tracer(ws: str, service: str, version: str):
    k = (ws, service, version)
    if k not in providers:
        p = TracerProvider(resource=Resource.create({"service.name": service, "tracium.workflow.version": version}))
        p.add_span_processor(BatchSpanProcessor(
            OTLPSpanExporter(endpoint=ENDPOINT, headers={"Authorization": f"Bearer {KEYS[ws]}"}),
            max_queue_size=500_000, max_export_batch_size=1000, schedule_delay_millis=500))
        providers[k] = p
    return providers[k].get_tracer("customer-simulator")


def ns(t: float) -> int:
    return int(t * 1e9)


def llm(model, prompt, output, tin, tout, cache=0, finish="stop", response_model=None):
    return {"model": model, "prompt": prompt, "output": output, "in": tin, "out": tout, "cache": cache,
            "finish": finish, "response_model": response_model or model}


def emit(ws, service, version, workflow, start, client="", calls=(), tools=(), steps=(), error=False, root=True):
    """One run: a root workflow span with steps, LLM calls and tool calls in order."""
    tr = tracer(ws, service, version)
    base = {"gen_ai.agent.name": workflow}
    if client:
        base["tracium.user.id"] = client
    parent = tr.start_span(workflow, start_time=ns(start), attributes={**base, "traceloop.span.kind": "workflow"})
    ctx = trace.set_span_in_context(parent)
    t = start + 0.05
    for name in steps:
        s = tr.start_span(name, context=ctx, start_time=ns(t), attributes={**base, "traceloop.span.kind": "task", "traceloop.entity.name": name})
        t += 0.3
        s.end(end_time=ns(t))
    for c in calls:
        dur = 0.4 + c["out"] / 120
        s = tr.start_span(f"chat {c['model']}", context=ctx, start_time=ns(t), attributes={
            **base, "gen_ai.system": "openai" if c["model"].startswith("gpt") else "anthropic",
            "gen_ai.operation.name": "chat", "gen_ai.request.model": c["model"], "gen_ai.response.model": c["response_model"],
            "gen_ai.usage.input_tokens": c["in"], "gen_ai.usage.output_tokens": c["out"],
            "gen_ai.usage.cache_read_input_tokens": c["cache"], "gen_ai.response.finish_reasons": [c["finish"]],
            "gen_ai.prompt.0.role": "user", "gen_ai.prompt.0.content": c["prompt"],
            "gen_ai.completion.0.role": "assistant", "gen_ai.completion.0.content": c["output"]})
        t += dur
        s.end(end_time=ns(t))
    for name, out, failed in tools:
        s = tr.start_span(name, context=ctx, start_time=ns(t), attributes={
            **base, "traceloop.span.kind": "tool", "traceloop.entity.name": name, "traceloop.entity.output": out})
        if failed:
            s.set_status(Status(StatusCode.ERROR, "tool failed"))
            s.add_event("exception", {"exception.type": "TimeoutError", "exception.message": f"{name} timed out"}, timestamp=ns(t + 0.2))
        t += 0.25
        s.end(end_time=ns(t))
    if error:
        parent.set_status(Status(StatusCode.ERROR, "run failed"))
    if root:
        parent.end(end_time=ns(t + 0.05))


def ticket():
    return f"#{rng.randint(10000, 99999)}"


TOPICS = ["refund for a damaged blender", "change delivery address", "invoice shows the wrong VAT number",
          "cannot log in after password reset", "order arrived without the charger", "cancel my subscription",
          "warranty claim for a cracked screen", "upgrade to the business plan", "missing loyalty points"]


def noise_output(text, finish="stop"):
    r = rng.random()
    if r < 0.005:
        return "I'm sorry, but I can't help with that request.", "stop"
    if r < 0.010:
        return text[: len(text) // 2], "length"
    return text, finish


class State:
    rag_version = "v2"
    classifier_version = "v5"
    fallback = loop_once = stuck = mcp_errors = False
    umbrella_quiet_since = None  # epoch seconds; None means active
    billing_quiet_since = None


S = State()


def support_run(t, client):
    tk, topic = ticket(), rng.choice(TOPICS)
    version = "v5" if t >= NOW - 12 * DAY else "v4"
    calls, tools = [], []
    n = 45 if S.loop_once else rng.choice([2, 2, 3, 3, 4])
    for i in range(n):
        out, finish = noise_output(f"Ticket {tk}: the customer asks about {topic}. Next step {i + 1}: checking the order history and the policy.")
        resp = "gpt-4o-mini" if S.fallback and rng.random() < 0.7 else "gpt-4o"
        calls.append(llm("gpt-4o", f"Customer ticket {tk}: {topic}. Order {rng.randint(1000, 9999)}.", out,
                         3200 + rng.randint(-400, 400), 160 + rng.randint(0, 120), cache=2400, finish=finish, response_model=resp))
    tools.append(("lookup_order", json.dumps({"order": rng.randint(1000, 9999), "status": "delivered"}), False))
    for _ in range(20 if S.loop_once else rng.choice([1, 1, 2])):
        tools.append(("search_kb", json.dumps({"hits": rng.randint(1, 6)}), rng.random() < 0.01))
    bad = rng.random() < (0.4 if S.mcp_errors else 0.01)
    crm = {"isError": True, "content": [{"type": "text", "text": "CRM upstream timed out after 30s"}]} if bad else \
          {"isError": False, "content": [{"type": "text", "text": json.dumps({"tier": rng.choice(["gold", "silver"])})}]}
    tools.append(("crm_lookup", json.dumps(crm), False))
    if "refund" in topic and rng.random() < 0.5:
        tools.append(("issue_refund", json.dumps({"refund_id": rng.randint(1, 10 ** 6)}), False))
    S.loop_once = False
    emit("A", "support-api", version, "support-agent", t, client, calls, tools, error=rng.random() < 0.015,
         root=not (S.stuck and rng.random() < 0.25))
    classifier_run(t + 0.5, client)


def classifier_run(t, client):
    label = rng.choice(["billing", "shipping", "account", "returns", "product"])
    out, finish = noise_output(json.dumps({"label": label}))
    emit("A", "support-api", S.classifier_version, "ticket-classifier", t, client,
         [llm("gpt-4o-mini", f"Classify ticket {ticket()}: {rng.choice(TOPICS)}", out, 420 + rng.randint(0, 80), 12, finish=finish)])


def rag_run(t):
    v = S.rag_version if t >= NOW else ("v2" if t >= NOW - 10 * DAY else "v1")
    q = rng.choice(TOPICS)
    answer = f"To handle '{q}', open Account, then Orders, choose the order and follow the guided steps; article {rng.randint(100, 999)} has details."
    tin, tout, finish = 2600 + rng.randint(-300, 300), 260 + rng.randint(0, 80), "stop"
    if v == "v3":
        tin, tout = tin * 2, tout * 2
        if rng.random() < 0.3:
            answer, finish = answer[:60], "length"
    out, finish = noise_output(answer, finish) if finish == "stop" else (answer, finish)
    steps = ["retrieve_documents"] + (["rerank"] if rng.random() < 0.6 else [])
    emit("A", "kb-search", v, "rag-answerer", t, "", [llm("gpt-4o-mini", f"Question: {q}", out, tin, tout, finish=finish)], steps=steps,
         error=rng.random() < 0.015)


def etl_run(t):
    doc = rng.randint(1, 10 ** 6)
    out, finish = noise_output(f"Summary of support conversation {doc}: the customer reported an issue, the agent resolved it within the SLA.")
    emit("A", "etl-worker", "v2", "nightly-etl-summariser", t, "", [llm("claude-sonnet-4-6", f"Summarise conversation {doc}", out, 5200, 400, finish=finish)],
         steps=["load_conversations", "write_summary"])


def billing_run(t):
    emit("A", "billing-sync", "v1", "invoice-reconciler", t, "", [llm("gpt-4.1-mini", f"Match invoice {rng.randint(1, 10 ** 6)}",
         json.dumps({"matched": True, "invoice": rng.randint(1, 10 ** 6)}), 900, 40)], steps=["fetch_invoices"])


def contract_run(t, client="contoso-internal"):
    out, finish = noise_output(f"Clause review for contract {rng.randint(1, 10 ** 5)}: indemnity cap is below policy, flag for counsel.")
    calls = [llm("gpt-4o", f"Review section {i} of contract", out, 8000 + rng.randint(0, 2000), 1200, finish=finish) for i in range(3)]
    emit("B", "legal-api", "v3", "contract-reviewer", t, client, calls, steps=["extract_clauses"])


def intake_run(t):
    out, finish = noise_output(json.dumps({"matter": rng.choice(["nda", "employment", "lease"]), "urgency": rng.choice(["low", "high"])}))
    emit("B", "legal-api", "v1", "intake-bot", t, "web-form", [llm("gpt-4o-mini", f"New intake {ticket()}", out, 600, 30, finish=finish)])


def office(t, peak, off):
    g = time.gmtime(t)
    return peak if g.tm_wday < 5 and 8 <= g.tm_hour < 18 else off


def rates(t):
    """Runs per hour of each run kind at t."""
    g = time.gmtime(t)
    day = 7 <= g.tm_hour < 22
    quiet_umbrella = S.umbrella_quiet_since is not None and t >= S.umbrella_quiet_since
    quiet_billing = S.billing_quiet_since is not None and t >= S.billing_quiet_since
    return {
        ("support", "globex"): office(t, 12, 0.5),
        ("support", "initech"): office(t, 10, 0.3),
        ("support", "umbrella"): 0 if quiet_umbrella else 4,
        ("support", "hooli"): 6 if g.tm_wday == 0 and 9 <= g.tm_hour < 12 else 0,
        ("rag", ""): (60 if day else 12) * (0.7 if g.tm_wday >= 5 else 1),
        ("etl", ""): 80 if g.tm_hour == 2 else 0,
        ("billing", ""): 0 if quiet_billing else 6,
        ("contract", ""): office(t, 12, 0),
        ("intake", ""): office(t, 8, 1),
    }


def run_kind(kind, client, t):
    {"support": lambda: support_run(t, client), "rag": lambda: rag_run(t), "etl": lambda: etl_run(t),
     "billing": lambda: billing_run(t), "contract": lambda: contract_run(t), "intake": lambda: intake_run(t)}[kind]()


def poisson(lam):
    n, p, limit = 0, 1.0, pow(2.718281828, -lam)
    while True:
        p *= rng.random()
        if p <= limit:
            return n
        n += 1


def seed(days):
    S.umbrella_quiet_since, S.billing_quiet_since = NOW - 7 * HOUR, NOW - 4 * HOUR
    start = int(NOW - days * DAY) // HOUR * HOUR
    runs = 0
    for h in range(start, int(NOW), HOUR):
        for (kind, client), r in rates(h).items():
            for _ in range(poisson(r)):
                t = h + rng.random() * HOUR
                if t < NOW - 60:
                    run_kind(kind, client, t)
                    runs += 1
        if (h - start) % DAY == 0:
            print(f"seeded {time.strftime('%Y-%m-%d', time.gmtime(h))}, {runs} runs so far", flush=True)
    return runs


TIMELINE = [
    (2, "rag-answerer v3 ships (regresses truncation and cost); ticket-classifier v6 ships (healthy)",
     lambda: (setattr(S, "rag_version", "v3"), setattr(S, "classifier_version", "v6"))),
    (8, "support-agent model fallback starts: gpt-4o answered by gpt-4o-mini", lambda: setattr(S, "fallback", True)),
    (12, "support-agent loop run", lambda: setattr(S, "loop_once", True)),
    (15, "support-agent runs start getting stuck", lambda: setattr(S, "stuck", True)),
    (18, "crm_lookup MCP errors start", lambda: setattr(S, "mcp_errors", True)),
    (20, "workspace B contract batch (40 runs)", lambda: [contract_run(time.time() + i * 2, "contoso-batch") for i in range(40)]),
    (25, "stuck runs stop", lambda: setattr(S, "stuck", False)),
    (30, "umbrella and billing-sync come back; rag-answerer rolls back to v2",
     lambda: (setattr(S, "umbrella_quiet_since", None), setattr(S, "billing_quiet_since", None), setattr(S, "rag_version", "v2"))),
    (35, "model fallback ends", lambda: setattr(S, "fallback", False)),
    (38, "MCP errors stop", lambda: setattr(S, "mcp_errors", False)),
]


def live(minutes, tick=10):
    S.umbrella_quiet_since, S.billing_quiet_since = NOW - 7 * HOUR, NOW - 4 * HOUR
    t0, pending = time.time(), list(TIMELINE)
    while (elapsed := time.time() - t0) < minutes * 60:
        while pending and elapsed >= pending[0][0] * 60:
            m, what, act = pending.pop(0)
            act()
            print(f"{time.strftime('%H:%M:%S', time.gmtime())} UTC  T+{m}m  {what}", flush=True)
        now = time.time()
        for (kind, client), r in rates(now).items():
            for _ in range(poisson(r * tick / HOUR)):
                run_kind(kind, client, now - rng.random() * 2)
        time.sleep(max(0.0, tick - (time.time() - now)))


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("mode", choices=["seed", "live"])
    ap.add_argument("--days", type=int, default=21)
    ap.add_argument("--minutes", type=int, default=50)
    a = ap.parse_args()
    NOW = time.time()
    if a.mode == "seed":
        print(f"{seed(a.days)} runs seeded")
    else:
        live(a.minutes)
    for p in providers.values():
        p.shutdown()
