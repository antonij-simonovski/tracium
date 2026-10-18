import React, { useMemo, useState, type ReactNode } from 'react';
import { KpiStrip, LastUpdated, Sparkline, deltaParts, fmtCost, fmtNum, relativeTime } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { Kpi } from '../../overview/interfaces';
import { UNATTRIBUTED, rowButtonProps } from '../utils';

export interface User {
  id: string;
  name: string;
  cost: number;
  runs: number;
  avg: number;
  trend: number[];
  // Non-telemetry metadata — present in the demo dataset only.
  region?: string;
  success?: number;
  lastSeen?: number;
}

const ago = (seconds: number) => Date.now() - seconds * 1000;

export const USERS: User[] = [
  { id: "tn_acme",      name: "Acme Robotics",       cost: 12.4421, runs: 58_022, avg: 0.000214, trend:[0.36,0.42,0.41,0.48,0.51,0.55,0.58,0.61,0.59,0.62,0.66,0.71], region: "us-east-1", success: 99.4, lastSeen: ago(12) },
  { id: "tn_northwind", name: "Northwind Logistics", cost:  8.9024, runs: 41_318, avg: 0.000216, trend:[0.18,0.22,0.27,0.31,0.34,0.39,0.41,0.44,0.45,0.49,0.52,0.55], region: "us-east-1", success: 98.1, lastSeen: ago(60) },
  { id: "tn_helix",     name: "Helix Health",        cost:  6.1102, runs: 28_104, avg: 0.000217, trend:[0.32,0.30,0.28,0.27,0.26,0.25,0.24,0.23,0.22,0.21,0.21,0.22], region: "eu-west-2", success: 99.6, lastSeen: ago(180) },
  { id: "tn_lumen",     name: "Lumen Studio",        cost:  4.4081, runs: 21_890, avg: 0.000201, trend:[0.14,0.15,0.16,0.16,0.18,0.19,0.18,0.18,0.19,0.20,0.20,0.20], region: "us-west-2", success: 99.2, lastSeen: ago(480) },
  { id: "tn_kestrel",   name: "Kestrel Finance",     cost:  3.2204, runs: 15_842, avg: 0.000203, trend:[0.10,0.12,0.13,0.14,0.14,0.15,0.16,0.16,0.17,0.18,0.18,0.19], region: "us-east-1", success: 99.0, lastSeen: ago(1320) },
  { id: "tn_polar",     name: "Polar Research",      cost:  1.8714, runs:  9_204, avg: 0.000203, trend:[0.07,0.08,0.08,0.08,0.09,0.08,0.09,0.08,0.08,0.09,0.09,0.09], region: "eu-west-2", success: 97.6, lastSeen: ago(3600) },
  { id: "tn_orbit",     name: "Orbit Labs",          cost:  0.9842, runs:  5_420, avg: 0.000182, trend:[0.13,0.12,0.11,0.10,0.09,0.08,0.07,0.06,0.06,0.05,0.05,0.04], region: "us-west-2", success: 94.2, lastSeen: ago(7200) },
  { id: "tn_sable",     name: "Sable Studios",       cost:  0.4830, runs:  4_423, avg: 0.000109, trend:[0.02,0.02,0.03,0.03,0.04,0.04,0.05,0.05,0.05,0.06,0.06,0.06], region: "us-east-1", success: 96.8, lastSeen: ago(300) },
];

function successColor(success: number): string {
  return success >= 99 ? "var(--foreground)" : success >= 97 ? "var(--warning)" : "var(--error)";
}

// Mirrors the API's KPI classification so these tiles read like the Usage ones.
function toKpi(value: number, prev: number, higherIsBad: boolean): Kpi {
  if (!prev || value === prev) return { value, delta: 0, delta_type: 'neutral' };
  return { value, delta: (value - prev) / prev, delta_type: value > prev !== higherIsBad ? 'good' : 'bad' };
}

const SHARE_PALETTE = ["var(--accent)", "#7aa5ff", "#c08aff", "#f5a524", "#5ec8b4", "#e76e8b", "#8b95a8", "color-mix(in srgb, var(--muted) 50%, transparent)"];

