import React from 'react';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { NamespaceDashboard } from './NamespaceDashboard';
import { useArgoApplications } from '../hooks/useArgoApplications';
import { useChargeback } from '../hooks/useChargeback';
import { useClusterOverhead } from '../hooks/useClusterOverhead';
import { useDataQuality } from '../hooks/useDataQuality';
import { UseNamespaceDataResult, useNamespaceData } from '../hooks/useNamespaceData';
import { useOrphanedPods } from '../hooks/useOrphanedPods';
import { useWasteRanking } from '../hooks/useWasteRanking';
import { CostData, PodCost } from '../utils/api';

vi.mock('../hooks/useNamespaceData', () => ({ useNamespaceData: vi.fn() }));
vi.mock('../hooks/useArgoApplications', () => ({ useArgoApplications: vi.fn() }));
vi.mock('../hooks/useClusterOverhead', () => ({ useClusterOverhead: vi.fn() }));
vi.mock('../hooks/useDataQuality', () => ({ useDataQuality: vi.fn() }));
vi.mock('../hooks/useOrphanedPods', () => ({ useOrphanedPods: vi.fn() }));
vi.mock('../hooks/useChargeback', () => ({ useChargeback: vi.fn() }));
vi.mock('../hooks/useWasteRanking', () => ({ useWasteRanking: vi.fn() }));

const DAY_MS = 86_400_000;
const measuredAt = new Date(Date.now() - 2 * DAY_MS).toISOString();

const pod = (name: string, controller: string, cpuCost: number, ramCost: number, totalCost: number): PodCost => ({
  name,
  namespace: 'argocd',
  controller,
  controllerKind: 'Deployment',
  cpuCost,
  ramCost,
  totalCost,
});

const snapshot = [pod('redis-66b8cb-1', 'redis', 904.5, 603, 1642.5), pod('guestbook-ui-abc-1', 'guestbook-ui', 217.2, 150.9, 375.6)];

const record = (applicationName: string): CostData => ({
  applicationName,
  applicationNamespace: 'argocd',
  measuredAt,
  totalMonthlyCost: 2018.1,
  pods: snapshot,
  opencostOutput: {
    allocationTotals: {
      cpuCost: 1121.7,
      memoryCost: 753.9,
      storageCost: 45,
      gpuCost: 0,
      networkCost: 37.5,
      loadBalancerCost: 60,
      totalCost: 2018.1,
    },
  },
  efficiency: {
    cpuEfficiency: 0.4,
    ramEfficiency: 0.7,
    totalEfficiency: 0.55,
    cpuCoresRequested: 1,
    cpuCoresUsed: 0.4,
    ramBytesRequested: 536870912,
    ramBytesUsed: 375809638,
    cpuIdleCost: 673.02,
    ramIdleCost: 226.17,
    cpuCoresLimit: 2,
    ramBytesLimit: 1073741824,
    cpuCoresUsedMax: 0,
    ramBytesUsedMax: 0,
  },
});

const setSelectedNs = vi.fn();
const reloadWaste = vi.fn(() => Promise.resolve());

function withNamespaceData(overrides: Partial<UseNamespaceDataResult> = {}) {
  vi.mocked(useNamespaceData).mockReturnValue({
    namespaces: [
      { namespace: 'argocd', totalCost: 2018.1, idleCost: 899.19, appCount: 2, measuredAt },
      { namespace: 'prometheus', totalCost: 865.5, idleCost: 416.16, appCount: 1, measuredAt },
    ],
    namespacesLoading: false,
    namespacesError: null,
    selectedNs: 'argocd',
    setSelectedNs,
    apps: [record('redis'), record('test-app')],
    appsLoading: false,
    appsError: null,
    reload: vi.fn(() => Promise.resolve()),
    ...overrides,
  });
}

// The cluster's stat comes first, the selected namespace's after it.
const stat = (label: string, index = 0) => {
  const term = screen.getAllByText(label, { selector: 'dt' })[index];
  return { value: term.nextElementSibling?.textContent, detail: term.nextElementSibling?.nextElementSibling?.textContent };
};

