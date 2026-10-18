export const VIEW_IDS = ['overview', 'workflows', 'compare', 'trace', 'usage', 'keys', 'settings', 'users', 'user'] as const;

export type ViewId = (typeof VIEW_IDS)[number] | `extension:${string}`;
