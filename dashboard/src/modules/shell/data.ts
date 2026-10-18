import type { Workspace } from './interfaces';
import type { WorkspaceId } from './ids';

export function createDemoWorkspace(): Workspace {
  return {
    id: `demo-${Math.random().toString(36).slice(2, 10)}` as WorkspaceId,
    name: 'Demo',
    slug: 'demo',
    role: 'Owner',
    members: 1,
    env: 'production',
  };
}
