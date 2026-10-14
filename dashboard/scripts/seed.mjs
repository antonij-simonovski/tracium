// Seed the local Tracium collector with realistic OTLP trace data.
//
// Sends complex, multi-level agent workflows (tool-calling ReAct loops, parallel
// fan-out, nested sub-agents, retries) with full prompt/completion content and
// custom business attributes to the collector's OTLP/HTTP endpoint. Zero
// dependencies (Node 18+).
//
// `npm run seed` runs the account seeder first (scripts/seed-account.mjs) and
// then this script, so one command sets up the whole demo. This file is the
// telemetry half; run it alone with `npm run seed:data`.
//
//   npm run seed                       # account + workspaces + 520 traces
//   npm run seed:data                  # telemetry only (no account changes)
//   N_TRACES=1000 npm run seed:data    # more volume
//   OTLP_ENDPOINT=http://host:4318/v1/traces npm run seed:data
//   SEED=42 npm run seed:data          # reproducible run (default: random)
//
// The collector prices spans, captures content (capture_content: true), derives
// agents/kinds/tools, and writes to ClickHouse; the dashboard then reads it.
//
// Workflows ship releases over the history (see WORKFLOWS[].versions), each
// version sent the way real apps send one: tracium.workflow.version on the
// resource, the resource service.version, or a Traceloop managed prompt version
// on the LLM spans. research-assistant sends none.

const OTLP = process.env.OTLP_ENDPOINT || "http://localhost:4318/v1/traces";
const WORKSPACE_ID = process.env.WORKSPACE_ID || "c4ef3026-f040-4221-ad58-d6345dd4570c";
const N_TRACES = parseInt(process.env.N_TRACES || "1400", 10);

// Ingest now requires a per-workspace API key: the collector authenticates every
// OTLP request against the API and the key decides the workspace. So the seeder
// mints one key per workspace up front (via the API, as the seeded account) and
// sends each workspace's spans under its own key. These must match the account
// that owns the workspaces — run `npm run seed:account` first (or the combined
// `npm run seed`), so the workspaces and this account's membership exist.
const API = process.env.API_ENDPOINT || "http://localhost:8090";
const EMAIL = process.env.SEED_EMAIL || "demo@tracium.ai";
const PASSWORD = process.env.SEED_PASSWORD || "tracium-demo-1234";
// Name the seeder's keys so a re-run can revoke its previous ones instead of
// piling up a new key on every seed.
const SEED_KEY_NAME = "seed";

// Workspaces the seed data is distributed across. Each span carries its
// workspace's id in tracium.workspace.id (a first-class column), so the
// dashboard's WorkspaceSwitcher re-scopes every view. These ids are fixed so
// the account seeder (seed:account) can create matching workspace rows owned by
// the seeded account — the account → workspace → span mapping. Kept in sync
// with seed-account.mjs.
export const WORKSPACES = [
  { id: WORKSPACE_ID, name: "Production", slug: "production", env: "production", weight: 60 },
  { id: "7a1e9b52-3c8d-4f6a-b210-9d4e2f8c1a37", name: "Staging", slug: "staging", env: "staging", weight: 25 },
  { id: "2f5c8d13-6b47-49e2-8a1f-c3e6b0d94a58", name: "Development", slug: "development", env: "development", weight: 15 },
];
const NS = 1_000_000_000n;
const MS = 1_000_000n;
const DAY_NS = 86400n * NS;
const NOW_NS = BigInt(Date.now()) * MS;

