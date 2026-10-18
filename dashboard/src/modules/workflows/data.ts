import type { Workflow, WorkflowMeta } from './interfaces';

// Demo workflows for the logged-out auth-page preview (embedded mode). The live app
// fetches the same shape from GET /v1/metrics/workflows. avg_latency_ms is in
// milliseconds; error_rate is a fraction (0–1). last_trace_id points at the demo
// trace so a row click resolves in the embedded preview.
const DEMO_TRACE = "t_a9f2_eb7c";

export const WORKFLOWS: Workflow[] = [
  { name: "summarize-comments",              calls: 847,  cost: 0.4234, avg_latency_ms: 2100, error_rate: 0.002, trend: [12,14,18,16,22,28,34,31,29,38,42,39,44],    last_trace_id: DEMO_TRACE },
  { name: "generate-clock-out-description",  calls: 412,  cost: 0.2891, avg_latency_ms: 3400, error_rate: 0.0,   trend: [8,10,9,12,14,16,15,18,20,19,22,24,21],       last_trace_id: DEMO_TRACE },
  { name: "classify-intent",                 calls: 1203, cost: 0.1204, avg_latency_ms: 800,  error_rate: 0.014, trend: [42,48,52,49,55,61,58,63,72,68,74,81,88],      last_trace_id: DEMO_TRACE },
  { name: "extract-entities",                calls: 328,  cost: 0.0892, avg_latency_ms: 1200, error_rate: 0.0,   trend: [4,5,6,5,7,8,7,9,10,9,11,12,10],              last_trace_id: DEMO_TRACE },
  { name: "rewrite-message",                 calls: 156,  cost: 0.3104, avg_latency_ms: 4800, error_rate: 0.083, trend: [2,3,4,3,5,6,5,8,12,14,18,22,28],             last_trace_id: DEMO_TRACE },
  { name: "detect-sentiment",                calls: 612,  cost: 0.0412, avg_latency_ms: 600,  error_rate: 0.003, trend: [18,20,22,19,24,26,28,25,30,32,34,31,35],      last_trace_id: DEMO_TRACE },
  { name: "summarize-thread",                calls: 89,   cost: 0.0834, avg_latency_ms: 3100, error_rate: 0.011, trend: [1,2,3,2,4,5,4,6,7,8,9,11,12],                last_trace_id: DEMO_TRACE },
  { name: "moderate-content",                calls: 2104, cost: 0.0892, avg_latency_ms: 400,  error_rate: 0.001, trend: [80,85,92,88,96,102,108,112,118,124,128,132,138], last_trace_id: DEMO_TRACE },
];

// Demo configuration metadata for the workflow detail page, keyed by workflow name.
export const WORKFLOW_META: Record<string, WorkflowMeta> = {
  "summarize-comments": {
    description: "Condenses long comment threads into a 3–5 bullet digest with sentiment and unresolved-question flags. Runs on every thread close.",
    model: "claude-sonnet-4-5", provider: "anthropic", version: "v3.1.0",
    temperature: 0.2, maxTokens: 1024, timeout: "45s", retries: 1, endpoint: "workflows.summarize-comments",
  },
  "generate-clock-out-description": {
    description: "Drafts an end-of-shift summary from a worker's activity log, matching the org's reporting template and tone.",
    model: "claude-sonnet-4-5", provider: "anthropic", version: "v1.6.2",
    temperature: 0.4, maxTokens: 768, timeout: "30s", retries: 2, endpoint: "workflows.generate-clock-out-description",
  },
  "classify-intent": {
    description: "Routes inbound messages into one of 18 intent labels with a confidence score. Powers downstream automation triggers.",
    model: "claude-haiku-4-5", provider: "anthropic", version: "v4.2.0",
    temperature: 0.0, maxTokens: 128, timeout: "15s", retries: 2, endpoint: "workflows.classify-intent",
  },
  "extract-entities": {
    description: "Pulls structured entities (people, orgs, dates, amounts) from free text and returns them as typed spans with offsets.",
    model: "claude-haiku-4-5", provider: "anthropic", version: "v2.0.4",
    temperature: 0.0, maxTokens: 512, timeout: "20s", retries: 1, endpoint: "workflows.extract-entities",
  },
  "rewrite-message": {
    description: "Rewrites a user-supplied draft to match a target tone and audience, enforcing word-count and style-guide constraints before returning.",
    model: "claude-haiku-4-5", provider: "anthropic", version: "v2.4.1",
    temperature: 0.3, maxTokens: 512, timeout: "30s", retries: 2, endpoint: "workflows.rewrite-message",
  },
  "detect-sentiment": {
    description: "Scores a single message on a continuous -1 to +1 sentiment axis with an emotion label. Lowest-latency workflow in the workspace.",
    model: "claude-haiku-4-5", provider: "anthropic", version: "v3.0.1",
    temperature: 0.0, maxTokens: 64, timeout: "10s", retries: 1, endpoint: "workflows.detect-sentiment",
  },
  "summarize-thread": {
    description: "Produces a narrative summary of an entire conversation thread, preserving decisions and action items in order.",
    model: "claude-sonnet-4-5", provider: "anthropic", version: "v1.3.0",
    temperature: 0.3, maxTokens: 1536, timeout: "60s", retries: 1, endpoint: "workflows.summarize-thread",
  },
  "moderate-content": {
    description: "Flags policy-violating content across 9 categories and returns a block/allow/review decision. Highest-volume workflow.",
    model: "claude-haiku-4-5", provider: "anthropic", version: "v5.1.2",
    temperature: 0.0, maxTokens: 96, timeout: "10s", retries: 2, endpoint: "workflows.moderate-content",
  },
};

export const DEFAULT_WORKFLOW_META = WORKFLOW_META["rewrite-message"];