function ShareBar({ users, totalCost }: { users: User[]; totalCost: number }) {
  return (
    <div>
      <div style={{
        display: "flex", height: 28,
        borderRadius: 5, overflow: "hidden",
        border: "1px solid color-mix(in srgb, var(--border) 60%, transparent)",
        background: "var(--surface-alt)",
      }}>
        {users.map((t, i) => {
          const pct = totalCost > 0 ? (t.cost / totalCost) * 100 : 0;
          if (pct < 0.5) return null;
          return (
            <div key={t.id} title={`${t.name}: ${fmtCost(t.cost)} (${pct.toFixed(1)}%)`} style={{
              width: pct + "%",
              background: SHARE_PALETTE[i % SHARE_PALETTE.length],
              opacity: 0.85,
              borderRight: i < users.length - 1 ? "1px solid color-mix(in srgb, #000 30%, transparent)" : "none",
            }}/>
          );
        })}
      </div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: "10px 18px", marginTop: 14, fontSize: 12.5 }}>
        {users.slice(0, 6).map((t, i) => {
          const pct = totalCost > 0 ? (t.cost / totalCost) * 100 : 0;
          return (
            <span key={t.id} style={{ display: "inline-flex", alignItems: "center", gap: 6, color: "var(--muted)" }}>
              <span style={{ width: 8, height: 8, borderRadius: 2, background: SHARE_PALETTE[i % SHARE_PALETTE.length], opacity: 0.85 }}/>
              <span style={{ color: "var(--foreground)", fontWeight: 500 }}>{t.name}</span>
              <span style={{ fontVariantNumeric: "tabular-nums" }}>{pct.toFixed(1)}%</span>
            </span>
          );
        })}
        {users.length > 6 && (
          <span style={{ color: "var(--muted)", display: "inline-flex", alignItems: "center", gap: 6 }}>
            <span style={{ width: 8, height: 8, borderRadius: 2, background: "color-mix(in srgb, var(--muted) 50%, transparent)" }}/>
            +{users.length - 6} smaller
          </span>
        )}
      </div>
    </div>
  );
}

type UserSortKey = 'name' | 'region' | 'runs' | 'cost' | 'avg' | 'success' | 'lastSeen';

interface SortState { key: UserSortKey; dir: 'asc' | 'desc' }

interface ColDef {
  key: UserSortKey | 'trend';
  label: string;
  width: string;
  align: 'left' | 'right';
  sortKey?: UserSortKey;
  // Region, success and last seen aren't telemetry; only demo rows carry them.
  meta?: boolean;
  cell: (t: User) => ReactNode;
}

