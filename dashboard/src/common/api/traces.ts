import { BaseAPIClient } from './client';
import type { PaginatedResponse } from '../interfaces';
import type { Trace, TraceDetail, TraceFilter } from '../../modules/trace-explorer/interfaces';

export class TracesAPI extends BaseAPIClient {
  async listTraces(filter: TraceFilter): Promise<PaginatedResponse<Trace>> {
    return this.get('/traces', { ...filter });
  }

  async getTrace(id: string): Promise<TraceDetail> {
    return this.get(`/traces/${encodeURIComponent(id)}`);
  }
}
