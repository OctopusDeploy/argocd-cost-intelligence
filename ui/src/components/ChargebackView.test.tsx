import React from 'react';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ChargebackView } from './ChargebackView';
import { useChargeback } from '../hooks/useChargeback';
import { useLabelKeys } from '../hooks/useLabelKeys';
import { ChargebackEntry, NamespaceSummary } from '../utils/api';

vi.mock('../hooks/useChargeback', () => ({ useChargeback: vi.fn() }));
vi.mock('../hooks/useLabelKeys', () => ({ useLabelKeys: vi.fn() }));

const namespaces = [
  { namespace: 'argocd', totalCost: 2018.1, appCount: 2 },
  { namespace: 'prometheus', totalCost: 865.5, appCount: 1 },
] as NamespaceSummary[];

const entry = (labelValue: string, totalCost: number, efficiency: number): ChargebackEntry => ({
  labelValue,
  cpuCost: totalCost * 0.6,
  ramCost: totalCost * 0.3,
  storageCost: totalCost * 0.05,
  networkCost: totalCost * 0.03,
  lbCost: totalCost * 0.02,
  totalCost,
  efficiency,
});

function withEntries(entries: ChargebackEntry[], overrides: Partial<ReturnType<typeof useChargeback>> = {}) {
  vi.mocked(useChargeback).mockReturnValue({
    entries,
    totalCost: entries.reduce((sum, e) => sum + e.totalCost, 0),
    loading: false,
    error: null,
    ...overrides,
  });
}

// lastQuery reports the arguments of the most recent useChargeback call, which is what the view
// asks the backend for.
function lastQuery(): { labelKey: string | null; namespace: string | null } {
  const calls = vi.mocked(useChargeback).mock.calls;
  const [labelKey, namespace] = calls[calls.length - 1];
  return { labelKey, namespace };
}

// The label keys pods in the cluster carry, as the backend reports them.
const clusterKeys = ['agent_name', 'app', 'app_kubernetes_io_name', 'helm_sh_chart', 'owner', 'team', 'zone'];

function withLabelKeys(keys: string[] | null, failed = false) {
  vi.mocked(useLabelKeys).mockReturnValue({ keys, failed });
}

const chipLabels = () =>
  within(screen.getByRole('group', { name: 'Suggested label keys' }))
    .getAllByRole('button')
    .map(button => button.textContent);

beforeEach(() => {
  vi.mocked(useChargeback).mockReset();
  vi.mocked(useLabelKeys).mockReset();
  withEntries([entry('platform', 1200, 0.85), entry('payments', 400, 0.3)]);
  withLabelKeys(clusterKeys);
});

