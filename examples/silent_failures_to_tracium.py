#!/usr/bin/env python3
"""
Send mock traces that exercise every silent-failure detector, with healthy
look-alikes that must not be flagged. Stdlib only; posts OTLP/JSON.

Each detector gets its own workflow (prefix "sf-" by default) with a 24-hour
baseline and an incident in the last complete hour:

    sf-truncation   finish_reasons length / MAX_TOKENS        -> truncation_rate
    sf-refusal      refusal text and content_filter           -> refusal_rate
    sf-tool-errors  OK tool spans returning error JSON        -> tool_error_rate
    sf-loop         one run with 40 LLM calls, 15 x "search"  -> llm_calls_per_run, tool_repeat
    sf-cache        cache reads drop from 80% to 0%           -> cache_hit_ratio
    sf-latency      root spans 10x slower today               -> latency_p95 (daily)
    sf-heartbeat    client sf-steady stopped 3 days ago       -> heartbeat
                    client sf-weekly ran 6 days ago           -> not flagged
    sf-release-bad  v2 truncates 25% of calls at 2x the cost  -> release report regressed
    sf-release-good v2 behaves like v1                        -> release report ok
    sf-release-thin v2 shipped 25h ago with 5 runs            -> release report insufficient

Run the stack first (docker compose up -d), create an ingest key, then:
    export TRACIUM_API_KEY=trc_...
    python examples/silent_failures_to_tracium.py

Then open the overview's outliers panel (7d range) or
GET /v1/metrics/anomalies?range=7d. Re-running adds the same data again.
"""

from __future__ import annotations

import json
import os
import random
import time
import urllib.request

ENDPOINT = os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318/v1/traces")
API_KEY = os.getenv("TRACIUM_API_KEY") or exit("TRACIUM_API_KEY is required")
PREFIX = os.getenv("SF_PREFIX", "sf")

HOUR = 3600
DAY = 24 * HOUR
NOW = int(time.time())
INCIDENT = NOW // HOUR * HOUR - HOUR  # start of the last complete hour
TODAY = NOW // DAY * DAY

resources: dict[str, list] = {}


def hexid(n: int) -> str:
    return "%0*x" % (n * 2, random.getrandbits(n * 8))


def attr(k, v):
    if isinstance(v, bool):
        return {"key": k, "value": {"boolValue": v}}
    if isinstance(v, int):
        return {"key": k, "value": {"intValue": str(v)}}
    if isinstance(v, list):
        return {"key": k, "value": {"arrayValue": {"values": [{"stringValue": x} for x in v]}}}
    return {"key": k, "value": {"stringValue": v}}


def span(trace, parent, name, start, dur, attrs, client="", version=""):
    s = {
        "traceId": trace,
        "spanId": hexid(8),
        "name": name,
        "kind": 1,
        "startTimeUnixNano": str(int(start * 1e9)),
        "endTimeUnixNano": str(int((start + dur) * 1e9)),
        "attributes": [attr(k, v) for k, v in attrs.items()],
        "status": {"code": 1},
    }
    if parent:
        s["parentSpanId"] = parent
    resources.setdefault((client, version), []).append(s)
    return s["spanId"]


def run(workflow, start, llm_calls, client="", tools=(), finish="stop", output="Done.",
        input_tokens=500, cache_read=0, root_dur=2.0, version="", model="gpt-4o-mini"):
    """One run: a root agent span, its LLM calls, then its tool calls (name, output)."""
    trace = hexid(16)
    base = {"gen_ai.agent.name": f"{PREFIX}-{workflow}"}
    root = span(trace, "", "agent", start, root_dur, {**base, "traceloop.span.kind": "workflow"}, client, version)
    for i in range(llm_calls):
        span(trace, root, f"chat {model}", start + 0.01 * i, 0.5, {
            **base,
            "gen_ai.operation.name": "chat",
            "gen_ai.request.model": model,
            "gen_ai.usage.input_tokens": input_tokens,
            "gen_ai.usage.output_tokens": 50,
            "gen_ai.usage.cache_read_input_tokens": cache_read,
            "gen_ai.response.finish_reasons": [finish],
            "gen_ai.completion.0.role": "assistant",
            "gen_ai.completion.0.content": output,
        }, client, version)
    for i, (name, out) in enumerate(tools):
        span(trace, root, name, start + 1 + 0.01 * i, 0.2, {
            **base,
            "traceloop.span.kind": "tool",
            "traceloop.entity.name": name,
            "traceloop.entity.output": out,
        }, client, version)


def baseline(workflow, per_hour=5, **kw):
    for h in range(1, 25):
        for k in range(per_hour):
            run(workflow, INCIDENT - h * HOUR + k * 60, **kw)


def incident(workflow, n, make):
    """n runs in the incident hour; make(k) returns run k's keyword arguments."""
    for k in range(n):
        run(workflow, INCIDENT + 60 + k * 60, **make(k))


# 1. Truncation: 8 of 12 calls cut off. "Yes." with finish stop is a short valid answer.
baseline("truncation", llm_calls=1, output="Yes.")
incident("truncation", 12, lambda k: dict(
    llm_calls=1,
    finish=["length", "MAX_TOKENS", "max_tokens", "FINISH_REASON_MAX_TOKENS"][k % 4] if k < 8 else "stop",
    output="The quarterly report covers" if k < 8 else "Yes."))