const COLUMNS: ColDef[] = [
  {
    key: "name", label: "Client", width: "minmax(180px, 1.4fr)", align: "left", sortKey: "name",
    cell: t => (
      <div style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
        <span style={{ fontSize: 14.5, fontWeight: 500, color: "var(--foreground)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{t.name}</span>
        <span style={{ fontSize: 12, fontFamily: "var(--font-mono)", color: "var(--muted)" }}>{t.id}</span>
      </div>
    ),
  },
  {
    key: "region", label: "Region", width: "84px", align: "left", sortKey: "region", meta: true,
    cell: t => <div style={{ fontSize: 13, color: "var(--muted)", fontFamily: "var(--font-mono)" }}>{t.region}</div>,
  },
  {
    key: "runs", label: "Runs", width: "80px", align: "right", sortKey: "runs",
    cell: t => <div style={{ textAlign: "right", fontVariantNumeric: "tabular-nums", fontSize: 14, color: "var(--foreground)" }}>{fmtNum(t.runs)}</div>,
  },
  {
    key: "trend", label: "Trend", width: "78px", align: "left",
    cell: t => <div><Sparkline data={t.trend} width={74} height={22} color="var(--accent)" fillOpacity={0.08} /></div>,
  },
  {
    key: "cost", label: "Cost", width: "90px", align: "right", sortKey: "cost",
    cell: t => <div style={{ textAlign: "right", fontVariantNumeric: "tabular-nums", fontSize: 14, color: "var(--foreground)", fontWeight: 500 }}>{fmtCost(t.cost)}</div>,
  },
  {
    key: "avg", label: "Avg / 1K", width: "96px", align: "right", sortKey: "avg",
    cell: t => <div style={{ textAlign: "right", fontVariantNumeric: "tabular-nums", fontSize: 13, color: "var(--muted)" }}>{fmtCost(t.avg * 1000)}</div>,
  },
  {
    key: "success", label: "Success", width: "76px", align: "right", sortKey: "success", meta: true,
    cell: t => <div style={{ textAlign: "right", fontVariantNumeric: "tabular-nums", fontSize: 13.5, color: successColor(t.success ?? 0) }}>{(t.success ?? 0).toFixed(1)}%</div>,
  },
  {
    key: "lastSeen", label: "Last seen", width: "84px", align: "right", sortKey: "lastSeen", meta: true,
    cell: t => <div style={{ textAlign: "right", fontSize: 12.5, color: "var(--muted)", fontVariantNumeric: "tabular-nums" }}>{t.lastSeen != null && relativeTime(t.lastSeen)}</div>,
  },
];

function SortableHeader({ label, sortKey, current, onSort, align }: {
  label: string; sortKey: UserSortKey; current: SortState; onSort: (k: UserSortKey) => void; align: 'left' | 'right';
}) {
  const active = current.key === sortKey;
  return (
    <button onClick={() => onSort(sortKey)} style={{
      display: "flex", alignItems: "center", gap: 4,
      justifyContent: align === "right" ? "flex-end" : "flex-start",
      width: "100%",
      padding: 0, border: "none", background: "transparent",
      color: active ? "var(--foreground)" : "var(--muted)",
      fontSize: 12, fontWeight: 600, letterSpacing: "0.08em", textTransform: "uppercase",
      cursor: "pointer",
    }}>
      <span>{label}</span>
      <span style={{ opacity: active ? 1 : 0, fontSize: 10, color: "var(--accent)" }}>{current.dir === "desc" ? "▼" : "▲"}</span>
    </button>
  );
}

function HeaderCell({ col, sort, onSort }: { col: ColDef; sort: SortState; onSort: (k: UserSortKey) => void }) {
  if (col.sortKey) {
    return <SortableHeader label={col.label} sortKey={col.sortKey} current={sort} onSort={onSort} align={col.align} />;
  }
  return (
    <span style={{ fontSize: 12, fontWeight: 600, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--muted)" }}>{col.label}</span>
  );
}

export interface UsersPageProps {
  users: User[];
  periodLabel: string;
  comparison?: { runs: number; cost: number };
  updatedAt?: number;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

export function UsersPage({ users, periodLabel, comparison, updatedAt, setView, setSelected }: UsersPageProps) {
  const hasMeta = users.some(t => t.region != null);

  const [search, setSearch] = useState<string>("");
  const [sort, setSort] = useState<SortState>({ key: "cost", dir: "desc" });

  const handleSort = (key: UserSortKey) => {
    setSort(s => s.key === key ? { key, dir: s.dir === "desc" ? "asc" : "desc" } : { key, dir: "desc" });
  };

  const visibleCols = useMemo(() => COLUMNS.filter(c => !c.meta || hasMeta), [hasMeta]);
  const cols = visibleCols.map(c => c.width).join(" ");
  const minWidth = hasMeta ? 900 : 560;

  const filtered = useMemo(() => {
    let r = users;
    if (search.trim()) {
      const q = search.trim().toLowerCase();
      r = r.filter(t => t.name.toLowerCase().includes(q) || t.id.toLowerCase().includes(q));
    }
    return r;
  }, [users, search]);

  const sorted = useMemo(() => {
    const arr = [...filtered];
    arr.sort((a, b) => {
      const av = a[sort.key];
      const bv = b[sort.key];
      if (typeof av === "string" && typeof bv === "string") {
        return sort.dir === "desc" ? bv.localeCompare(av) : av.localeCompare(bv);
      }
      if (typeof av === "number" && typeof bv === "number") {
        return sort.dir === "desc" ? bv - av : av - bv;
      }
      return 0;
    });
    return arr;
  }, [filtered, sort]);

  const totalCost = users.reduce((s, t) => s + t.cost, 0);
  const totalRuns = users.reduce((s, t) => s + t.runs, 0);
  const sortedByCost = useMemo(() => [...users].sort((a, b) => b.cost - a.cost), [users]);

  return (
    <div style={{ padding: "clamp(20px, 4vw, 32px) clamp(16px, 4vw, 36px) 64px", maxWidth: 1440, margin: "0 auto" }}>
      <header style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 24, paddingBottom: 28, flexWrap: "wrap" }}>
        <div style={{ minWidth: 0 }}>
          <h1 style={{ fontSize: 28, fontWeight: 600, letterSpacing: "-0.02em", margin: 0, color: "var(--foreground)" }}>Clients</h1>
          <p style={{ fontSize: 14.5, color: "var(--muted)", margin: "2px 0 0" }}>
            {periodLabel} · {users.length} active clients · {fmtCost(totalCost)} this period
          </p>
        </div>
        {updatedAt != null && <LastUpdated at={updatedAt} />}
      </header>

      <KpiStrip
        items={[
          { label: "Active clients", value: users.length.toString(), hint: "this period" },
          {
            label: "Total runs",
            value: fmtNum(totalRuns),
            ...(comparison && deltaParts(toKpi(totalRuns, comparison.runs, false))),
          },
          {
            label: "Spend",
            value: fmtCost(totalCost),
            ...(comparison && deltaParts(toKpi(totalCost, comparison.cost, true))),
            hint: fmtCost(users.length > 0 ? totalCost / users.length : 0) + " / client",
          },
        ]}
      />

      <div style={{
        paddingBottom: 44,
        borderBottom: "1px solid color-mix(in srgb, var(--border) 50%, transparent)",
      }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 6, marginBottom: 20 }}>
          <h2 style={{ fontSize: 15, fontWeight: 600, margin: 0, color: "var(--foreground)" }}>Cost share</h2>
          <p style={{ fontSize: 13.5, color: "var(--muted)", margin: 0 }}>Top 3 clients drive {totalCost > 0 ? ((sortedByCost.slice(0, 3).reduce((s, t) => s + t.cost, 0) / totalCost) * 100).toFixed(0) : "0"}% of spend this period.</p>
        </div>
        <ShareBar users={sortedByCost} totalCost={totalCost} />
      </div>

      <section style={{ paddingTop: 44 }}>
        <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 24, paddingBottom: 18, flexWrap: "wrap" }}>
          <div style={{ display: "flex", flexDirection: "column", gap: 5 }}>
            <h2 style={{ fontSize: 19, fontWeight: 600, letterSpacing: "-0.018em", margin: 0, color: "var(--foreground)" }}>All clients</h2>
            <p style={{ fontSize: 14, color: "var(--muted)", margin: 0 }}>{sorted.length} of {users.length} shown · click a row to drill in.</p>
          </div>
          <div style={{
            display: "flex", alignItems: "center", gap: 8,
            padding: "6px 10px",
            border: "1px solid var(--border)",
            borderRadius: 6, background: "var(--surface-alt)",
            width: 280,
          }}>
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" style={{ color: "var(--muted)" }}><circle cx="7" cy="7" r="4.5"/><path d="m10.5 10.5 3 3"/></svg>
            <input
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder="Search client name or ID…"
              style={{
                flex: 1, background: "transparent", border: "none", outline: "none",
                color: "var(--foreground)", fontSize: 13.5,
                fontFamily: "inherit",
              }}
            />
            {search && (
              <button onClick={() => setSearch("")} style={{ background: "transparent", border: "none", color: "var(--muted)", cursor: "pointer", padding: 0, fontSize: 15, lineHeight: 1 }}>×</button>
            )}
          </div>
        </header>

        <div style={{ overflowX: "auto" }}>
          <div style={{
            display: "grid", gridTemplateColumns: cols, minWidth, gap: 16,
            alignItems: "center", padding: "0 4px 10px",
            borderBottom: "1px solid color-mix(in srgb, var(--border) 70%, transparent)",
          }}>
            {visibleCols.map(c => <HeaderCell key={c.key} col={c} sort={sort} onSort={handleSort} />)}
          </div>

          {sorted.length === 0 ? (
            <div style={{ padding: "60px 0", textAlign: "center", color: "var(--muted)", fontSize: 14 }}>
              No clients match this filter.
            </div>
          ) : sorted.map(t => (
            <div
              key={t.id}
              className={hover.row}
              {...(t.id !== UNATTRIBUTED && rowButtonProps(() => { setSelected(s => ({ ...s, user: t.id })); setView("user"); }))}
              style={{
                display: "grid", gridTemplateColumns: cols, minWidth, gap: 16,
                alignItems: "center", padding: "14px 4px",
                borderBottom: "1px solid color-mix(in srgb, var(--border) 30%, transparent)",
              }}
            >
              {visibleCols.map(c => <React.Fragment key={c.key}>{c.cell(t)}</React.Fragment>)}
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