describe('ChargebackView controls', () => {
  it('queries the label it starts on, with no namespace filter', () => {
    render(<ChargebackView namespaces={namespaces} />);

    expect(lastQuery()).toEqual({ labelKey: 'team', namespace: null });
  });

  it('starts from the grouping a link names', () => {
    window.history.replaceState(null, '', '/cost-intelligence?costTab=chargeback&costLabel=owner&costScope=argocd');

    render(<ChargebackView namespaces={namespaces} />);

    expect(lastQuery()).toEqual({ labelKey: 'owner', namespace: 'argocd' });
    expect((screen.getByLabelText('Label key') as HTMLInputElement).value).toBe('owner');
    expect((screen.getByLabelText('Namespace') as HTMLSelectElement).value).toBe('argocd');
  });

  it('puts the grouping in the URL without stacking up Back steps', () => {
    render(<ChargebackView namespaces={namespaces} />);
    const before = window.history.length;

    fireEvent.click(screen.getByRole('button', { name: 'owner' }));
    fireEvent.change(screen.getByLabelText('Namespace'), { target: { value: 'prometheus' } });

    const params = new URLSearchParams(window.location.search);
    expect(params.get('costLabel')).toBe('owner');
    expect(params.get('costScope')).toBe('prometheus');
    expect(window.history.length).toBe(before);
  });

  it('has nothing to submit', () => {
    render(<ChargebackView namespaces={namespaces} />);

    expect(screen.queryByRole('button', { name: /apply/i })).toBeNull();
  });

  it('offers the label keys pods carry, the commonly used ones first', () => {
    render(<ChargebackView namespaces={namespaces} />);

    const suggestions = within(screen.getByRole('group', { name: 'Suggested label keys' })).getAllByRole('button');

    expect(suggestions.map(button => button.textContent)).toEqual([
      'team',
      'owner',
      'app_kubernetes_io_name',
      'app',
      'agent_name',
      'helm_sh_chart',
    ]);
    expect(suggestions[0].getAttribute('aria-pressed')).toBe('true');
    expect(suggestions[1].getAttribute('aria-pressed')).toBe('false');
  });

  it('suggests every label key while typing', () => {
    render(<ChargebackView namespaces={namespaces} />);

    const input = screen.getByLabelText('Label key');
    const list = document.getElementById(input.getAttribute('list') ?? '');

    expect(Array.from(list?.querySelectorAll('option') ?? []).map(option => option.getAttribute('value'))).toEqual(
      clusterKeys
    );
  });

  it('asks for the label keys of the namespace in scope', () => {
    render(<ChargebackView namespaces={namespaces} />);
    expect(vi.mocked(useLabelKeys).mock.calls.at(-1)).toEqual([null]);

    fireEvent.change(screen.getByLabelText('Namespace'), { target: { value: 'prometheus' } });

    expect(vi.mocked(useLabelKeys).mock.calls.at(-1)).toEqual(['prometheus']);
  });

  it('shows no suggestions until the label keys arrive', () => {
    withLabelKeys(null);
    render(<ChargebackView namespaces={namespaces} />);

    expect(screen.queryByRole('group', { name: 'Suggested label keys' })).toBeNull();
  });

  it('falls back to commonly used keys when the label keys cannot be listed', () => {
    withLabelKeys(null, true);
    render(<ChargebackView namespaces={namespaces} />);

    expect(chipLabels()).toEqual(['team', 'app', 'env', 'tier', 'owner', 'cost-center']);
  });

  it('marks a suggestion pressed when the key is typed with its dots and slashes', () => {
    window.history.replaceState(null, '', '/cost-intelligence?costTab=chargeback&costLabel=app.kubernetes.io/name');
    render(<ChargebackView namespaces={namespaces} />);

    expect(screen.getByRole('button', { name: 'app_kubernetes_io_name' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByRole('button', { name: 'app' }).getAttribute('aria-pressed')).toBe('false');
  });

  it('queries at once when a suggestion is picked', () => {
    render(<ChargebackView namespaces={namespaces} />);

    fireEvent.click(screen.getByRole('button', { name: 'owner' }));

    expect(lastQuery().labelKey).toBe('owner');
    expect(screen.getByRole('button', { name: 'owner' }).getAttribute('aria-pressed')).toBe('true');
  });

  it('queries at once when the namespace changes', () => {
    render(<ChargebackView namespaces={namespaces} />);

    fireEvent.change(screen.getByLabelText('Namespace'), { target: { value: 'prometheus' } });

    expect(lastQuery()).toEqual({ labelKey: 'team', namespace: 'prometheus' });
  });
});

describe('ChargebackView typing', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('waits for typing to settle before querying', () => {
    render(<ChargebackView namespaces={namespaces} />);
    const field = screen.getByLabelText('Label key');

    fireEvent.change(field, { target: { value: 'tie' } });
    fireEvent.change(field, { target: { value: 'tier' } });
    expect(lastQuery().labelKey).toBe('team');

    act(() => void vi.advanceTimersByTime(400));

    expect(lastQuery().labelKey).toBe('tier');
  });

  it('queries immediately on Enter', () => {
    render(<ChargebackView namespaces={namespaces} />);
    const field = screen.getByLabelText('Label key');

    fireEvent.change(field, { target: { value: 'squad' } });
    fireEvent.keyDown(field, { key: 'Enter' });

    expect(lastQuery().labelKey).toBe('squad');
  });
});

describe('ChargebackView results', () => {
  it('lists each label value with its costs and share', () => {
    render(<ChargebackView namespaces={namespaces} />);

    const rows = screen.getAllByRole('row');
    const platform = within(rows[1]).getAllByRole('cell').map(cell => cell.textContent);

    // CPU, memory, storage, network (plus load balancers), total.
    expect(platform.slice(0, 5)).toEqual(['$720.00', '$360.00', '$60.00', '$60.00', '$1,200.00']);
    expect(within(rows[1]).getByLabelText('85%, Healthy')).toBeTruthy();
    expect(within(rows[2]).getByLabelText('30%, Over-provisioned')).toBeTruthy();
  });

  it('shows no GPU column when no value has GPU cost', () => {
    render(<ChargebackView namespaces={namespaces} />);

    expect(screen.queryByRole('columnheader', { name: 'GPU' })).toBeNull();
  });

  it('adds a GPU column when a value has GPU cost, so the columns still add up to the total', () => {
    withEntries([{ ...entry('ml', 1000, 0.5), cpuCost: 300, gpuCost: 300 }, entry('web', 200, 0.5)]);
    render(<ChargebackView namespaces={namespaces} />);

    expect(screen.getByRole('columnheader', { name: 'GPU' })).toBeTruthy();
    const rows = screen.getAllByRole('row');
    const ml = within(rows[1]).getAllByRole('cell').map(cell => cell.textContent);
    // CPU, memory, storage, network (plus load balancers), GPU, total.
    expect(ml.slice(0, 6)).toEqual(['$300.00', '$300.00', '$50.00', '$50.00', '$300.00', '$1,000.00']);
    const amount = (text: string | null) => Number((text ?? '').replace(/[$,]/g, ''));
    expect(ml.slice(0, 5).reduce((sum, text) => sum + amount(text), 0)).toBe(amount(ml[5]));
    expect(within(rows[2]).getAllByRole('cell')[4].textContent).toBe('$0.00');
  });

  it('rounds the cells so they add up to the total beside them, and the rows to the attributed cost', () => {
    const exact = (labelValue: string, cpuCost: number, ramCost: number): ChargebackEntry => ({
      labelValue,
      cpuCost,
      ramCost,
      storageCost: 0,
      networkCost: 0,
      lbCost: 0,
      totalCost: cpuCost + ramCost,
      efficiency: 0.5,
    });
    // Each rounded on its own, CPU and memory would show as $0.09 each beside a total of $0.17, and
    // the rows as $12.96, $12.84, $1.69 and $0.17 under an attributed cost of $27.65.
    withEntries([exact('a', 6.4776, 6.4775), exact('b', 6.4224, 6.4223), exact('c', 1.6, 0.0851), exact('d', 0.085, 0.085)]);
    const { container } = render(<ChargebackView namespaces={namespaces} />);

    const rows = screen.getAllByRole('row');
    expect(within(rows[4]).getAllByRole('cell').map(cell => cell.textContent).slice(0, 5)).toEqual([
      '$0.09',
      '$0.08',
      '$0.00',
      '$0.00',
      '$0.17',
    ]);
    expect(container.querySelector('dd')?.textContent).toBe('$27.66');
  });

  it('reads a value whose pods request nothing as having no requests, not as efficient', () => {
    withEntries([{ ...entry('batch', 400, 0), noRequests: true }, entry('jobs', 200, 0)]);
    render(<ChargebackView namespaces={namespaces} />);

    const rows = screen.getAllByRole('row');
    expect(within(rows[1]).getByLabelText('No CPU or memory requests set').textContent).toBe('No request set');
    expect(within(rows[2]).getByLabelText('Efficiency not measured')).toBeTruthy();
  });

  it('sums the attributed cost and names the largest value', () => {
    const { container } = render(<ChargebackView namespaces={namespaces} />);

    const stats = container.querySelector('dl') as HTMLElement;
    const figures = [...stats.querySelectorAll('dd')].map(dd => dd.textContent);

    expect(figures).toEqual([
      '$1,600.00',
      'Grouped by team across the cluster',
      '2',
      'Pods without the label are excluded',
      'platform',
      '$1,200.00 · 75%',
    ]);
  });

  it('says so when no pod carries the label', () => {
    withEntries([]);
    render(<ChargebackView namespaces={namespaces} />);

    fireEvent.change(screen.getByLabelText('Namespace'), { target: { value: 'prometheus' } });

    expect(screen.getByText(/No pods carry the label/).textContent).toContain('in prometheus');
  });

  it('reports a failed query', () => {
    withEntries([], { error: 'proxy unreachable' });
    render(<ChargebackView namespaces={namespaces} />);

    expect(screen.getByRole('alert').textContent).toContain('proxy unreachable');
  });

  it('asks for a label key once the field is empty', () => {
    render(<ChargebackView namespaces={namespaces} />);

    fireEvent.change(screen.getByLabelText('Label key'), { target: { value: '' } });
    fireEvent.keyDown(screen.getByLabelText('Label key'), { key: 'Enter' });

    expect(screen.getByText('Enter a label key to group costs by its values.')).toBeTruthy();
  });
});