# 2. Refusal: 6 refusal phrases + 2 content_filter, 4 healthy look-alikes.
REFUSALS = ["I'm sorry, but I can't help with that.", "I cannot assist with that request."]
LOOKALIKES = ["Sorry for the wait! Here is your summary.", "I can't believe how simple this is: x = 2.",
              "Here is the policy. If asked for keys, say I'm sorry, but I can't.", "No."]
baseline("refusal", llm_calls=1, output=LOOKALIKES[0])
incident("refusal", 12, lambda k: dict(
    llm_calls=1,
    finish="content_filter" if k in (6, 7) else "stop",
    output=REFUSALS[k % 2] if k < 6 else ("" if k < 8 else LOOKALIKES[k - 8])))

# 3. Tool errors hidden in OK spans; outputs that only mention "error" stay clean.
CLEAN = ['{"result": "No error found in the logs"}', '{"error": null, "data": [1, 2]}',
         '{"errors": [], "items": []}', '{"status": 200, "items": [1]}', "error handling is in section 4"]
ERRORS = ['{"error": "rate limited"}', '{"status": 503, "body": "unavailable"}',
          '{"exception": "KeyError: id"}', '{"status": "error", "message": "bad input"}',
          '{"error": {"code": 500, "message": "boom"}}', '{"errors": [{"message": "not found"}]}']
baseline("tool-errors", llm_calls=1, tools=[("lookup", CLEAN[0])])
incident("tool-errors", 11, lambda k: dict(llm_calls=1, tools=[("lookup", ERRORS[k] if k < 6 else CLEAN[k - 6])]))

# 4. Runaway run: usually 3 LLM calls and 1 search; one run makes 40 calls and 15 searches.
baseline("loop", llm_calls=3, tools=[("search", '{"hits": 3}')])
incident("loop", 4, lambda k: dict(llm_calls=3, tools=[("search", '{"hits": 3}')]))
run("loop", INCIDENT + 600, 40, tools=[("search", '{"hits": 0}')] * 15)

# 5. Cache hit ratio: 1600/2000 cached in the baseline, nothing cached in the incident hour.
baseline("cache", llm_calls=2, input_tokens=2000, cache_read=1600)
incident("cache", 5, lambda k: dict(llm_calls=2, input_tokens=2000, cache_read=0))

# 6. Latency p95 (daily): 30 runs a day at 0.8s for 20 days, 9s today.
for d in range(1, 21):
    for k in range(30):
        run("latency", TODAY - d * DAY + 3600 + k * 60, 1, root_dur=0.8)
for k in range(30):
    run("latency", min(TODAY + 60 + k * 30, NOW - 60), 1, root_dur=9.0)

# 7. Heartbeat: sf-steady ran daily at 09:00 UTC until 3 days ago; sf-weekly runs every 7 days.
for d in range(3, 23):
    run("heartbeat", TODAY - d * DAY + 9 * HOUR, 1, client=f"{PREFIX}-steady")
for d in (6, 13, 20, 27):
    run("heartbeat", TODAY - d * DAY + 9 * HOUR, 1, client=f"{PREFIX}-weekly")

# 8. Release reports: v1 for a day, then v2. gpt-4o keeps cost per run above the $0.01 floor.
SHIP = NOW - 9000
for wf in ("release-bad", "release-good"):
    for h in range(1, 25):
        for k in range(5):
            run(wf, SHIP - h * HOUR + k * 60, 1, version="v1", model="gpt-4o", input_tokens=5000)
for k in range(40):
    run("release-bad", SHIP + k * 200, 1, version="v2", model="gpt-4o", input_tokens=10000,
        finish="length" if k % 4 == 0 else "stop", output="The quarterly report covers" if k % 4 == 0 else "Done.")
    run("release-good", SHIP + k * 200, 1, version="v2", model="gpt-4o", input_tokens=5000)
THIN = NOW - 25 * HOUR
for h in range(1, 25):
    run("release-thin", THIN - h * HOUR, 1, version="v1")
for k in range(5):
    run("release-thin", THIN + k * 600, 1, version="v2")


def post(spans, key):
    client, version = key
    res = [attr("service.name", f"{PREFIX}-mock")]
    if client:
        res.append(attr("tracium.user.id", client))
    if version:
        res.append(attr("tracium.workflow.version", version))
    body = {"resourceSpans": [{"resource": {"attributes": res},
                               "scopeSpans": [{"scope": {"name": "silent-failure-mock"}, "spans": spans}]}]}
    req = urllib.request.Request(ENDPOINT, data=json.dumps(body).encode(), method="POST", headers={
        "Content-Type": "application/json", "Authorization": f"Bearer {API_KEY}"})
    urllib.request.urlopen(req).read()


total = 0
for key, spans in resources.items():
    for i in range(0, len(spans), 500):
        post(spans[i:i + 500], key)
        total += len(spans[i:i + 500])
print(f"sent {total} spans to {ENDPOINT}; incident hour starts {time.strftime('%Y-%m-%d %H:%M UTC', time.gmtime(INCIDENT))}")
