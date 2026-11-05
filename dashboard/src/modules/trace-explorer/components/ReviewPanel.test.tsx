import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { APIError } from '../../../common/api';
import { ReviewPanel } from './ReviewPanel';

const api = vi.hoisted(() => ({ getReview: vi.fn(), saveReview: vi.fn(), deleteReview: vi.fn() }));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ tracesAPI: api }),
}));

function show(output: string | null, spanId?: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ReviewPanel traceId="run-1" spanId={spanId} output={output} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  api.getReview.mockRejectedValue(new APIError('none', 404, 'REVIEW_NOT_FOUND'));
  api.saveReview.mockResolvedValue({});
});

describe('review panel', () => {
  it('marks a run correct', async () => {
    show('hello');
    fireEvent.click(await screen.findByRole('button', { name: 'Mark correct' }));
    await waitFor(() => expect(api.saveReview).toHaveBeenCalledWith('run-1', { verdict: 'correct' }, undefined));
  });

  it('corrects a JSON output field by field', async () => {
    show('{"total": 10, "account": 12345678901234567890, "customer": {"city": "Oslo"}}');
    fireEvent.click(await screen.findByRole('button', { name: 'Correct output' }));
    fireEvent.change(screen.getByLabelText('customer.city'), { target: { value: 'Bergen' } });
    fireEvent.change(screen.getByLabelText('total'), { target: { value: '12' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save correction' }));
    await waitFor(() => expect(api.saveReview).toHaveBeenCalled());
    const [, input] = api.saveReview.mock.calls[0];
    expect(input).toEqual({ corrections: { total: 12, 'customer.city': 'Bergen' }, note: '' });
  });

  it('sends the whole object when an edited key has a dot in it', async () => {
    show('{"a.b": 1, "c": 2}');
    fireEvent.click(await screen.findByRole('button', { name: 'Correct output' }));
    fireEvent.change(screen.getByLabelText('a.b'), { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save correction' }));
    await waitFor(() => expect(api.saveReview).toHaveBeenCalled());
    const [, input] = api.saveReview.mock.calls[0];
    expect(JSON.parse(input.corrected_output)).toEqual({ 'a.b': 3, c: 2 });
  });

  it('keeps earlier field fixes when a correction is edited again', async () => {
    api.getReview.mockResolvedValue({
      trace_id: 'run-1', verdict: 'incorrect', output: '{"total": 10, "due": "2026-01-01"}', corrected_output: '{"total": 10, "due": "2026-02-01"}', note: '',
      updated_at: '2026-10-31T10:00:00Z', fields: [],
    });
    show('{"total": 10, "due": "2026-01-01"}');
    fireEvent.click(await screen.findByRole('button', { name: 'Edit correction' }));
    expect(screen.getByLabelText('due')).toHaveValue('2026-02-01');
    fireEvent.change(screen.getByLabelText('total'), { target: { value: '12' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save correction' }));
    await waitFor(() => expect(api.saveReview).toHaveBeenCalled());
    expect(api.saveReview.mock.calls[0][1]).toEqual({ corrections: { total: 12, due: '2026-02-01' }, note: '' });
  });

  it('shows graded fields of an existing review', async () => {
    api.getReview.mockResolvedValue({
      trace_id: 'run-1', verdict: 'incorrect', output: '{"total": 10}', corrected_output: '{"total": 12}', note: '',
      updated_at: '2026-10-31T10:00:00Z', fields: [{ path: 'total', output: '10', corrected: '12', correct: false }],
    });
    show('{"total": 10}');
    expect(await screen.findByText('Incorrect')).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /total/ })).toHaveTextContent('10→12');
  });

  it('marks a run with no output missing', async () => {
    show(null);
    fireEvent.click(await screen.findByRole('button', { name: 'Mark missing' }));
    await waitFor(() => expect(api.saveReview).toHaveBeenCalledWith('run-1', { verdict: 'missing' }, undefined));
  });

  it('keeps every digit of a big number when editing as raw JSON', async () => {
    const out = '{"account": 12345678901234567890}';
    show(out);
    fireEvent.click(await screen.findByRole('button', { name: 'Correct output' }));
    fireEvent.click(screen.getByLabelText(/Edit as raw JSON/));
    expect(screen.getByLabelText('Corrected output')).toHaveValue(out);
  });

  it('reopens a correction that removed a field as raw JSON, so the removal survives', async () => {
    api.getReview.mockResolvedValue({
      trace_id: 'run-1', verdict: 'incorrect', output: '{"total": 10, "extra": 1}', corrected_output: '{"total": 10}', note: '',
      updated_at: '2026-10-31T10:00:00Z', fields: [],
    });
    show('{"total": 10, "extra": 1}');
    fireEvent.click(await screen.findByRole('button', { name: 'Edit correction' }));
    expect(screen.getByLabelText('Corrected output')).toHaveValue('{"total": 10}');
  });

  it('reviews a single span and labels reviews sent by API', async () => {
    api.getReview.mockResolvedValue({
      trace_id: 'run-1', span_id: 'llm', verdict: 'correct', output: 'hi', corrected_output: null, note: '',
      source: 'api', updated_at: '2026-10-31T10:00:00Z', fields: [],
    });
    show('hi', 'llm');
    expect(await screen.findByText(/sent by API/)).toBeInTheDocument();
    expect(api.getReview).toHaveBeenCalledWith('run-1', 'llm');
    fireEvent.click(screen.getByRole('button', { name: 'Mark correct' }));
    await waitFor(() => expect(api.saveReview).toHaveBeenCalledWith('run-1', { verdict: 'correct' }, 'llm'));
  });
});
