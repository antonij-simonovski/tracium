import { SpanStatusCode, trace } from "@opentelemetry/api";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { NodeSDK } from "@opentelemetry/sdk-node";

const apiKey = process.env.TRACIUM_API_KEY;
if (!apiKey) {
  console.error("Set TRACIUM_API_KEY to an ingest key from the dashboard's API keys screen.");
  process.exit(1);
}

const endpoint = process.env.TRACIUM_ENDPOINT ?? "http://localhost:4318";

const sdk = new NodeSDK({
  resource: resourceFromAttributes({
    "service.name": "node-example",
    "tracium.user.id": process.env.TRACIUM_USER_ID ?? "acme-corp",
    "tracium.workflow.version": process.env.TRACIUM_WORKFLOW_VERSION ?? "v1",
  }),
  traceExporter: new OTLPTraceExporter({
    url: `${endpoint}/v1/traces`,
    headers: { Authorization: `Bearer ${apiKey}` },
  }),
});
sdk.start();

const tracer = trace.getTracer("node-example");

function chat(model, prompt, usage) {
  tracer.startActiveSpan(`chat ${model}`, (span) => {
    span.setAttributes({
      "traceloop.workflow.name": "summarize-feedback",
      "gen_ai.operation.name": "chat",
      "gen_ai.request.model": model,
      "gen_ai.response.model": model,
      "gen_ai.usage.input_tokens": usage.input,
      "gen_ai.usage.output_tokens": usage.output,
      "gen_ai.input.messages": JSON.stringify([{ role: "user", parts: [{ type: "text", content: prompt }] }]),
    });
    span.setStatus({ code: SpanStatusCode.OK });
    span.end();
  });
}

tracer.startActiveSpan("summarize-feedback", (workflow) => {
  workflow.setAttribute("traceloop.workflow.name", "summarize-feedback");
  chat("gpt-4o-mini", "Group these 40 survey answers by theme.", { input: 1840, output: 312 });
  chat("claude-sonnet-4-5", "Write a one-paragraph summary of the themes.", { input: 420, output: 160 });
  workflow.end();
});

await sdk.shutdown();
console.log(`Sent one trace to ${endpoint}. Open the dashboard to see it.`);
