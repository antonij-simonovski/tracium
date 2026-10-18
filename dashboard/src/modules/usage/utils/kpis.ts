import { deltaParts, fmtCost, fmtNum, fmtTokens } from '../../../common';
import type { KpiItem } from '../../../common';
import type { KpiSet } from '../../overview/interfaces';

export function spendTile(kpis: KpiSet): KpiItem {
  return { label: 'Spend', value: fmtCost(kpis.cost.value), ...deltaParts(kpis.cost) };
}

export function runsTile(kpis: KpiSet): KpiItem {
  const runs = kpis.runs.value;
  const avgPer1k = runs > 0 ? (kpis.cost.value / runs) * 1000 : 0;
  return {
    label: 'Runs',
    value: fmtNum(Math.round(runs)),
    ...deltaParts(kpis.runs),
    hint: fmtCost(avgPer1k) + ' / 1K runs',
  };
}

// tokens is undefined until the model-cost query that supplies it resolves.
export function tokensTile(tokens: number | undefined): KpiItem {
  return {
    label: 'Tokens',
    value: tokens != null ? fmtTokens(tokens) : '—',
    hint: 'input + output',
  };
}
