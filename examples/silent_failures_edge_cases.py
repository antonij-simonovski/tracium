#!/usr/bin/env python3
"""
Edge-case traces for the silent-failure detectors. Stdlib only, OTLP/JSON.

Needs two workspaces with one ingest key each:
    KEYA=trc_... KEYB=trc_... python3 examples/silent_failures_edge_cases.py

Expected result in workspace A after package 00 (GET /v1/metrics/anomalies?range=7d):

    adv-lowvol-loop     llm_calls_per_run fires   (8 runs a day, one run with 200 calls)
    adv-trunc-steady    nothing                   (steady 20% truncation, 25% in the hour)
    adv-new-wf          truncation_rate fires     (new workflow, 60% truncated)
    adv-refusal-im      refusal_rate fires        ("I'm unable", "I'm not able")
    adv-refusal-fp      nothing                   ("I can't help but notice", hedged answers)
    adv-mcp-tools       tool_error_rate fires     (isError, success:false, status failed)
    adv-crawler         nothing                   (always calls fetch_page 12 times)
    adv-hourly          heartbeat fires           (24/7 client, silent for 5h)
    adv-office-sat2000  nothing                   (weekday 09-17 client, seen on a Saturday evening)
    adv-office-sun1400  nothing                   (same, seen on a Sunday afternoon)

Workspace B gets adv-b-only (refusal_rate). It must not appear in workspace A.

On commit 699e2db the results were: lowvol missed, refusal-im missed, mcp-tools
missed, refusal-fp fired, crawler fired, both office clients fired.
"""
import datetime as dt
import json
import os
import random
import time
import urllib.request

ENDPOINT = os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318/v1/traces")
HOUR, DAY = 3600, 86400
NOW = int(time.time())
INC = NOW // HOUR * HOUR - HOUR
out = {}


def hexid(n):
    return "%0*x" % (n * 2, random.getrandbits(n * 8))


def attr(k, v):
    if isinstance(v, int):
        return {"key": k, "value": {"intValue": str(v)}}
    if isinstance(v, list):
        return {"key": k, "value": {"arrayValue": {"values": [{"stringValue": x} for x in v]}}}
    return {"key": k, "value": {"stringValue": v}}


def span(key, client, tr, parent, name, start, dur, attrs):
    s = {"traceId": tr, "spanId": hexid(8), "name": name, "kind": 1,
         "startTimeUnixNano": str(int(start * 1e9)), "endTimeUnixNano": str(int((start + dur) * 1e9)),
         "attributes": [attr(k, v) for k, v in attrs.items()], "status": {"code": 1}}
    if parent:
        s["parentSpanId"] = parent
    out.setdefault((key, client), []).append(s)
    return s["spanId"]


def run(wf, start, calls=1, out_text="Done.", finish="stop", tools=(), key="A", client="", cache=0, inp=500):
    tr = hexid(16)
    base = {"gen_ai.agent.name": wf}
    root = span(key, client, tr, "", "agent", start, 2, {**base, "traceloop.span.kind": "workflow"})
    for i in range(calls):
        span(key, client, tr, root, "chat", start + 0.01 * i, 0.3, {
            **base, "gen_ai.operation.name": "chat", "gen_ai.request.model": "gpt-4o-mini",
            "gen_ai.usage.input_tokens": inp, "gen_ai.usage.output_tokens": 40,
            "gen_ai.usage.cache_read_input_tokens": cache, "gen_ai.response.finish_reasons": [finish],
            "gen_ai.completion.0.role": "assistant", "gen_ai.completion.0.content": out_text})
    for i, (n, o) in enumerate(tools):
        span(key, client, tr, root, n, start + 1 + 0.01 * i, 0.1, {
            **base, "traceloop.span.kind": "tool", "traceloop.entity.name": n, "traceloop.entity.output": o})


def baseline(wf, per_hour=5, **kw):
    for h in range(1, 25):
        for k in range(per_hour):
            run(wf, INC - h * HOUR + k * 60, **kw)


