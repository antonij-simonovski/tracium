import type { Kpi } from '../../modules/overview/interfaces';
import type { KpiItem } from '../components/KpiStrip/KpiStrip';
import { fmtDelta } from './formatters';

// The API marks a KPI neutral exactly when there is no baseline to compare
// against, so that case shows no figure.
export function deltaParts(k: Kpi): Required<Pick<KpiItem, 'delta' | 'deltaTone' | 'hint'>> {
  if (k.delta_type === 'neutral') return { delta: '', deltaTone: 'neutral', hint: 'no change' };
  return { delta: fmtDelta(k.delta), deltaTone: k.delta_type, hint: 'vs prev period' };
}
