// niceCeil rounds a value up to a 1/2/5 × 10ⁿ step, so an axis top is a clean
// number across scales (cents to dollars, or tens to thousands of runs).
export function niceCeil(v: number): number {
  if (v <= 0) return 1;
  const mag = Math.pow(10, Math.floor(Math.log10(v)));
  const n = v / mag;
  const step = n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10;
  return step * mag;
}

// Up to five evenly spaced tick indices for the x-axis.
export function tickIndices(n: number): number[] {
  if (n <= 1) return [0];
  const raw = [0, 0.25, 0.5, 0.75, 1].map((t) => Math.round((n - 1) * t));
  return Array.from(new Set(raw));
}
