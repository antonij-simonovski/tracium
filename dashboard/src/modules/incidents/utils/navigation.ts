import type { Incident } from '../interfaces';

type SetSelected = (updater: (prev: Record<string, string>) => Record<string, string>) => void;

export function openSubject(inc: Pick<Incident, 'scope' | 'subject'>, setView: (v: string) => void, setSelected: SetSelected) {
  if (inc.scope === 'workflow') {
    setSelected((s) => ({ ...s, workflow: inc.subject }));
    setView('workflows');
  } else if (inc.scope === 'client') {
    setSelected((s) => ({ ...s, user: inc.subject }));
    setView('user');
  }
}

export function openIncident(id: string, setView: (v: string) => void, setSelected: SetSelected) {
  setSelected((s) => ({ ...s, incident: id }));
  setView('incidents');
}

export function openIncidentList(setView: (v: string) => void, setSelected: SetSelected) {
  setSelected(({ incident: _, ...rest }) => rest);
  setView('incidents');
}
