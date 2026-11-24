import type { QueueReason } from '../../../common/interfaces';

export const QUEUE_REASONS: Record<QueueReason, string> = {
  incident: 'Open incident',
  truncated: 'Truncated',
  refused: 'Refused',
  tool_error: 'Tool error',
  sample: 'Random sample',
};