describe('NamespaceDashboard', () => {
  beforeEach(() => {
    setSelectedNs.mockClear();
    withNamespaceData();
    vi.mocked(useArgoApplications).mockReturnValue({
      applications: [
        { name: 'redis', namespace: 'argocd', resources: [{ kind: 'Deployment', name: 'redis', namespace: 'argocd' }] },
        { name: 'test-app', namespace: 'argocd', resources: [{ kind: 'Deployment', name: 'guestbook-ui', namespace: 'argocd' }] },
      ],
      loading: false,
      error: null,
    });
    vi.mocked(useClusterOverhead).mockReturnValue({ overhead: null, loading: false, error: '500: failed to retrieve cluster overhead' });
    vi.mocked(useDataQuality).mockReturnValue({ pricingSource: 'estimated', message: 'Public cloud list prices (on-demand)' });
    vi.mocked(useOrphanedPods).mockReturnValue({ pods: [], totalWasted: 0, loading: false });
    vi.mocked(useChargeback).mockReturnValue({ entries: [], totalCost: 0, loading: false, error: null });
    reloadWaste.mockClear();
    vi.mocked(useWasteRanking).mockReturnValue({
      ranking: {
        apps: [
          {
            applicationNamespace: 'argocd',
            applicationName: 'prometheus',
            deploymentNamespace: 'prometheus',
            measuredAt,
            monthlyCost: 865.5,
            cpuCost: 500,
            idleCost: 416.16,
            efficiency: record('prometheus').efficiency!,
          },
        ],
        idleCost: 416.16,
        namespaceOnly: 0,
        noPods: 0,
      },
      loading: false,
      error: null,
      reload: reloadWaste,
    });
  });

  it('summarises the cluster and explains a missing control plane fee', () => {
    render(<NamespaceDashboard />);

    expect(stat('Cluster monthly cost')).toEqual({ value: '$2,883.60', detail: '2 namespaces · 3 apps' });
    expect(stat('Control plane fee')).toEqual({ value: 'Not available', detail: "OpenCost didn't return cluster assets" });
    expect(stat('Pricing').value).toBe('Estimated');
  });

  it('shows idle spend as a share of the cost it was measured for', () => {
    render(<NamespaceDashboard />);

    expect(stat('Idle spend')).toEqual({ value: '$1,315.35', detail: '46% of the cluster cost · See which apps' });
    expect(stat('Idle spend', 1)).toEqual({ value: '$899.19', detail: '45% of the namespace cost' });
  });

  it('leaves out of idle spend the namespaces it was not measured for', () => {
    withNamespaceData({
      namespaces: [
        { namespace: 'argocd', totalCost: 2018.1, idleCost: null, appCount: 2, measuredAt },
        { namespace: 'prometheus', totalCost: 865.5, idleCost: 416.16, appCount: 1, measuredAt },
        { namespace: 'quiet', totalCost: 40, idleCost: 0, appCount: 1, measuredAt },
      ],
    });
    render(<NamespaceDashboard />);

    expect(stat('Idle spend')).toEqual({
      value: '$416.16',
      detail: '46% of the cost of the 2 of 3 namespaces measured · See which apps',
    });
    expect(stat('Idle spend', 1)).toEqual({ value: '—', detail: 'Not measured yet. Measured at the next sync' });
  });

  it('shows idle spend as not measured when no namespace has it, and still leads to the apps', () => {
    withNamespaceData({
      namespaces: [{ namespace: 'argocd', totalCost: 2018.1, idleCost: null, appCount: 2, measuredAt }],
    });
    render(<NamespaceDashboard />);

    expect(stat('Idle spend')).toEqual({
      value: '—',
      detail: "Not measured yet. Measured at each namespace's next sync · See which apps",
    });
  });

  it('shows the selected namespace and lets another be picked', () => {
    render(<NamespaceDashboard />);
    const list = screen.getByRole('navigation', { name: 'Namespaces' });

    expect(within(list).getByRole('button', { name: /^argocd/ }).getAttribute('aria-current')).toBe('true');
    expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('argocd');
    expect(stat('Idle spend').value).toBe('$1,315.35');

    fireEvent.click(within(list).getByRole('button', { name: /^prometheus/ }));
    expect(setSelectedNs).toHaveBeenCalledWith('prometheus');
  });

  it('counts each pod once, under the app that manages it, with links to the app', () => {
    render(<NamespaceDashboard />);
    const table = screen.getByRole('table', { name: 'Applications' });
    const rows = within(table)
      .getAllByRole('row')
      .slice(1)
      .map(row => within(row).getAllByRole('cell').map(cell => cell.textContent));

    expect(within(table).getAllByRole('rowheader').map(header => header.querySelector('a')?.textContent)).toEqual(['redis', 'test-app']);
    expect(rows.map(cells => cells.slice(1, 4))).toEqual([
      ['$904.50', '$603.00', '$1,642.50'],
      ['$217.20', '$150.90', '$375.60'],
    ]);
    expect(within(table).getByRole('link', { name: 'redis' }).getAttribute('href')).toBe(
      '/applications/argocd/redis?view=Cost+Intelligence'
    );

    fireEvent.click(within(table).getByRole('button', { name: 'Controllers of redis' }));
    expect(within(table).getAllByRole('rowheader').map(header => header.textContent)).toContain('redis Deployment · 1 pod');
  });

  it('recommends lower CPU requests for the namespace', () => {
    render(<NamespaceDashboard />);

    const card = screen.getByRole('region', { name: 'Recommendation' });
    expect(within(card).getByRole('heading').textContent).toBe('Lower CPU requests in argocd');
    expect(card.textContent).toContain('Requesting 500m would save about $561/mo.');
  });

  it('keeps pods Argo CD cannot attribute in their own row', () => {
    vi.mocked(useArgoApplications).mockReturnValue({ applications: [], loading: false, error: null });
    render(<NamespaceDashboard />);
    const table = screen.getByRole('table', { name: 'Applications' });

    expect(within(table).getAllByRole('rowheader').map(header => header.firstElementChild?.textContent)).toEqual([
      'redis',
      'test-app',
      'Not managed by an Argo CD app',
    ]);
    expect(within(table).getAllByText('Not listed by Argo CD')).toHaveLength(2);
  });

  describe('layout', () => {
    afterEach(() => {
      vi.restoreAllMocks();
    });

    const atWidth = (width: number) =>
      vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({ width }) as DOMRect);

    // The strip holding the namespace's own figures, as opposed to the cluster's.
    const namespaceStrip = () => screen.getByText('Efficiency', { selector: 'dt' }).closest('dl') as HTMLElement;

    it('recommends a change for the selected namespace', () => {
      render(<NamespaceDashboard />);

      const card = screen.getByRole('region', { name: 'Recommendation' });
      expect(within(card).getByRole('heading').textContent).toBe('Lower CPU requests in argocd');
    });

    it('puts the namespace figures and the recommendation in one row when wide', () => {
      atWidth(1300);
      render(<NamespaceDashboard />);

      const card = screen.getByRole('region', { name: 'Recommendation' });
      expect(card.parentElement).toBe(namespaceStrip().parentElement);
      expect(card.parentElement?.children).toHaveLength(2);
    });

    it('stacks them below that width', () => {
      atWidth(1299);
      render(<NamespaceDashboard />);

      const card = screen.getByRole('region', { name: 'Recommendation' });
      expect(card.parentElement?.children.length).toBeGreaterThan(2);
      expect(namespaceStrip().nextElementSibling).toBe(card);
    });
  });

  it('opens the namespace a link names', () => {
    window.history.replaceState(null, '', '/cost-intelligence?costNs=prometheus');
    withNamespaceData({ selectedNs: 'prometheus' });

    render(<NamespaceDashboard />);

    expect(vi.mocked(useNamespaceData)).toHaveBeenCalledWith('prometheus');
    expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('prometheus');
  });

  it('puts the namespace being read into the URL', () => {
    render(<NamespaceDashboard />);

    // The page opened on its default namespace, so the address bar catches up without
    // adding a step to go Back through.
    const afterDefault = window.history.length;
    expect(new URLSearchParams(window.location.search).get('costNs')).toBe('argocd');
    expect(afterDefault).toBe(window.history.length);

    fireEvent.click(
      within(screen.getByRole('navigation', { name: 'Namespaces' })).getByRole('button', { name: /^prometheus/ })
    );

    expect(new URLSearchParams(window.location.search).get('costNs')).toBe('prometheus');
  });

  it('ranks apps by idle spend on their own tab, reached from the idle spend figure', () => {
    render(<NamespaceDashboard />);
    expect(screen.queryByText('Apps by idle spend')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'See which apps' }));

    expect(new URLSearchParams(window.location.search).get('costTab')).toBe('idle');
    expect(screen.getByRole('tab', { name: 'Idle spend' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByText('Apps by idle spend')).toBeTruthy();
  });

  it('opens the namespace an app in the idle ranking deploys to, one step back from the ranking', () => {
    window.history.replaceState(null, '', '/cost-intelligence?costTab=idle');
    const length = window.history.length;
    render(<NamespaceDashboard />);

    fireEvent.click(screen.getByRole('button', { name: 'Show namespace prometheus' }));

    expect(setSelectedNs).toHaveBeenCalledWith('prometheus');
    const params = new URLSearchParams(window.location.search);
    expect(params.get('costNs')).toBe('prometheus');
    expect(params.get('costTab')).toBeNull();
    expect(window.history.length).toBe(length + 1);
    expect(screen.getByRole('tab', { name: 'Namespaces' }).getAttribute('aria-selected')).toBe('true');
  });

  it('reloads the idle ranking with the namespaces', async () => {
    render(<NamespaceDashboard />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Reload cost data' }));
    });

    expect(reloadWaste).toHaveBeenCalled();
  });

  it('goes back to the namespace the previous step was reading', () => {
    window.history.replaceState(null, '', '/cost-intelligence?costNs=prometheus');
    withNamespaceData({ selectedNs: 'prometheus' });
    render(<NamespaceDashboard />);
    setSelectedNs.mockClear();

    // What the browser does on Back: the address changes, then the page is told about it.
    act(() => {
      window.history.replaceState(null, '', '/cost-intelligence?costNs=argocd');
      window.dispatchEvent(new PopStateEvent('popstate'));
    });

    expect(setSelectedNs).toHaveBeenCalledWith('argocd');
  });

  it('opens the view a link names', () => {
    window.history.replaceState(null, '', '/cost-intelligence?costTab=chargeback');

    render(<NamespaceDashboard />);

    expect(screen.getByRole('tab', { name: 'Chargeback' }).getAttribute('aria-selected')).toBe('true');
  });

  it('shows the chargeback controls on the Chargeback tab', () => {
    render(<NamespaceDashboard />);

    fireEvent.click(screen.getByRole('tab', { name: 'Chargeback' }));

    expect(screen.getByRole('tab', { name: 'Chargeback' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByLabelText('Label key')).toBeTruthy();
    // Every namespace on the page can be used as a filter.
    expect([...screen.getByLabelText('Namespace').querySelectorAll('option')].map(o => o.textContent)).toEqual([
      'All namespaces',
      'argocd',
      'prometheus',
    ]);
  });

  it('shows loading, error and empty states', () => {
    withNamespaceData({ namespaces: [], namespacesLoading: true });
    const { rerender } = render(<NamespaceDashboard />);
    expect(screen.getByText('Loading namespaces…')).toBeTruthy();

    withNamespaceData({ namespaces: [], namespacesError: '502: proxy error' });
    rerender(<NamespaceDashboard />);
    expect(screen.getByRole('alert').textContent).toContain('502: proxy error');

    withNamespaceData({ namespaces: [] });
    rerender(<NamespaceDashboard />);
    expect(screen.getByText(/No cost data yet/)).toBeTruthy();
  });
});
