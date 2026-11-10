import { BaseAPIClient } from './client';
import type { PaginatedResponse } from '../interfaces';
import type { Incident, IncidentStatus, MuteInput } from '../../modules/incidents/interfaces';

export class IncidentsAPI extends BaseAPIClient {
  listIncidents(status: IncidentStatus, page = 1, pageSize = 50): Promise<PaginatedResponse<Incident>> {
    return this.get('/incidents', { status, page, page_size: pageSize });
  }

  getIncident(id: string): Promise<Incident> {
    return this.get(`/incidents/${encodeURIComponent(id)}`);
  }

  resolveIncident(id: string): Promise<Incident> {
    return this.post(`/incidents/${encodeURIComponent(id)}/resolve`, {});
  }

  muteIncident(id: string, input: MuteInput): Promise<unknown> {
    return this.post(`/incidents/${encodeURIComponent(id)}/mute`, input);
  }
}
