import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AccuracySection } from './AccuracySection';

const api = vi.hoisted(() => ({
  getStepAccuracy: vi.fn(),
  getFieldAccuracy: vi.fn(),
  getReviews: vi.fn(),
}));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ metricsAPI: api }),
}));

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AccuracySection workflow="extract" version="v2" onOpenTrace={() => {}} />
    </QueryClientProvider>,
  );
}

const acc = (span: string, version: string, correct: number, reviewed: number) => ({ span, version, reviewed_runs: reviewed, correct_runs: correct, accuracy: correct / reviewed });

beforeEach(() => {
  vi.resetAllMocks();
  api.getStepAccuracy.mockResolvedValue([acc('classify', 'v2', 30, 40), acc('classify', 'v1', 38, 40), acc('', 'v2', 90, 100), acc('', 'v1', 60, 100)]);
  api.getFieldAccuracy.mockResolvedValue([]);
  api.getReviews.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 });
});

describe('accuracy section', () => {
  it('lays out run and span accuracy by release and opens on the version in view', async () => {
    show();
    expect(await screen.findByRole('button', { name: 'Whole run v2' })).toHaveTextContent('90');
    expect(await screen.findByRole('button', { name: 'classify v1' })).toHaveTextContent('95');
    expect(api.getStepAccuracy).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: 'Whole run v2' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('+30.0 pts')).toBeInTheDocument();
    expect(api.getFieldAccuracy).toHaveBeenCalledWith('extract', 'v1', '');
  });

  it('loads a picked cell and shows changes in change mode', async () => {
    show();
    fireEvent.click(await screen.findByRole('button', { name: 'classify v2' }));
    expect(api.getFieldAccuracy).toHaveBeenLastCalledWith('extract', 'v1', 'classify');
    expect(api.getReviews).toHaveBeenLastCalledWith('extract', expect.objectContaining({ version: 'v2', span: 'classify', verdicts: ['incorrect', 'missing'] }));
    fireEvent.click(screen.getByRole('button', { name: 'Change' }));
    expect(screen.getByRole('button', { name: 'classify v2' })).toHaveTextContent('−20');
    expect(screen.getByRole('button', { name: 'Whole run v2' })).toHaveTextContent('+30');
  });

  it('shows corrections of the weakest field', async () => {
    api.getFieldAccuracy.mockResolvedValue([
      { path: 'total', reviewed: 100, wrong: 4, missing: 0, accuracy: 0.96 },
      { path: 'vendor', reviewed: 100, wrong: 9, missing: 3, accuracy: 0.91 },
    ]);
    api.getReviews.mockResolvedValue({
      items: [{ trace_id: 'tr_1', span_id: '', verdict: 'incorrect', output: '', corrected_output: '', note: '', fields: [{ path: 'vendor', output: '"Acme"', corrected: '"ACME Ltd"', correct: false, missing: false }] }],
      total: 1, page: 1, page_size: 20,
    });
    show();
    expect(await screen.findByText('"ACME Ltd"', { exact: false })).toBeInTheDocument();
    expect(screen.getByText('Wrong value')).toBeInTheDocument();
    expect(api.getReviews).toHaveBeenLastCalledWith('extract', expect.objectContaining({ field: 'vendor' }));
    expect(screen.getByText('6 · 3')).toBeInTheDocument();
  });

  it('renders nothing without reviews', async () => {
    api.getStepAccuracy.mockResolvedValue([]);
    const { container } = show();
    await new Promise(r => setTimeout(r, 20));
    expect(container).toBeEmptyDOMElement();
  });
});
