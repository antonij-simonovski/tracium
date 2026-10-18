import type { ExtensionPage } from '../../extensions';
import type { ViewId } from './ids';

export interface NavState {
  view: ViewId;
  selected: Record<string, string>;
}

export function stateToPath(view: ViewId, selected: Record<string, string>, pages: readonly ExtensionPage[] = []): string {
  if (pages.some(page => page.id === view)) return `/extensions/${view.slice(10)}`;
  const enc = (v: string) => encodeURIComponent(v);
  switch (view) {
    case 'overview':   return '/';
    case 'workflows':     return selected.workflow ? `/workflows/${enc(selected.workflow)}` : '/workflows';
    case 'compare':    return selected.workflow && selected.base && selected.candidate
      ? `/workflows/${enc(selected.workflow)}/compare/${enc(selected.base)}/${enc(selected.candidate)}`
      : selected.workflow ? `/workflows/${enc(selected.workflow)}` : '/workflows';
    case 'trace':      return selected.traceId ? `/traces/${enc(selected.traceId)}` : '/traces';
    case 'usage':      return '/usage';
    case 'users':    return '/clients';
    case 'user':     return selected.user ? `/clients/${enc(selected.user)}` : '/clients';
    case 'keys':       return '/api-keys';
    case 'settings':   return '/settings';
    default:           return '/';
  }
}

export function pathToState(pathname: string, pages: readonly ExtensionPage[] = []): NavState | null {
  const page = pages.find(page => pathname === `/extensions/${page.id.slice(10)}`);
  if (page) return { view: page.id, selected: {} };
  let seg: string[];
  try { seg = pathname.split('/').filter(Boolean).map(decodeURIComponent); } catch { return null; }

  if (seg.length === 0) return { view: 'overview', selected: {} };

  switch (seg[0]) {
    case 'workflows':
      if (seg[2] === 'compare' && seg[3] && seg[4]) {
        return { view: 'compare', selected: { workflow: seg[1], base: seg[3], candidate: seg[4] } };
      }
      return seg[1]
        ? { view: 'workflows', selected: { workflow: seg[1] } }
        : { view: 'workflows', selected: {} };
    case 'traces':
      return seg[1]
        ? { view: 'trace', selected: { traceId: seg[1] } }
        : { view: 'trace', selected: {} };
    case 'usage':
      return { view: 'usage', selected: {} };
    case 'clients':
    case 'users':
      return seg[1]
        ? { view: 'user', selected: { user: seg[1] } }
        : { view: 'users', selected: {} };
    case 'api-keys':
      return { view: 'keys', selected: {} };
    case 'settings':
      return { view: 'settings', selected: {} };
    default:
      return null;
  }
}
