// Error-rate fractions above which a workflow reads as failing / degraded.
export const ERR_BAD = 0.02;
export const ERR_WARN = 0.005;

// error_rate is failed/calls, so rounding the product recovers the failed count.
interface RunOutcomes {
  completed: number;
  failed: number;
}

export function deriveRunOutcomes(calls: number, errorRate: number): RunOutcomes {
  const failed = Math.min(calls, Math.max(0, Math.round(calls * errorRate)));
  return { completed: calls - failed, failed };
}