// ---- PRNG (mulberry32). Random each run so re-seeding adds fresh, distinct
// data; set SEED=<n> for a reproducible run. -----------------------------------
let _s = (process.env.SEED ? parseInt(process.env.SEED, 10) : (Date.now() ^ (Math.random() * 2 ** 32))) >>> 0;
function rnd() {
  _s |= 0; _s = (_s + 0x6d2b79f5) | 0;
  let t = Math.imul(_s ^ (_s >>> 15), 1 | _s);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const randint = (lo, hi) => lo + Math.floor(rnd() * (hi - lo + 1));
const choice = (arr) => arr[Math.floor(rnd() * arr.length)];
const chance = (p) => rnd() < p;
function weighted(pairs) {
  const total = pairs.reduce((a, [, w]) => a + w, 0);
  let r = rnd() * total;
  for (const [item, w] of pairs) if ((r -= w) <= 0) return item;
  return pairs[pairs.length - 1][0];
}
const expo = (mean) => -Math.log(1 - rnd()) * mean;
const hexid = (bytes) =>
  Array.from({ length: bytes * 2 }, () => "0123456789abcdef"[Math.floor(rnd() * 16)]).join("");
const orderId = () => "ord_" + randint(100000, 999999);
const money = () => (randint(1200, 480000) / 100).toFixed(2);

// ---- OTLP/JSON value helpers -------------------------------------------------
const sv = (s) => ({ stringValue: String(s) });
const iv = (n) => ({ intValue: String(Math.trunc(n)) });
const av = (xs) => ({ arrayValue: { values: xs.map(sv) } });
const kv = (k, v) => ({ key: k, value: v });

// ---- metering users (tracium.user.id) — dashboard breaks cost down by this ---
const USERS = [
  ["u_umbrella", 34], ["u_acme_corp", 22], ["u_globex", 16],
  ["u_initech", 12], ["u_umbrella_dev", 9], ["u_personal", 7],
];
const ENVIRONMENTS = ["production", "production", "production", "staging", "development"];
const REGIONS = ["us-east-1", "us-west-2", "eu-west-1", "ap-south-1"];
const CUSTOMERS = ["acme-corp", "globex", "initech", "umbrella", "wayne-ent", "hooli"];
const PRIORITIES = ["low", "normal", "normal", "normal", "high"];

// =============================================================================
// Span-tree builder — tracks a monotonic clock and lets workflows nest spans,
// run branches in parallel, retry, and fail mid-run.
// =============================================================================
function newTrace(traceId, startNs) {
  const spans = [];
  let maxEnd = startNs;

  // Create a span. `opts`:
  //   op:     invoke_agent | chat | execute_tool | embeddings
  //   model:  priced model id (chat/embeddings)
  //   inTok/outTok
  //   system: system prompt (chat)
  //   input:  [{role,content}]  (chat)  — becomes gen_ai.input.messages
  //   output: string            (chat)  — becomes gen_ai.output.messages assistant turn
  //   toolCalls: ["tool_name"]  (chat)  — assistant tool-call turn (marks tools used)
  //   tools:  [{name,description}]      — gen_ai.tool.definitions -> available_tools
  //   entityIn/entityOut: JSON-ish strings for tool/agent structural spans
  //   finish: finish_reason override
  //   error:  {type,message}
  function span(parentId, name, startAt, durMs, opts = {}) {
    const spanId = hexid(8);
    const endNs = startAt + BigInt(durMs) * MS;
    if (endNs > maxEnd) maxEnd = endNs;
    const a = [kv("gen_ai.operation.name", sv(opts.op || "chat"))];
    if (opts.model) {
      a.push(kv("gen_ai.request.model", sv(opts.model)));
      a.push(kv("gen_ai.response.model", sv(opts.model)));
    }
    if (opts.inTok) a.push(kv("gen_ai.usage.input_tokens", iv(opts.inTok)));
    if (opts.outTok) a.push(kv("gen_ai.usage.output_tokens", iv(opts.outTok)));

    // content
    if (opts.system) a.push(kv("gen_ai.system_instructions", sv(opts.system)));
    if (opts.input) a.push(kv("gen_ai.input.messages", sv(JSON.stringify(opts.input))));
    if (opts.output !== undefined || opts.toolCalls) {
      const msg = { role: "assistant", content: opts.output || "" };
      if (opts.toolCalls) msg.tool_calls = opts.toolCalls.map((n) => ({ name: n }));
      a.push(kv("gen_ai.output.messages", sv(JSON.stringify([msg]))));
    }
    if (opts.tools) a.push(kv("gen_ai.tool.definitions", sv(JSON.stringify(opts.tools))));
    if (opts.entityIn !== undefined) a.push(kv("traceloop.entity.input", sv(opts.entityIn)));
    if (opts.entityOut !== undefined) a.push(kv("traceloop.entity.output", sv(opts.entityOut)));

    const sp = {
      traceId, spanId, parentSpanId: parentId || "", name, kind: 1,
      startTimeUnixNano: String(startAt), endTimeUnixNano: String(endNs),
      attributes: a, status: { code: 0 },
    };
    if (opts.error) {
      sp.status = { code: 2, message: opts.error.message };
      sp.events = [{
        name: "exception", timeUnixNano: sp.endTimeUnixNano,
        attributes: [kv("exception.type", sv(opts.error.type)),
                     kv("exception.message", sv(opts.error.message))],
      }];
    } else if (opts.finish) {
      a.push(kv("gen_ai.response.finish_reasons", av([opts.finish])));
    } else if (opts.model && opts.op !== "embeddings") {
      a.push(kv("gen_ai.response.finish_reasons", av(["stop"])));
    }
    spans.push(sp);
    return { id: spanId, start: startAt, end: endNs, sp };
  }

  const setEnd = (h, endNs) => { h.sp.endTimeUnixNano = String(endNs); if (endNs > maxEnd) maxEnd = endNs; };
  return { spans, span, setEnd, get maxEnd() { return maxEnd; } };
}

// =============================================================================
// Content pools
// =============================================================================
const TICKETS = [
  { t: "My invoice for March is $240 but I was told it'd be $180. Can you fix this?", intent: "billing_dispute", urgency: "high" },
  { t: "The mobile app crashes every time I open the reports tab on Android 14.", intent: "bug_report", urgency: "high" },
  { t: "How do I export my data to CSV? I can't find the button anywhere.", intent: "how_to", urgency: "low" },
  { t: "I want to cancel my subscription and get a refund for this month.", intent: "cancellation", urgency: "normal" },
  { t: "Order hasn't arrived and tracking says delivered. Need help urgently.", intent: "order_status", urgency: "high" },
];
const DIFFS = [
  "auth/session.go: add refresh-token rotation; drops the reuse-detection check",
  "api/handler/users.go: N+1 query in ListUsers; missing pagination bound",
  "web/checkout.tsx: unhandled promise rejection on payment retry path",
  "db/migrations/014.sql: adds NOT NULL column without default on a live table",
];
const RESEARCH = [
  "What are the tradeoffs of vector vs. keyword search for support KBs?",
  "Compare 2025 EU AI Act obligations for SaaS vendors vs. US state laws.",
  "Summarize recent advances in speculative decoding for LLM inference.",
];
const MOD_SAMPLES = [
  { t: "This product is garbage and the founder is a crook.", verdict: "allow", reason: "criticism, not harassment" },
  { t: "Meet me at the address below and I'll make you regret it.", verdict: "block", reason: "credible threat" },
  { t: "Buy cheap followers >> spam-link.example", verdict: "block", reason: "spam" },
  { t: "Loved the new update, the dashboard is so much faster!", verdict: "allow", reason: "benign" },
];

// =============================================================================
// Workflow builders. Each returns the failure step it aborted on (or null).
// Signature: build(T, rootId, ctx) where ctx has {rng helpers already global}.
// =============================================================================

// ReAct support agent: classify -> tool-augmented reasoning loop (2 turns) ->
// draft (with a chance of rate-limit + retry) -> moderate.
function wfSupport(T, root, seq) {
  const tk = choice(TICKETS);
  root.opts_in = `Customer ticket: ${tk.t}`;
  const tools = [
    { name: "search_kb", description: "Search the help center knowledge base" },
    { name: "get_order_status", description: "Look up an order's fulfillment status" },
    { name: "issue_refund", description: "Issue a partial or full refund" },
    { name: "escalate", description: "Escalate to a human agent" },
  ];
  let t = seq();
  const c = T.span(root.id, "classify-intent", t, randint(180, 640), {
    op: "chat", model: "claude-haiku-4-5", inTok: randint(420, 1100), outTok: randint(20, 90),
    system: "You triage support tickets. Reply with JSON {intent, urgency}.",
    input: [{ role: "user", content: tk.t }],
    output: JSON.stringify({ intent: tk.intent, urgency: tk.urgency }),
  });
  t = c.end + BigInt(randint(10, 40)) * MS;

  // reasoning turn 1 -> calls search_kb
  const r1 = T.span(root.id, "agent-step-1", t, randint(600, 2200), {
    op: "chat", model: "claude-sonnet-4-5", inTok: randint(1200, 3200), outTok: randint(120, 420),
    system: "You are a senior support agent. Use tools before answering. Think step by step.",
    input: [{ role: "user", content: tk.t }],
    tools, toolCalls: ["search_kb"], output: "Let me check the knowledge base for this issue.",
  });
  const kb = T.span(r1.id, "tool:search_kb", r1.end + BigInt(randint(5, 30)) * MS, randint(40, 320), {
    op: "execute_tool",
    entityIn: JSON.stringify({ query: tk.intent, top_k: 5 }),
    entityOut: JSON.stringify({ hits: [{ id: "kb_204", title: "Billing adjustments" }, { id: "kb_119", title: "Refund policy" }] }),
  });
  t = kb.end + BigInt(randint(10, 50)) * MS;

  // reasoning turn 2 -> calls get_order_status
  const r2 = T.span(root.id, "agent-step-2", t, randint(600, 2000), {
    op: "chat", model: "claude-sonnet-4-5", inTok: randint(1800, 4200), outTok: randint(140, 460),
    system: "You are a senior support agent. Use tools before answering. Think step by step.",
    input: [{ role: "assistant", content: "KB results: billing adjustments, refund policy" },
            { role: "user", content: "Continue resolving the ticket." }],
    tools, toolCalls: ["get_order_status"], output: `I'll verify the order details for ${orderId()}.`,
  });
  const os = T.span(r2.id, "tool:get_order_status", r2.end + BigInt(randint(5, 25)) * MS, randint(30, 240), {
    op: "execute_tool",
    entityIn: JSON.stringify({ order_id: orderId() }),
    entityOut: JSON.stringify({ status: "delivered", amount: money(), delivered_at: "2026-08-30" }),
  });
  t = os.end + BigInt(randint(10, 60)) * MS;

  // draft reply — sometimes rate-limited, then retried successfully
  const reply = `Hi! Thanks for reaching out about "${tk.intent}". I've reviewed your account and here's what I found and how we'll fix it...`;
  if (chance(0.09)) {
    const bad = T.span(root.id, "draft-reply", t, randint(120, 500), {
      op: "chat", model: "claude-sonnet-4-5", inTok: randint(1600, 3600),
      system: "Write a warm, concise support reply.",
      input: [{ role: "user", content: "Draft the reply." }],
      error: { type: "RateLimitError", message: "429 Too Many Requests: token bucket exhausted" },
    });
    t = bad.end + BigInt(randint(300, 1200)) * MS; // backoff
  }
  T.span(root.id, "draft-reply", t, randint(700, 3200), {
    op: "chat", model: "claude-sonnet-4-5", inTok: randint(1600, 3800), outTok: randint(180, 700),
    system: "Write a warm, concise support reply.",
    input: [{ role: "user", content: "Draft the reply using the KB and order info." }],
    output: reply,
  });
  root.opts_out = reply;

  // moderation gate — occasionally flags
  const modBad = chance(0.02);
  T.span(root.id, "moderate-reply", T.maxEnd + BigInt(randint(10, 40)) * MS, randint(120, 460), {
    op: "chat", model: "claude-haiku-4-5", inTok: randint(260, 800), outTok: randint(8, 30),
    system: "Content safety gate. Return {safe:bool}.",
    input: [{ role: "user", content: reply }],
    output: JSON.stringify({ safe: !modBad }),
    finish: modBad ? "content_filter" : undefined,
  });
  return null;
}

// Invoice extractor: parse (fn-calling, sometimes context-length fail) ->
// PARALLEL field extractors -> validate tool -> reconcile.
function wfInvoice(T, root, seq) {
  root.opts_in = `Extract structured data from invoice PDF (vendor, line items, totals).`;
  const parseFail = chance(0.07);
  let t = seq();
  const parse = T.span(root.id, "parse-document", t, randint(1200, 4200), parseFail ? {
    op: "chat", model: "gpt-4o", inTok: randint(120000, 145000),
    system: "Extract invoice fields as JSON.",
    input: [{ role: "user", content: "<document: 38-page scanned invoice>" }],
    error: { type: "ContextLengthExceeded", message: "maximum context length is 128000 tokens; request had 141220" },
    finish: "length",
  } : {
    op: "chat", model: "gpt-4o", inTok: randint(2200, 7800), outTok: randint(300, 900),
    system: "Extract invoice fields. Call extract_fields with the parsed structure.",
    input: [{ role: "user", content: "<document: multi-page invoice>" }],
    tools: [{ name: "extract_fields", description: "Persist the parsed invoice fields" }],
    toolCalls: ["extract_fields"],
    output: "Parsed vendor, 12 line items and totals; persisting fields.",
  });
  if (parseFail) return "parse-document";
  t = parse.end + BigInt(randint(10, 50)) * MS;

  // parallel field extractors share a start point
  const branchStart = t;
  const fields = [
    ["extract-line-items", "12 line items itemized"],
    ["extract-totals", `subtotal ${money()}, tax ${money()}, total ${money()}`],
    ["extract-vendor", "vendor: Globex Industrial LLC, VAT GB123456789"],
  ];
  for (const [nm, out] of fields) {
    T.span(root.id, nm, branchStart + BigInt(randint(0, 60)) * MS, randint(500, 2100), {
      op: "chat", model: "gpt-4o-mini", inTok: randint(1400, 4200), outTok: randint(220, 640),
      system: "Extract a single field group from the invoice as JSON.",
      input: [{ role: "user", content: "Parsed invoice context ..." }],
      output: out,
    });
  }
  const valStart = T.maxEnd + BigInt(randint(10, 40)) * MS;
  const val = T.span(root.id, "validate-totals", valStart, randint(30, 240), {
    op: "execute_tool",
    entityIn: JSON.stringify({ subtotal: money(), tax: money(), total: money() }),
    entityOut: JSON.stringify({ balanced: true }),
  });
  const rec = `Reconciled invoice; totals balance. Ready for AP approval.`;
  T.span(root.id, "reconcile", val.end + BigInt(randint(10, 40)) * MS, randint(400, 1600), {
    op: "chat", model: "gpt-4o", inTok: randint(900, 2600), outTok: randint(120, 380),
    system: "Reconcile extracted fields and flag anomalies.",
    input: [{ role: "user", content: "line items + totals + vendor" }],
    output: rec,
  });
  root.opts_out = rec;
  return null;
}

// Code review: fetch diff -> PARALLEL per-file analysis (one may time out+retry)
// -> suggest fixes -> nested security sub-agent -> summarize.
function wfCodeReview(T, root, seq) {
  const files = [choice(DIFFS), choice(DIFFS), choice(DIFFS)];
  root.opts_in = `Review PR #${randint(200, 4800)} touching ${files.length} files.`;
  let t = seq();
  const fetch = T.span(root.id, "fetch-diff", t, randint(40, 260), {
    op: "execute_tool",
    entityIn: JSON.stringify({ pr: randint(200, 4800) }),
    entityOut: JSON.stringify({ files: files.length, additions: randint(20, 900), deletions: randint(5, 400) }),
  });
  const branchStart = fetch.end + BigInt(randint(10, 40)) * MS;
  const timeoutIdx = chance(0.05) ? randint(0, files.length - 1) : -1;
  files.forEach((f, i) => {
    if (i === timeoutIdx) {
      const bad = T.span(root.id, `analyze-file[${i}]`, branchStart + BigInt(randint(0, 80)) * MS, randint(200, 900), {
        op: "chat", model: "claude-opus-4-5", inTok: randint(3200, 9000),
        system: "Review this file for bugs, security and style.",
        input: [{ role: "user", content: f }],
        error: { type: "APITimeoutError", message: "request timed out after 60s" },
      });
      T.span(root.id, `analyze-file[${i}]:retry`, bad.end + BigInt(randint(400, 1500)) * MS, randint(2400, 8000), {
        op: "chat", model: "claude-opus-4-5", inTok: randint(3200, 12000), outTok: randint(600, 2200),
        system: "Review this file for bugs, security and style.",
        input: [{ role: "user", content: f }],
        output: `Found 2 issues in ${f.split(":")[0]}: a correctness bug and a missing bound.`,
      });
    } else {
      T.span(root.id, `analyze-file[${i}]`, branchStart + BigInt(randint(0, 80)) * MS, randint(2400, 8800), {
        op: "chat", model: "claude-opus-4-5", inTok: randint(3200, 12000), outTok: randint(600, 2200),
        system: "Review this file for bugs, security and style.",
        input: [{ role: "user", content: f }],
        output: `Reviewed ${f.split(":")[0]}: ${chance(0.5) ? "1 blocking issue" : "looks good, minor nits"}.`,
      });
    }
  });
  const sf = T.span(root.id, "suggest-fixes", T.maxEnd + BigInt(randint(10, 50)) * MS, randint(1100, 4200), {
    op: "chat", model: "claude-sonnet-4-5", inTok: randint(1800, 6400), outTok: randint(400, 1400),
    system: "Propose concrete patch suggestions for the issues found.",
    input: [{ role: "user", content: "aggregated file findings" }],
    output: "Suggested 3 diffs: add pagination bound, rotate refresh token safely, guard payment retry.",
  });
  // nested security sub-agent
  const sub = T.span(root.id, "security-subagent", sf.end + BigInt(randint(10, 40)) * MS, randint(900, 3000), {
    op: "invoke_agent",
    entityIn: JSON.stringify({ scope: "changed files", checks: ["secrets", "authz", "injection"] }),
    entityOut: JSON.stringify({ findings: chance(0.3) ? 1 : 0 }),
  });
  T.span(sub.id, "scan-vulnerabilities", sub.start + BigInt(randint(20, 120)) * MS, randint(700, 2400), {
    op: "chat", model: "claude-sonnet-4-5", inTok: randint(1400, 4800), outTok: randint(200, 800),
    system: "Static security review. Report CWE-tagged findings.",
    input: [{ role: "user", content: "diff hunks" }],
    output: chance(0.3) ? "CWE-639: IDOR risk in ListUsers pagination param." : "No high-severity findings.",
  });
  const summary = "Requested changes: 1 blocking correctness bug, 2 suggestions. Approving after fixes.";
  T.span(root.id, "summarize-review", T.maxEnd + BigInt(randint(10, 40)) * MS, randint(240, 900), {
    op: "chat", model: "claude-haiku-4-5", inTok: randint(700, 2100), outTok: randint(90, 320),
    system: "Write the final PR review summary.",
    input: [{ role: "user", content: "all findings" }],
    output: summary,
  });
  root.opts_out = summary;
  return null;
}

// Semantic search RAG: embed -> vector search tool -> rerank -> answer.
function wfSearch(T, root, seq) {
  const q = choice(["reset my 2FA device", "export invoices to CSV", "SSO with Okta", "rate limits on the API"]);
  root.opts_in = `Query: ${q}`;
  let t = seq();
  const emb = T.span(root.id, "embed-query", t, randint(20, 160), {
    op: "embeddings", model: "text-embedding-3-small", inTok: randint(12, 90),
    input: [{ role: "user", content: q }],
  });
  const vs = T.span(root.id, "vector-search", emb.end + BigInt(randint(5, 25)) * MS, randint(20, 180), {
    op: "execute_tool",
    entityIn: JSON.stringify({ k: 8, filter: { lang: "en" } }),
    entityOut: JSON.stringify({ ids: ["doc_12", "doc_88", "doc_3"], scores: [0.83, 0.79, 0.71] }),
  });
  const rr = T.span(root.id, "rerank-results", vs.end + BigInt(randint(5, 25)) * MS, randint(180, 800), {
    op: "chat", model: "gpt-4o-mini", inTok: randint(600, 2200), outTok: randint(40, 160),
    system: "Rerank passages by relevance to the query.",
    input: [{ role: "user", content: q }],
    output: "Top passage: doc_88 (setup guide).",
  });
  const ans = `Here's how to ${q}: follow steps 1–3 in the linked guide.`;
  T.span(root.id, "generate-answer", rr.end + BigInt(randint(5, 25)) * MS, randint(500, 2400), {
    op: "chat", model: "claude-sonnet-4-5", inTok: randint(1200, 3600), outTok: randint(120, 520),
    system: "Answer using only the retrieved passages; cite doc ids.",
    input: [{ role: "user", content: q }],
    output: ans,
  });
  root.opts_out = ans;
  return null;
}

// Content moderation: classify -> (if borderline) explain. High volume.
function wfModeration(T, root, seq) {
  const s = choice(MOD_SAMPLES);
  root.opts_in = s.t;
  const refusal = chance(0.03);
  const rate = chance(0.01);
  let t = seq();
  if (rate) {
    T.span(root.id, "moderate-content", t, randint(60, 200), {
      op: "chat", model: "claude-haiku-4-5", inTok: randint(180, 500),
      system: "Classify content: allow/block with a reason.",
      input: [{ role: "user", content: s.t }],
      error: { type: "RateLimitError", message: "429 Too Many Requests" },
    });
    return "moderate-content";
  }
  const cls = T.span(root.id, "moderate-content", t, randint(80, 420), {
    op: "chat", model: "claude-haiku-4-5", inTok: randint(180, 620), outTok: randint(6, 30),
    system: "Classify content: allow/block with a reason.",
    input: [{ role: "user", content: s.t }],
    output: JSON.stringify({ verdict: s.verdict, reason: s.reason }),
    finish: refusal ? "content_filter" : undefined,
  });
  root.opts_out = JSON.stringify({ verdict: s.verdict });
  if (s.verdict === "block" && chance(0.6)) {
    T.span(root.id, "explain-decision", cls.end + BigInt(randint(5, 20)) * MS, randint(120, 520), {
      op: "chat", model: "claude-haiku-4-5", inTok: randint(200, 700), outTok: randint(40, 160),
      system: "Explain the moderation decision to an appeals reviewer.",
      input: [{ role: "user", content: s.t }],
      output: `Blocked: ${s.reason}. Policy §3.2.`,
    });
  }
  return null;
}

// Deep research: plan -> PARALLEL per-subquestion (search+read+summarize) ->
// synthesize (overloaded+retry) -> verify citations w/ tool.
function wfResearch(T, root, seq) {
  const q = choice(RESEARCH);
  root.opts_in = q;
  let t = seq();
  const subqs = ["definitions & scope", "current state of the art", "tradeoffs & risks"];
  const plan = T.span(root.id, "plan-research", t, randint(900, 3600), {
    op: "chat", model: "gemini-2.5-pro", inTok: randint(900, 2800), outTok: randint(300, 900),
    system: "Decompose the research question into 3 sub-questions.",
    input: [{ role: "user", content: q }],
    output: JSON.stringify({ subquestions: subqs }),
  });
  const branchStart = plan.end + BigInt(randint(10, 60)) * MS;
  subqs.forEach((sq, i) => {
    const web = T.span(root.id, `web-search[${i}]`, branchStart + BigInt(randint(0, 120)) * MS, randint(120, 900), {
      op: "execute_tool",
      entityIn: JSON.stringify({ q: sq }),
      entityOut: JSON.stringify({ results: randint(4, 12) }),
    });
    const read = T.span(web.id, `read-source[${i}]`, web.end + BigInt(randint(5, 30)) * MS, randint(80, 500), {
      op: "execute_tool",
      entityIn: JSON.stringify({ url: `https://example.com/${i}` }),
      entityOut: JSON.stringify({ chars: randint(2000, 18000) }),
    });
    T.span(read.id, `summarize[${i}]`, read.end + BigInt(randint(5, 30)) * MS, randint(600, 2400), {
      op: "chat", model: "gemini-2.5-flash", inTok: randint(1800, 6000), outTok: randint(200, 800),
      system: "Summarize the source for the sub-question with citations.",
      input: [{ role: "user", content: sq }],
      output: `Summary for "${sq}" with 2 citations.`,
    });
  });

  // synthesize — may be overloaded then retried
  let st = T.maxEnd + BigInt(randint(10, 60)) * MS;
  if (chance(0.07)) {
    const bad = T.span(root.id, "synthesize", st, randint(200, 900), {
      op: "chat", model: "gemini-2.5-pro", inTok: randint(3000, 8000),
      system: "Synthesize the sub-summaries into a briefing.",
      input: [{ role: "user", content: "sub-summaries" }],
      error: { type: "OverloadedError", message: "503 model temporarily unavailable" },
    });
    st = bad.end + BigInt(randint(500, 2000)) * MS;
  }
  const brief = `Briefing on "${q}": key findings across ${subqs.length} dimensions with sourced citations.`;
  const syn = T.span(root.id, "synthesize", st, randint(1400, 5200), {
    op: "chat", model: "gemini-2.5-pro", inTok: randint(3400, 11000), outTok: randint(500, 1800),
    system: "Synthesize the sub-summaries into a briefing with inline citations.",
    input: [{ role: "user", content: "sub-summaries" }],
    output: brief,
  });
  const ver = T.span(root.id, "verify-citations", syn.end + BigInt(randint(10, 40)) * MS, randint(800, 3000), {
    op: "chat", model: "o3-mini", inTok: randint(1200, 3600), outTok: randint(400, 1600),
    system: "Verify each citation resolves and supports its claim. Use check_source.",
    input: [{ role: "user", content: brief }],
    tools: [{ name: "check_source", description: "Fetch a URL and verify it supports a claim" }],
    toolCalls: ["check_source"],
    output: "All 6 citations verified.",
  });
  T.span(ver.id, "tool:check_source", ver.start + BigInt(randint(20, 120)) * MS, randint(60, 400), {
    op: "execute_tool",
    entityIn: JSON.stringify({ url: "https://example.com/2" }),
    entityOut: JSON.stringify({ ok: true }),
  });
  root.opts_out = brief;
  return null;
}

// versions: { source, releases: [[version, released days ago, token factor?]] },
// oldest first. The token factor makes a release cheaper or costlier per run so
// comparing versions shows a real difference.
const WORKFLOWS = [
  {
    workflow: "support-ticket-resolver", team: "support", feature: "ticket-triage", cost_center: "CC-1001", weight: 20, build: wfSupport,
    versions: { source: "explicit", releases: [["v2.1.0", 42], ["v2.2.0", 18, 0.85], ["v2.3.0", 6, 1.3]] },
  },
  {
    workflow: "invoice-extractor", team: "finance", feature: "invoice-ocr", cost_center: "CC-2100", weight: 14, build: wfInvoice,
    versions: { source: "service", releases: [["1.8.3", 42], ["1.9.0", 11, 0.9]] },
  },
  {
    workflow: "code-review-bot", team: "engineering", feature: "pr-review", cost_center: "CC-3050", weight: 12, build: wfCodeReview,
    versions: { source: "explicit", releases: [["3f9c2e1", 42], ["a71d04b", 20], ["c5e88f2", 8, 0.8]] },
  },
  {
    workflow: "semantic-search", team: "growth", feature: "kb-search", cost_center: "CC-4200", weight: 14, build: wfSearch,
    versions: { source: "prompt", releases: [["3", 42], ["4", 14, 1.15]] },
  },
  {
    workflow: "content-moderation", team: "trust-safety", feature: "auto-moderation", cost_center: "CC-5500", weight: 30, build: wfModeration,
    versions: { source: "explicit", releases: [["2026.09.1", 42], ["2026.10.0", 13]] },
  },
  { workflow: "research-assistant", team: "research", feature: "deep-research", cost_center: "CC-6300", weight: 10, build: wfResearch },
];

// releaseAt returns the release a workflow was running startNs, or undefined.
function releaseAt(wf, startNs) {
  const daysAgo = Number((NOW_NS - startNs) / DAY_NS);
  return wf.versions?.releases.filter(([, since]) => since >= daysAgo).at(-1);
}

// makeTrace builds one trace starting at startNs. forceWf pins the workflow (for
// injected per-agent anomalies); omitted, it is chosen by weight as normal.
function makeTrace(startNs, forceWf) {
  const wf = forceWf || weighted(WORKFLOWS.map((w) => [w, w.weight]));
  const userId = weighted(USERS);
  const ws = weighted(WORKSPACES.map((w) => [w, w.weight]));
  const traceId = hexid(16);
  // environment follows the workspace so a "Staging" workspace's spans read as
  // staging — keeps the workspace scope and the environment attribute coherent.
  const env = ws.env;
  const region = choice(REGIONS);
  const customer = choice(CUSTOMERS);
  const priority = choice(PRIORITIES);

  const T = newTrace(traceId, startNs);
  // root structural agent span; content filled in by the builder via root.opts_*
  const root = T.span("", wf.workflow, startNs, 10, { op: "invoke_agent" });
  let cursor = startNs + BigInt(randint(20, 120)) * MS;
  const seq = () => cursor;
  wf.build(T, root, seq);

  const release = releaseAt(wf, startNs);
  const [version, , tokenFactor = 1] = release || [];

  // finalize root: envelope + entity content the builder recorded
  T.setEnd(root, T.maxEnd + BigInt(randint(10, 90)) * MS);
  if (root.opts_in !== undefined) root.sp.attributes.push(kv("traceloop.entity.input", sv(root.opts_in)));
  if (root.opts_out !== undefined) root.sp.attributes.push(kv("traceloop.entity.output", sv(root.opts_out)));

  // No tracium.workspace.id here: the ingest key decides the workspace, and the
  // collector stamps it (overriding any sender-supplied value). The seeder groups
  // spans by _workspaceId below and sends each group under that workspace's key.
  const resourceAttrs = [
    kv("service.name", sv(wf.workflow)),
    kv("tracium.user.id", sv(userId)),
    kv("environment", sv(env)),
    kv("region", sv(region)),
    kv("team", sv(wf.team)),
    kv("customer", sv(customer)),
    kv("cost_center", sv(wf.cost_center)),
    kv("feature", sv(wf.feature)),
    kv("priority", sv(priority)),
    kv("deployment.environment", sv(env)),
  ];
  if (version && wf.versions.source === "explicit") resourceAttrs.push(kv("tracium.workflow.version", sv(version)));
  if (version && wf.versions.source === "service") resourceAttrs.push(kv("service.version", sv(version)));
  if (version && wf.versions.source === "prompt") {
    for (const sp of T.spans) {
      if (sp.attributes.some((a) => a.key === "gen_ai.request.model")) {
        sp.attributes.push(kv("traceloop.prompt.key", sv(wf.workflow)), kv("traceloop.prompt.version", sv(version)));
      }
    }
  }
  const trace = {
    resource: { attributes: resourceAttrs },
    scopeSpans: [{ scope: { name: "tracium.seed" }, spans: T.spans }],
    _spanCount: T.spans.length,
    _workspaceId: ws.id,
  };
  if (tokenFactor !== 1) scaleTokens(trace, tokenFactor);
  return trace;
}

// ---- ingest-key provisioning -------------------------------------------------
// Ingest is key-only. The seeder authenticates as the seeded account and mints
// one key per workspace; each OTLP request then carries its workspace's key.

async function authenticate() {
  const body = JSON.stringify({ email: EMAIL, password: PASSWORD });
  let res = await fetch(`${API}/v1/auth/register`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body,
  });
  if (res.status === 409) {
    res = await fetch(`${API}/v1/auth/login`, {
      method: "POST", headers: { "Content-Type": "application/json" }, body,
    });
  }
  if (!res.ok) throw new Error(`auth failed (${res.status}): ${await res.text()}`);
  return (await res.json()).token;
}

