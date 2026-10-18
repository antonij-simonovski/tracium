import type { Severity } from '../utils/severity';

export interface SetupIssue {
  code: string;
  severity: Severity;
  message: string;
}
