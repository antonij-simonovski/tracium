import { useState, useMemo } from 'react';
import { USERS } from './UsersPage';

export interface UserDetailPageProps {
  selected: Record<string, string>;
  setView: (v: string) => void;
}

interface WorkflowRow {
  workflow: string;
  calls: number;
  completed: number;
  failed: number;
  success: number;
  cost: number;
  avg: number;
  p95: number;
  growth: number;
}

const TD_WORKFLOW_ROWS: WorkflowRow[] = [
  { workflow: "summarize-comments",             calls: 18204, completed: 18187, failed:  17, success: 99.91, cost: 4.0214, avg: 0.000221, p95: 2.4, growth: 0.18 },
  { workflow: "classify-intent",                calls: 21882, completed: 21873, failed:   9, success: 99.96, cost: 2.4124, avg: 0.000110, p95: 0.9, growth: 0.22 },
  { workflow: "moderate-content",               calls: 10882, completed: 10869, failed:  13, success: 99.88, cost: 1.4982, avg: 0.000138, p95: 0.5, growth: 0.04 },
  { workflow: "rewrite-message",                calls:  3104, completed:  2998, failed: 106, success: 96.58, cost: 2.4204, avg: 0.000780, p95: 4.8, growth: 0.42 },
  { workflow: "generate-clock-out-description", calls:  2840, completed:  2839, failed:   1, success: 99.96, cost: 1.4082, avg: 0.000496, p95: 3.4, growth: 0.11 },
  { workflow: "detect-sentiment",               calls:  1110, completed:  1107, failed:   3, success: 99.73, cost: 0.6841, avg: 0.000616, p95: 1.1, growth:-0.04 },
];

type TraceStatus = 'completed' | 'failed' | 'running';

interface RecentTrace {
  id: string;
  workflow: string;
  status: TraceStatus;
  started: string;
  cost: number;
  latency: number;
  msg?: string;
}

const TD_RECENT_TRACES: RecentTrace[] = [
  { id: "t_eb7c92a1", workflow: "rewrite-message",     status: "failed",    started: "Apr 18 · 09:41:17", cost: 0.0012, latency: 4823, msg: "rate_limit_exceeded" },
  { id: "t_d8f4b2c9", workflow: "summarize-comments",  status: "completed", started: "Apr 18 · 09:41:14", cost: 0.0003, latency: 2114 },
  { id: "t_d8f4b2c8", workflow: "classify-intent",     status: "completed", started: "Apr 18 · 09:41:12", cost: 0.0001, latency:  812 },
  { id: "t_d8f4b2c7", workflow: "rewrite-message",     status: "failed",    started: "Apr 18 · 09:41:09", cost: 0.0009, latency: 6210, msg: "context_length" },
  { id: "t_d8f4b2c6", workflow: "moderate-content",    status: "completed", started: "Apr 18 · 09:41:07", cost: 0.0000, latency:  342 },
  { id: "t_d8f4b2c5", workflow: "summarize-comments",  status: "completed", started: "Apr 18 · 09:41:04", cost: 0.0005, latency: 1922 },
  { id: "t_d8f4b2c4", workflow: "detect-sentiment",    status: "completed", started: "Apr 18 · 09:41:02", cost: 0.0001, latency:  604 },
  { id: "t_d8f4b2c3", workflow: "classify-intent",     status: "running",   started: "Apr 18 · 09:41:00", cost: 0.0000, latency:    0 },
  { id: "t_d8f4b2c2", workflow: "rewrite-message",     status: "completed", started: "Apr 18 · 09:40:57", cost: 0.0008, latency: 3941 },
  { id: "t_d8f4b2c1", workflow: "summarize-comments",  status: "completed", started: "Apr 18 · 09:40:55", cost: 0.0004, latency: 2204 },
  { id: "t_d8f4b2c0", workflow: "moderate-content",    status: "completed", started: "Apr 18 · 09:40:52", cost: 0.0000, latency:  412 },
  { id: "t_d8f4b2bf", workflow: "classify-intent",     status: "completed", started: "Apr 18 · 09:40:50", cost: 0.0001, latency:  741 },
];