// provisionKeys returns a Map of workspaceId -> plaintext ingest token, minting a
// fresh "seed" key in each workspace (revoking any prior one first, so re-seeding
// does not accumulate keys). Requires the workspaces + this account's membership
// to already exist (npm run seed:account).
async function provisionKeys(token) {
  const auth = { Authorization: `Bearer ${token}`, "Content-Type": "application/json" };
  const keys = new Map();
  for (const ws of WORKSPACES) {
    const base = `${API}/v1/workspaces/${ws.id}/api-keys`;
    // Revoke any previous seed keys so runs stay idempotent.
    const listRes = await fetch(base, { headers: auth });
    if (listRes.status === 403 || listRes.status === 404) {
      throw new Error(
        `workspace ${ws.name} (${ws.id}) not accessible as ${EMAIL} — run \`npm run seed:account\` first`,
      );
    }
    if (!listRes.ok) throw new Error(`list keys failed (${listRes.status}): ${await listRes.text()}`);
    for (const k of await listRes.json()) {
      if (k.name === SEED_KEY_NAME && !k.revoked_at) {
        await fetch(`${base}/${k.id}`, { method: "DELETE", headers: auth });
      }
    }
    // Mint the fresh key and capture its one-time token.
    const createRes = await fetch(base, {
      method: "POST", headers: auth, body: JSON.stringify({ name: SEED_KEY_NAME }),
    });
    if (!createRes.ok) throw new Error(`create key failed (${createRes.status}): ${await createRes.text()}`);
    const created = await createRes.json();
    keys.set(ws.id, created.token);
    console.log(`  minted ingest key for ${ws.name.padEnd(12)} ${created.key.prefix}…`);
  }
  return keys;
}

