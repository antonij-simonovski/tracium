import { describe, expect, it } from 'vitest';
import { workflowStatus } from './incidents';
import type { Incident } from '../interfaces';

const inc = (subject: string, severity: Incident['severity'], scope: Incident['scope'] = 'workflow') => ({ subject, severity, scope }) as Incident;

describe('workflowStatus', () => {
  it('maps each workflow to its worst open severity and skips info and other scopes', () => {
    expect(workflowStatus([
      inc('a', 'warning'), inc('a', 'critical'), inc('b', 'warning'), inc('c', 'info'), inc('d', 'critical', 'client'),
    ])).toEqual({ a: 'critical', b: 'warning' });
  });
});