interface CostSeriesPoint { day: number; cost: number; runs: number }

function makeCostSeries(seed: number, weeklyTrend: number[]): CostSeriesPoint[] {
  return Array.from({ length: 30 }, (_, i) => {
    const phase = Math.sin((i + seed) / 4.2);
    const w = weeklyTrend[Math.min(weeklyTrend.length - 1, Math.floor(i * weeklyTrend.length / 30))];
    return {
      day: i + 1,
      cost: Math.max(0.02, w * 1.2 + phase * 0.06 + (i === 13 + (seed % 4) ? 0.45 : 0)),
      runs: Math.round((w * 1900) + phase * 320 + (i === 13 + (seed % 4) ? 4200 : 0)),
    };
  });
}

interface TdSectionHeadProps {
  title: string;
  hint?: string;
  first?: boolean;
}

function TdSectionHead({ title, hint, first = false }: TdSectionHeadProps) {
  return (
    <header style={{
      display: "flex", alignItems: "flex-end", justifyContent: "space-between",
      gap: 24, flexWrap: "wrap",
      paddingTop: first ? 0 : 48,
      paddingBottom: 18,
      borderTop: first ? "none" : "1px solid color-mix(in srgb, var(--border) 50%, transparent)",
    }}>
      <div style={{ display: "flex", flexDirection: "column", gap: 5, minWidth: 0, paddingTop: first ? 0 : 28 }}>
        <h2 style={{ fontSize: 19, fontWeight: 600, letterSpacing: "-0.018em", margin: 0, color: "var(--foreground)" }}>{title}</h2>
        {hint && <p style={{ fontSize: 14, color: "var(--muted)", margin: 0, maxWidth: 620, lineHeight: 1.55 }}>{hint}</p>}
      </div>
    </header>
  );
}

interface User {
  id: string;
  name: string;
  region?: string;
  cost: number;
  runs: number;
  avg: number;
  trend: number[];
}

interface TdHeaderProps {
  user: User;
  setView: (v: string) => void;
}

function TdHeader({ user, setView }: TdHeaderProps) {
  const initials = user.name.split(/\s+/).map((w: string) => w[0]).slice(0, 2).join("");

  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--muted)", marginBottom: 16 }}>
        <button
          onClick={() => setView("users")}
          style={{
            padding: 0, background: "transparent", border: "none",
            color: "var(--muted)", fontSize: 13, fontFamily: "inherit",
            cursor: "pointer", fontWeight: 500,
          }}
          onMouseEnter={e => { (e.currentTarget as HTMLButtonElement).style.color = "var(--foreground)"; }}
          onMouseLeave={e => { (e.currentTarget as HTMLButtonElement).style.color = "var(--muted)"; }}
        >
          ← All clients
        </button>
      </div>

      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 24, flexWrap: "wrap" }}>
        <div style={{ display: "flex", alignItems: "flex-start", gap: 16, minWidth: 0 }}>
          <div style={{
            width: 52, height: 52, borderRadius: 11,
            background: "linear-gradient(135deg, var(--surface-alt), color-mix(in srgb, var(--accent) 14%, var(--surface-alt)))",
            border: "1px solid var(--border)",
            display: "grid", placeItems: "center",
            color: "var(--foreground)", fontSize: 18, fontWeight: 600,
            letterSpacing: "-0.01em", flexShrink: 0,
          }}>{initials}</div>

          <div style={{ display: "flex", flexDirection: "column", gap: 6, minWidth: 0 }}>
            <h1 style={{ fontSize: 26, fontWeight: 600, color: "var(--foreground)", margin: 0, letterSpacing: "-0.02em", lineHeight: 1.15 }}>{user.name}</h1>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <span style={{
                fontFamily: "var(--font-mono)", fontSize: 13, color: "var(--muted)",
                padding: "2px 7px", background: "var(--surface-alt)",
                border: "1px solid var(--border)", borderRadius: 5,
              }}>{user.id}</span>
              <span style={{ fontSize: 13, color: "var(--muted)", fontFamily: "var(--font-mono)" }}>{user.region ?? "us-west-2"}</span>
              <span style={{ fontSize: 13, color: "var(--muted)" }}>·</span>
              <span style={{ fontSize: 13, color: "var(--muted)" }}>
                <span style={{ display: "inline-block", width: 6, height: 6, borderRadius: 999, background: "var(--accent)", marginRight: 6, verticalAlign: "1px" }}/>
                Active · last seen <span style={{ color: "var(--foreground)" }}>2 minutes ago</span>
              </span>
            </div>
          </div>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 8, flexShrink: 0 }}>
          <button style={{
            padding: "7px 12px", background: "var(--accent)",
            border: "1px solid var(--accent)",
            borderRadius: 7, color: "var(--accent-contrast)",
            fontSize: 14, fontWeight: 600, fontFamily: "inherit", cursor: "pointer",
          }}>Configure</button>
        </div>
      </div>
    </div>
  );
}