async function post(resourceSpans, token) {
  const body = JSON.stringify({
    resourceSpans: resourceSpans.map(({ _spanCount, _workspaceId, ...r }) => r),
  });
  const res = await fetch(OTLP, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`OTLP ${res.status}: ${text}`);
  return res.status;
}

// ---- anomaly injection -------------------------------------------------------
// The baseline is deliberately stable (see the schedule in main) so that a few
// injected outliers stand out the way real incidents do, instead of every recent
// day looking anomalous. These helpers mutate a built trace in place.

const findWorkflow = (workflow) => WORKFLOWS.find((w) => w.workflow === workflow);

// scaleTokens multiplies every LLM span's token usage, inflating the trace's
// priced cost — a runaway-context / prompt-bloat cost spike.
function scaleTokens(trace, factor) {
  for (const sp of trace.scopeSpans[0].spans) {
    for (const a of sp.attributes) {
      if (a.key === "gen_ai.usage.input_tokens" || a.key === "gen_ai.usage.output_tokens") {
        a.value = iv(Math.round(parseInt(a.value.intValue, 10) * factor));
      }
    }
  }
}

// forceError marks the trace's first LLM span failed, matching the span builder's
// error shape (status code 2 + an exception event) so the collector records it.
function forceError(trace, type, message) {
  for (const sp of trace.scopeSpans[0].spans) {
    if (sp.attributes.some((a) => a.key === "gen_ai.request.model")) {
      sp.status = { code: 2, message };
      sp.events = [{ name: "exception", timeUnixNano: sp.endTimeUnixNano, attributes: [kv("exception.type", sv(type)), kv("exception.message", sv(message))] }];
      return;
    }
  }
}

