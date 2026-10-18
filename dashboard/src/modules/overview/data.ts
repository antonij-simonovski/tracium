import type { ActivityItem } from './interfaces';
import type { TraceId } from '../../common/ids';
import type { CostPoint, LatencyPoint, ErrorPoint } from '../../common/interfaces';

export const ACTIVITY_FEED: ActivityItem[] = [
  { id: "t_a9f2" as TraceId, workflow: "rewrite-message",                status: "failed",    time: "just now", latency: 4.8, msg: "rate_limit_exceeded" },
  { id: "t_a9f1" as TraceId, workflow: "summarize-comments",             status: "completed", time: "2s ago",   latency: 2.1 },
  { id: "t_a9f0" as TraceId, workflow: "classify-intent",                status: "completed", time: "4s ago",   latency: 0.8 },
  { id: "t_a9ef" as TraceId, workflow: "rewrite-message",                status: "failed",    time: "7s ago",   latency: 6.2, msg: "context_length" },
  { id: "t_a9ee" as TraceId, workflow: "moderate-content",               status: "completed", time: "9s ago",   latency: 0.3 },
  { id: "t_a9ed" as TraceId, workflow: "summarize-comments",             status: "completed", time: "12s ago",  latency: 1.9 },
  { id: "t_a9ec" as TraceId, workflow: "detect-sentiment",               status: "completed", time: "14s ago",  latency: 0.6 },
  { id: "t_a9eb" as TraceId, workflow: "classify-intent",                status: "completed", time: "16s ago",  latency: 0.7 },
  { id: "t_a9ea" as TraceId, workflow: "extract-entities",               status: "completed", time: "19s ago",  latency: 1.1 },
  { id: "t_a9e9" as TraceId, workflow: "summarize-comments",             status: "completed", time: "21s ago",  latency: 2.2 },
  { id: "t_a9e8" as TraceId, workflow: "moderate-content",               status: "completed", time: "24s ago",  latency: 0.4 },
  { id: "t_a9e7" as TraceId, workflow: "rewrite-message",                status: "completed", time: "27s ago",  latency: 3.9 },
  { id: "t_a9e6" as TraceId, workflow: "classify-intent",                status: "completed", time: "29s ago",  latency: 0.9 },
  { id: "t_a9e5" as TraceId, workflow: "generate-clock-out-description", status: "completed", time: "32s ago",  latency: 3.1 },
  { id: "t_a9e4" as TraceId, workflow: "detect-sentiment",               status: "completed", time: "34s ago",  latency: 0.5 },
];

export const COST_SERIES_7D: CostPoint[] = [
  { label: "Mon 4/13", value: 0.1804 },
  { label: "Tue 4/14", value: 1.7402 },
  { label: "Wed 4/15", value: 0.1602 },
  { label: "Thu 4/16", value: 0.1324 },
  { label: "Fri 4/17", value: 0.1489 },
  { label: "Sat 4/18", value: 0.0968 },
  { label: "Sun 4/19", value: 0.1214 },
];

export const COST_SERIES_24H: CostPoint[] = Array.from({ length: 24 }, (_, i) => {
  const base = 0.02 + Math.sin(i / 3) * 0.008 + (i === 11 ? 0.09 : 0);
  return { label: `${String(i).padStart(2, '0')}:00`, value: Math.max(0.002, base) };
});

export const COST_SERIES_30D: CostPoint[] = Array.from({ length: 30 }, (_, i) => {
  const v = 0.12 + Math.sin(i / 4) * 0.04 + (i === 7 ? 1.5 : 0) + (i === 22 ? 0.8 : 0);
  return { label: `${i + 1}`, value: Math.max(0.04, v) };
});

export const LATENCY_SERIES_7D: LatencyPoint[] = [
  { label: "Mon", p50: 1.2, p95: 3.1, p99: 6.0  },
  { label: "Tue", p50: 1.8, p95: 4.2, p99: 31.5 },
  { label: "Wed", p50: 1.4, p95: 3.8, p99: 12.0 },
  { label: "Thu", p50: 1.1, p95: 2.9, p99: 7.2  },
  { label: "Fri", p50: 1.3, p95: 3.2, p99: 6.8  },
  { label: "Sat", p50: 0.9, p95: 2.4, p99: 5.1  },
  { label: "Sun", p50: 1.0, p95: 2.6, p99: 5.4  },
];

export const LATENCY_SERIES_24H: LatencyPoint[] = Array.from({ length: 24 }, (_, i) => ({
  label: `${String(i).padStart(2, '0')}:00`,
  p50: 1.0 + Math.sin(i / 4) * 0.3,
  p95: 2.8 + Math.sin(i / 4) * 0.8 + (i === 11 ? 2.2 : 0),
  p99: 5.5 + Math.sin(i / 4) * 1.5 + (i === 11 ? 18 : 0),
}));

export const LATENCY_SERIES_30D: LatencyPoint[] = Array.from({ length: 30 }, (_, i) => ({
  label: `${i + 1}`,
  p50: 1.1 + Math.sin(i / 5) * 0.3,
  p95: 3.0 + Math.sin(i / 5) * 0.6 + (i === 7 ? 1.4 : 0),
  p99: 6.0 + Math.sin(i / 5) * 1.2 + (i === 7 ? 25 : 0) + (i === 22 ? 14 : 0),
}));

export const ERROR_SERIES_7D: ErrorPoint[] = [
  { label: "Mon", errors: 2,  total: 312 },
  { label: "Tue", errors: 1,  total: 418 },
  { label: "Wed", errors: 3,  total: 287 },
  { label: "Thu", errors: 0,  total: 245 },
  { label: "Fri", errors: 8,  total: 298 },
  { label: "Sat", errors: 12, total: 201 },
  { label: "Sun", errors: 14, total: 189 },
];