interface TdKpiStripProps { user: User }

function TdKpiStrip({ user }: TdKpiStripProps) {
  const cells = [
    { label: "Total runs",  value: user.runs.toLocaleString(),                       delta: "+18%",  good: true,  sub: "vs prior 30d" },
    { label: "Completed",   value: Math.round(user.runs * 0.9962).toLocaleString(),  delta: "99.62%", good: true,  sub: "success rate" },
    { label: "Failed",      value: Math.round(user.runs * 0.0038).toLocaleString(),  delta: "0.38%",  good: false, sub: "of total" },
    { label: "Total cost",  value: "$" + user.cost.toFixed(2),                       delta: "+12%",  good: true,  sub: "vs prior 30d" },
    { label: "Avg / run",   value: "$" + user.avg.toFixed(6),                        delta: "−4%",   good: true,  sub: "trending down" },
    { label: "P95 latency", value: "2.4s",                                              delta: "+0.3s", good: false, sub: "vs prior 30d" },
  ];
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", padding: "28px 0 8px" }}>
      {cells.map((c, i) => (
        <div key={c.label} style={{
          paddingRight: 20,
          paddingLeft: i === 0 ? 0 : 20,
          borderRight: i < cells.length - 1 ? "1px solid color-mix(in srgb, var(--border) 45%, transparent)" : "none",
        }}>
          <div style={{ fontSize: 12.5, color: "var(--muted)", marginBottom: 10, fontWeight: 500, textTransform: "uppercase", letterSpacing: "0.04em" }}>{c.label}</div>
          <div style={{ fontSize: 26, fontWeight: 500, letterSpacing: "-0.02em", color: "var(--foreground)", fontVariantNumeric: "tabular-nums", lineHeight: 1, marginBottom: 8 }}>{c.value}</div>
          <div style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ color: c.good ? "var(--accent)" : "var(--warning)", fontWeight: 500, fontVariantNumeric: "tabular-nums" }}>{c.delta}</span>
            <span style={{ color: "var(--muted)" }}>{c.sub}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

interface TdCostChartProps { series: CostSeriesPoint[]; height?: number }

function TdCostChart({ series, height = 200 }: TdCostChartProps) {
  const maxVal = Math.max(...series.map(d => d.cost)) * 1.15;
  const w = 1000;
  const h = height;
  const xStep = w / (series.length - 1);
  const points = series.map((d, i) => [i * xStep, h - (d.cost / maxVal) * h] as [number, number]);
  const linePath = points.map((p, i) => (i === 0 ? "M" : "L") + p[0].toFixed(1) + "," + p[1].toFixed(1)).join(" ");
  const areaPath = linePath + ` L ${w},${h} L 0,${h} Z`;
  const gridYs = [0.25, 0.5, 0.75].map(p => h - p * h);

  return (
    <div style={{ position: "relative" }}>
      <div style={{
        position: "absolute", left: 0, top: 0, bottom: 24,
        width: 56, display: "flex", flexDirection: "column", justifyContent: "space-between",
        fontSize: 11.5, color: "var(--muted)", fontVariantNumeric: "tabular-nums", textAlign: "right", paddingRight: 8,
      }}>
        <span>${maxVal.toFixed(2)}</span>
        <span>${(maxVal * 0.5).toFixed(2)}</span>
        <span>$0.00</span>
      </div>

      <div style={{ marginLeft: 56 }}>
        <svg viewBox={`0 0 ${w} ${h + 22}`} preserveAspectRatio="none" style={{ width: "100%", height: height + 22, display: "block" }}>
          <defs>
            <linearGradient id="td-area-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%"   stopColor="var(--accent)" stopOpacity="0.22"/>
              <stop offset="100%" stopColor="var(--accent)" stopOpacity="0"/>
            </linearGradient>
          </defs>
          {gridYs.map((y, i) => (
            <line key={i} x1="0" y1={y} x2={w} y2={y}
              stroke="color-mix(in srgb, var(--border) 50%, transparent)" strokeWidth="1" strokeDasharray="2 4"/>
          ))}
          <path d={areaPath} fill="url(#td-area-grad)"/>
          <path d={linePath} fill="none" stroke="var(--accent)" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round"/>
          {series.map((d, i) => d.cost > maxVal * 0.7 ? (
            <circle key={i} cx={i * xStep} cy={h - (d.cost / maxVal) * h} r="3"
              fill="var(--surface)" stroke="var(--warning)" strokeWidth="1.5"/>
          ) : null)}
          {([0, 7, 14, 21, 29] as const).map(i => (
            <text key={i} x={i * xStep} y={h + 16}
              fontSize="11.5" fill="var(--muted)"
              textAnchor={i === 0 ? "start" : i === 29 ? "end" : "middle"}
              fontFamily="var(--font-mono)">{series[i] ? "Day " + series[i].day : ""}</text>
          ))}
        </svg>
      </div>
    </div>
  );
}

type WorkflowSortKey = keyof Pick<WorkflowRow, 'workflow' | 'calls' | 'success' | 'failed' | 'p95' | 'cost' | 'avg' | 'growth'>;

interface WorkflowColDef {
  key: WorkflowSortKey;
  label: string;
  w: string;
  align: 'left' | 'right';
  mono?: boolean;
  muted?: boolean;
  format: (a: WorkflowRow) => string;
  color?: (a: WorkflowRow) => string;
}

const WORKFLOW_COLS: WorkflowColDef[] = [
  { key: "workflow",   label: "Workflow",      w: "minmax(220px, 1.6fr)", align: "left",  format: a => a.workflow },
  { key: "calls",   label: "Calls",      w: "100px", align: "right", mono: true,  format: a => a.calls.toLocaleString() },
  { key: "success", label: "Success",    w: "90px",  align: "right", mono: true,  format: a => a.success.toFixed(2) + "%",
    color: a => a.success >= 99 ? "var(--accent)" : a.success >= 95 ? "var(--warning)" : "var(--error)" },
  { key: "failed",  label: "Failed",     w: "80px",  align: "right", mono: true,  format: a => a.failed.toLocaleString() },
  { key: "p95",     label: "P95",        w: "70px",  align: "right", mono: true,  format: a => a.p95.toFixed(1) + "s" },
  { key: "cost",    label: "Cost",       w: "100px", align: "right", mono: true,  format: a => "$" + a.cost.toFixed(4) },
  { key: "avg",     label: "Avg / call", w: "110px", align: "right", mono: true, muted: true, format: a => "$" + a.avg.toFixed(6) },
  { key: "growth",  label: "Growth",     w: "80px",  align: "right", mono: true,
    format: a => (a.growth >= 0 ? "+" : "") + (a.growth * 100).toFixed(0) + "%",
    color: a => a.growth >= 0 ? "var(--accent)" : "var(--warning)" },
];

function TdWorkflowTable() {
  const [sortKey, setSortKey] = useState<WorkflowSortKey>("cost");
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>("desc");

  const sorted = useMemo(() => {
    const sign = sortDir === "asc" ? 1 : -1;
    return [...TD_WORKFLOW_ROWS].sort((a, b) => {
      const av = a[sortKey];
      const bv = b[sortKey];
      if (typeof av === "string" && typeof bv === "string") return av.localeCompare(bv) * sign;
      if (typeof av === "number" && typeof bv === "number") return (av - bv) * sign;
      return 0;
    });
  }, [sortKey, sortDir]);

  const onSort = (k: WorkflowSortKey) => {
    if (sortKey === k) setSortDir(sortDir === "asc" ? "desc" : "asc");
    else { setSortKey(k); setSortDir("desc"); }
  };

  const grid = WORKFLOW_COLS.map(c => c.w).join(" ");

  return (
    <div style={{ overflowX: "auto" }}>
      <div style={{
        display: "grid", gridTemplateColumns: grid, minWidth: 640, gap: 14,
        padding: "12px 0",
        borderBottom: "1px solid color-mix(in srgb, var(--border) 70%, transparent)",
      }}>
        {WORKFLOW_COLS.map(c => (
          <button key={c.key} onClick={() => onSort(c.key)} style={{
            padding: 0, background: "transparent", border: "none",
            color: sortKey === c.key ? "var(--foreground)" : "var(--muted)",
            fontSize: 12, fontWeight: 500, textTransform: "uppercase", letterSpacing: "0.06em",
            fontFamily: "inherit", cursor: "pointer", textAlign: c.align,
            display: "inline-flex", alignItems: "center", gap: 4,
            justifyContent: c.align === "right" ? "flex-end" : "flex-start",
          }}>
            {c.label}
            {sortKey === c.key && <span style={{ fontSize: 9, opacity: 0.7 }}>{sortDir === "asc" ? "▲" : "▼"}</span>}
          </button>
        ))}
      </div>

      {sorted.map((row, i) => (
        <div key={row.workflow}
          style={{
            display: "grid", gridTemplateColumns: grid, minWidth: 640, gap: 14,
            padding: "14px 0",
            borderBottom: i < sorted.length - 1 ? "1px solid color-mix(in srgb, var(--border) 50%, transparent)" : "none",
            alignItems: "center",
            cursor: "pointer",
          }}
          onMouseEnter={e => { (e.currentTarget as HTMLDivElement).style.background = "color-mix(in srgb, var(--foreground) 2%, transparent)"; }}
          onMouseLeave={e => { (e.currentTarget as HTMLDivElement).style.background = "transparent"; }}
        >
          {WORKFLOW_COLS.map(c => {
            const val = c.format(row);
            const color = c.color ? c.color(row) : c.muted ? "var(--muted)" : "var(--foreground)";
            return (
              <div key={c.key} style={{
                fontSize: 14, color,
                textAlign: c.align,
                fontFamily: c.mono ? "var(--font-mono)" : "inherit",
                fontVariantNumeric: "tabular-nums",
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
                fontWeight: c.key === "workflow" ? 500 : 400,
              }}>{val}</div>
            );
          })}
        </div>
      ))}
    </div>
  );
}

type TraceFilterKey = 'all' | TraceStatus;

interface TdTracesTableProps {
  setView: (v: string) => void;
}

function statusColor(s: TraceStatus): string {
  if (s === "completed") return "var(--accent)";
  if (s === "failed")    return "var(--error)";
  if (s === "running")   return "#7aa5ff";
  return "var(--muted)";
}

function TdTracesTable({ setView }: TdTracesTableProps) {
  const [filter, setFilter] = useState<TraceFilterKey>("all");
  const filtered = filter === "all" ? TD_RECENT_TRACES : TD_RECENT_TRACES.filter(t => t.status === filter);
  const cols = "minmax(170px, 1.4fr) 110px minmax(140px, 1fr) 90px 90px";

  const filterOptions: { id: TraceFilterKey; label: string; n: number }[] = [
    { id: "all",       label: "All",       n: TD_RECENT_TRACES.length },
    { id: "completed", label: "Completed", n: TD_RECENT_TRACES.filter(t => t.status === "completed").length },
    { id: "failed",    label: "Failed",    n: TD_RECENT_TRACES.filter(t => t.status === "failed").length },
    { id: "running",   label: "Running",   n: TD_RECENT_TRACES.filter(t => t.status === "running").length },
  ];

  return (
    <div style={{ overflowX: "auto" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", paddingBottom: 14 }}>
        <div style={{ display: "flex", gap: 6 }}>
          {filterOptions.map(o => {
            const active = filter === o.id;
            return (
              <button key={o.id} onClick={() => setFilter(o.id)} style={{
                padding: "5px 11px",
                background: active ? "var(--surface)" : "transparent",
                border: "1px solid " + (active ? "var(--border)" : "transparent"),
                borderRadius: 999, color: active ? "var(--foreground)" : "var(--muted)",
                fontSize: 13, fontWeight: 500, fontFamily: "inherit", cursor: "pointer",
                display: "inline-flex", alignItems: "center", gap: 6,
              }}>
                {o.label}
                <span style={{
                  fontSize: 11.5, color: "var(--muted)", fontVariantNumeric: "tabular-nums",
                  padding: "0 5px", background: active ? "var(--surface-alt)" : "transparent",
                  borderRadius: 4,
                }}>{o.n}</span>
              </button>
            );
          })}
        </div>
        <button onClick={() => setView("workflows")} style={{
          padding: "5px 11px", background: "transparent",
          border: "1px solid var(--border-strong)",
          borderRadius: 7, color: "var(--muted)",
          fontSize: 13, fontWeight: 500, fontFamily: "inherit", cursor: "pointer",
          display: "inline-flex", alignItems: "center", gap: 6,
        }}>View all traces →</button>
      </div>

      <div style={{
        display: "grid", gridTemplateColumns: cols, minWidth: 640, gap: 14,
        padding: "12px 0",
        borderBottom: "1px solid color-mix(in srgb, var(--border) 70%, transparent)",
        fontSize: 12, color: "var(--muted)", textTransform: "uppercase",
        letterSpacing: "0.06em", fontWeight: 500,
      }}>
        <div>Trace</div>
        <div>Status</div>
        <div>Started</div>
        <div style={{ textAlign: "right" }}>Cost</div>
        <div style={{ textAlign: "right" }}>Latency</div>
      </div>

      {filtered.length === 0 ? (
        <div style={{ padding: "40px 20px", textAlign: "center", color: "var(--muted)", fontSize: 14 }}>No traces for this filter.</div>
      ) : filtered.map((t, i) => (
        <div key={t.id}
          onClick={() => setView("trace")}
          style={{
            display: "grid", gridTemplateColumns: cols, minWidth: 640, gap: 14,
            padding: "13px 0",
            borderBottom: i < filtered.length - 1 ? "1px solid color-mix(in srgb, var(--border) 50%, transparent)" : "none",
            alignItems: "center", cursor: "pointer",
          }}
          onMouseEnter={e => { (e.currentTarget as HTMLDivElement).style.background = "color-mix(in srgb, var(--foreground) 2%, transparent)"; }}
          onMouseLeave={e => { (e.currentTarget as HTMLDivElement).style.background = "transparent"; }}
        >
          <div style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
            <span style={{ fontSize: 14, fontWeight: 500, color: "var(--foreground)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{t.workflow}</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--muted)" }}>{t.id}{t.msg ? " · " + t.msg : ""}</span>
          </div>
          <div>
            <span style={{
              display: "inline-flex", alignItems: "center", gap: 5,
              padding: "2px 8px 2px 6px", borderRadius: 999,
              fontSize: 12.5, fontWeight: 500,
              color: statusColor(t.status),
              background: `color-mix(in srgb, ${statusColor(t.status)} 10%, transparent)`,
              border: `1px solid color-mix(in srgb, ${statusColor(t.status)} 22%, transparent)`,
              textTransform: "capitalize",
            }}>
              <span style={{ width: 5, height: 5, borderRadius: 999, background: "currentColor" }}/>
              {t.status}
            </span>
          </div>
          <div style={{ fontSize: 13.5, color: "var(--muted)", fontFamily: "var(--font-mono)", fontVariantNumeric: "tabular-nums" }}>{t.started}</div>
          <div style={{ fontSize: 14, color: "var(--foreground)", fontFamily: "var(--font-mono)", fontVariantNumeric: "tabular-nums", textAlign: "right" }}>${t.cost.toFixed(4)}</div>
          <div style={{ fontSize: 14, color: "var(--muted)", fontFamily: "var(--font-mono)", fontVariantNumeric: "tabular-nums", textAlign: "right" }}>
            {t.latency === 0 ? "—" : t.latency < 1000 ? t.latency + "ms" : (t.latency / 1000).toFixed(1) + "s"}
          </div>
        </div>
      ))}
    </div>
  );
}

interface TdConnectionProps { user: User }

function TdConnection({ user }: TdConnectionProps) {
  const fields = [
    { label: "External ID",  value: user.id,       mono: true },
    { label: "SDK version",  value: "py-sdk@2.4.1",  mono: true },
    { label: "First seen",   value: "Jan 14, 2025",  mono: false },
    { label: "Onboarded by", value: "Priya Sharma",  mono: false },
  ];
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: 0, padding: "20px 0" }}>
      {fields.map((f, i) => (
        <div key={f.label} style={{
          paddingRight: 20,
          paddingLeft: i === 0 ? 0 : 20,
          borderRight: i < fields.length - 1 ? "1px solid color-mix(in srgb, var(--border) 45%, transparent)" : "none",
        }}>
          <div style={{ fontSize: 12.5, color: "var(--muted)", marginBottom: 6, fontWeight: 500, textTransform: "uppercase", letterSpacing: "0.04em" }}>{f.label}</div>
          <div style={{ fontSize: 14, color: "var(--foreground)", fontFamily: f.mono ? "var(--font-mono)" : "inherit", fontVariantNumeric: "tabular-nums" }}>{f.value}</div>
        </div>
      ))}
    </div>
  );
}

export function UserDetailPage({ selected, setView }: UserDetailPageProps) {
  const userId = selected.user ?? "";

  const userFull = useMemo(() => {
    return USERS.find(t => t.id === userId) ?? USERS[0];
  }, [userId]);

  const user: User = userFull;

  const series = useMemo(() => makeCostSeries(user.name.length, user.trend), [user]);

  return (
    <div style={{ padding: "clamp(20px, 4vw, 32px) clamp(16px, 4vw, 28px) 96px", maxWidth: 1320, margin: "0 auto" }}>
      <TdHeader user={user} setView={setView}/>

      <TdKpiStrip user={user}/>

      <TdSectionHead
        title="Cost & runs · last 30 days"
        hint="Hover the line for daily totals. Markers flag days exceeding the 14-day rolling baseline."
      />
      <div style={{ padding: "8px 0 4px" }}>
        <TdCostChart series={series}/>
      </div>

      <TdSectionHead
        title="Usage by workflow"
        hint="Per-workflow activity within this client. Click a row for the workflow's full timeline."
      />
      <TdWorkflowTable/>

      <TdSectionHead
        title="Recent traces"
        hint="Live tail of this client's traffic. Click a row to inspect spans."
      />
      <TdTracesTable setView={setView}/>

      <TdSectionHead title="Connection" hint="How this client identifies itself to Tracium."/>
      <TdConnection user={user}/>
    </div>
  );
}