// dayStartNs returns the UTC-midnight-ish start (ns) of the bucket `daysAgo` days
// before now; a random within-day offset is added by the caller.
function dayStartNs(nowNs, daysAgo) {
  return ((nowNs / DAY_NS) - BigInt(daysAgo)) * DAY_NS;
}
// withinDay adds a random hour/minute offset so a day's traces spread across it.
const withinDay = () => BigInt(randint(0, 23 * 3600 + 3540)) * NS;

async function main() {
  const nowNs = NOW_NS;
  // History spans HISTORY_DAYS so the 28-day detection baseline is fully
  // populated with real, active days before the recent display window.
  const HISTORY_DAYS = 42;
  // Weekend days carry less traffic (mild weekly seasonality), everything else is
  // uniform with per-trace noise — a stable baseline, not a recent-heavy ramp.
  const dayWeight = (daysAgo) => {
    const d = new Date(Number(nowNs / MS) - daysAgo * 86400000).getUTCDay();
    return d === 0 || d === 6 ? 0.6 : 1;
  };
  const dayPicker = Array.from({ length: HISTORY_DAYS }, (_, k) => [HISTORY_DAYS - 1 - k, dayWeight(HISTORY_DAYS - 1 - k)]);

  // Baseline traces: spread across the whole history by day weight.
  const tasks = [];
  for (let i = 0; i < N_TRACES; i++) {
    const daysAgo = weighted(dayPicker);
    tasks.push({ startNs: dayStartNs(nowNs, daysAgo) + withinDay() });
  }

  // ---- injected incidents, on recent days so they show in the 7d view --------
  // 1. Cost spike: support-ticket-resolver blows its token budget two days ago.
  const supportWf = findWorkflow("support-ticket-resolver");
  for (let i = 0; i < 20; i++) {
    tasks.push({ startNs: dayStartNs(nowNs, 2) + withinDay(), wf: supportWf, mutate: (t) => scaleTokens(t, 9) });
  }
  // 2. Error cluster: code-review-bot rate-limited three days ago.
  const codeWf = findWorkflow("code-review-bot");
  for (let i = 0; i < 26; i++) {
    tasks.push({ startNs: dayStartNs(nowNs, 3) + withinDay(), wf: codeWf, mutate: (t) => forceError(t, "RateLimitError", "429 Too Many Requests: token bucket exhausted") });
  }
  // 3. Volume spike: a traffic surge across the workspace yesterday.
  for (let i = 0; i < 95; i++) {
    tasks.push({ startNs: dayStartNs(nowNs, 1) + withinDay() });
  }

  // Provision one ingest key per workspace before sending anything.
  console.log(`authenticating as ${EMAIL} and minting ingest keys -> ${API}`);
  const token = await authenticate();
  const keys = await provisionKeys(token);

  const total = tasks.length;
  console.log(`seeding ${total} traces over ${HISTORY_DAYS}d (baseline ${N_TRACES} + injected incidents) -> ${OTLP}`);
  let batch = [], sent = 0, spans = 0;
  const flush = async () => {
    if (!batch.length) return;
    // The key decides the workspace, so a request carries one workspace's spans.
    // Group the batch by workspace and send each group under its own key.
    const byWs = new Map();
    for (const t of batch) {
      if (!byWs.has(t._workspaceId)) byWs.set(t._workspaceId, []);
      byWs.get(t._workspaceId).push(t);
    }
    let lastStatus = 0;
    for (const [wsId, group] of byWs) {
      lastStatus = await post(group, keys.get(wsId));
    }
    sent += batch.length;
    console.log(`  sent ${sent}/${total} traces (http ${lastStatus})`);
    batch = [];
  };
  for (const task of tasks) {
    const t = makeTrace(task.startNs, task.wf);
    if (task.mutate) task.mutate(t);
    spans += t._spanCount;
    batch.push(t);
    if (batch.length >= 30) await flush();
  }
  await flush();
  console.log(`done: ${sent} traces, ${spans} spans (avg ${(spans / sent).toFixed(1)} spans/trace).`);
  console.log("Injected: cost spike (support-ticket-resolver, 2d ago), error cluster (code-review-bot, 3d ago), volume surge (yesterday).");
  console.log("Versions: support-ticket-resolver v2.3.0 (6d ago) costs more per run than v2.2.0 and carries the cost spike; research-assistant sends no version.");
  console.log("Give the collector a few seconds to flush, then refresh the dashboard.");
}

// Run only when invoked directly (`npm run seed`), not when imported for its
// exported WORKSPACES (e.g. by seed-account.mjs).
if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch((e) => { console.error(e.message || e); process.exit(1); });
}