def incident(wf, n, f):
    for k in range(n):
        run(wf, INC + 60 + k * 60, **f(k))


for h in range(3, 25, 3):
    run("adv-lowvol-loop", INC - h * HOUR, calls=3)
run("adv-lowvol-loop", INC + 120, calls=200)

baseline("adv-trunc-steady", per_hour=10, calls=1, finish="stop")
for h in range(1, 25):
    for k in range(2):
        run("adv-trunc-steady", INC - h * HOUR + 900 + k, finish="length")
incident("adv-trunc-steady", 12, lambda k: dict(finish="length" if k < 3 else "stop"))

incident("adv-new-wf", 10, lambda k: dict(finish="length" if k < 6 else "stop"))

baseline("adv-refusal-im", calls=1, out_text="Here is your summary.")
R = ["I'm unable to help with that request.", "I'm sorry, but I'm unable to assist with that.",
     "I'm not able to provide that information."]
incident("adv-refusal-im", 12, lambda k: dict(out_text=R[k % 3] if k < 9 else "Here is your summary."))

baseline("adv-refusal-fp", calls=1, out_text="Here is your summary.")
V = ["I can't help but notice the query has a typo; here's the fixed SQL: SELECT 1;",
     "I cannot provide a definitive diagnosis, but common causes are A, B and C."]
incident("adv-refusal-fp", 12, lambda k: dict(out_text=V[k % 2]))

OK_TOOL = '{"content":[{"type":"text","text":"ok"}],"isError":false}'
baseline("adv-mcp-tools", calls=1, tools=[("read_file", OK_TOOL)])
E = ['{"content":[{"type":"text","text":"ENOENT: no such file"}],"isError":true}',
     '{"success": false, "message": "timeout"}', '{"status": "failed", "reason": "quota"}']
incident("adv-mcp-tools", 10, lambda k: dict(tools=[("read_file", E[k % 3] if k < 8 else OK_TOOL)]))

CRAWL = [("fetch_page", '{"html":"x"}')] * 12
baseline("adv-crawler", calls=2, tools=CRAWL)
incident("adv-crawler", 5, lambda k: dict(calls=2, tools=CRAWL))

for h in range(5, 28 * 24):
    run("adv-hb", NOW - h * HOUR, client="adv-hourly")


def office(client, weekday, hour):
    """A weekday 09-17 client, with history shifted so that 'now' falls on the given weekday and hour."""
    vn = dt.datetime.fromtimestamp(NOW, dt.timezone.utc)
    vn = (vn - dt.timedelta(days=(vn.weekday() - weekday) % 7)).replace(hour=hour, minute=30)
    shift = vn.timestamp() - NOW
    for d in range(1, 29):
        day = (vn - dt.timedelta(days=d)).replace(minute=0)
        for hh in range(9, 18):
            t = day.replace(hour=hh)
            if t.weekday() < 5 and t < vn:
                run("adv-hb", int(t.timestamp() - shift), client=client)


office("adv-office-sat2000", 5, 20)
office("adv-office-sun1400", 6, 14)

baseline("adv-b-only", calls=1, out_text="ok", key="B")
incident("adv-b-only", 10, lambda k: dict(finish="content_filter", key="B"))

keys = {"A": os.environ["KEYA"], "B": os.environ["KEYB"]}
n = 0
for (key, client), spans in out.items():
    res = [attr("service.name", "adv-mock")] + ([attr("tracium.user.id", client)] if client else [])
    for i in range(0, len(spans), 500):
        body = {"resourceSpans": [{"resource": {"attributes": res},
                                   "scopeSpans": [{"scope": {"name": "adv"}, "spans": spans[i:i + 500]}]}]}
        req = urllib.request.Request(ENDPOINT, json.dumps(body).encode(), {
            "Content-Type": "application/json", "Authorization": "Bearer " + keys[key]})
        urllib.request.urlopen(req).read()
        n += len(spans[i:i + 500])
print("sent", n, "spans; incident hour starts", time.strftime("%Y-%m-%d %H:%M UTC", time.gmtime(INC)))
