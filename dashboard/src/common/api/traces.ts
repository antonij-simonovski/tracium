import { BaseAPIClient } from './client';
import type { PaginatedResponse, ReviewInput, ReviewQueueItem, RunReview } from '../interfaces';
import type { Trace, TraceDetail, TraceFilter } from '../../modules/trace-explorer/interfaces';

const reviewPath = (id: string, spanId?: string) =>
  `/traces/${encodeURIComponent(id)}${spanId ? `/spans/${encodeURIComponent(spanId)}` : ''}/review`;

export class TracesAPI extends BaseAPIClient {
  async listTraces(filter: TraceFilter): Promise<PaginatedResponse<Trace>> {
    return this.get('/traces', { ...filter });
  }

  async getTrace(id: string): Promise<TraceDetail> {
    return this.get(`/traces/${encodeURIComponent(id)}`);
  }

  getReview(id: string, spanId?: string): Promise<RunReview> {
    return this.get(reviewPath(id, spanId));
  }

  saveReview(id: string, input: ReviewInput, spanId?: string): Promise<RunReview> {
    return this.put(reviewPath(id, spanId), input);
  }

  getReviewQueue(workflow?: string, limit?: number): Promise<ReviewQueueItem[]> {
    return this.get('/reviews/queue', { workflow, limit });
  }

  deleteReview(id: string, spanId?: string): Promise<void> {
    return this.delete(reviewPath(id, spanId));
  }
}
