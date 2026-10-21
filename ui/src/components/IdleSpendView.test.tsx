import React from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ClusterIdle, IdleSpendView } from './IdleSpendView';
import { UseWasteRankingResult } from '../hooks/useWasteRanking';
import { AppWaste, EfficiencyMetrics, WasteRanking } from '../utils/api';

const efficiency = (cpuEfficiency: number): EfficiencyMetrics => ({
  cpuEfficiency,
  ramEfficiency: 0.5,
  totalEfficiency: 0.4,
  cpuCoresRequested: 2,
  cpuCoresUsed: 2 * cpuEfficiency,
  ramBytesRequested: 1024,
  ramBytesUsed: 512,
  cpuIdleCost: 0,
  ramIdleCost: 0,
  cpuCoresLimit: 0,
  ramBytesLimit: 0,
  cpuCoresUsedMax: 0,
  ramBytesUsedMax: 0,
});

// At 20% of 2 cores, the app's tab suggests 0.5 cores: a saving of 3/4 of the $200 CPU cost.
const app = (applicationName: string, deploymentNamespace: string, idleCost: number, cpuEfficiency = 0.2): AppWaste => ({
  applicationNamespace: 'argocd',
  applicationName,
  deploymentNamespace,
  measuredAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
  monthlyCost: 400,
  cpuCost: 200,
  idleCost,
  efficiency: efficiency(cpuEfficiency),
});

const apps = [app('web', 'shop', 100), app('api', 'shop', 80, 0.9), app('ledger', 'billing', 50)];

const waste = (ranking: Partial<WasteRanking> | null, overrides: Partial<UseWasteRankingResult> = {}): UseWasteRankingResult => ({
  ranking: ranking && { apps: [], idleCost: 0, namespaceOnly: 0, noPods: 0, ...ranking },
  loading: false,
  error: null,
  reload: vi.fn(() => Promise.resolve()),
  ...overrides,
});

const allMeasured: ClusterIdle = { idle: 412.5, measured: 2, namespaces: 2 };

const view = (result: UseWasteRankingResult, clusterIdle: ClusterIdle | null = allMeasured) =>
  render(<IdleSpendView waste={result} clusterIdle={clusterIdle} onSelectNamespace={vi.fn()} />);

// stats reads the strip as label → value and detail.
const stats = () =>
  Object.fromEntries(
    Array.from(document.querySelectorAll('dl > div')).map(stat => {
      const [label, ...rest] = Array.from(stat.children).map(child => child.textContent);
      return [label, rest];
    })
  );

const rankedApps = () =>
  within(screen.getByRole('table'))
    .getAllByRole('link')
    .map(link => link.textContent);

describe('IdleSpendView', () => {
  afterEach(() => {
    window.history.replaceState(null, '', '/');
  });

  it('sums the ranking above it, beside the cluster idle spend', () => {
    view(waste({ apps, namespaceOnly: 2, noPods: 1 }));

    expect(stats()).toEqual({
      "Apps' idle spend": ['$230.00', 'In the pods of 3 ranked apps'],
      'Lower CPU requests': ['~$300', 'Suggested for 2 apps'],
      'Not ranked': ['3', '2 measured only as their whole namespace, until their next sync, and 1 with no pods'],
      'Cluster idle spend': ['$412.50', 'Including pods no app owns'],
    });
  });

  it('says when nothing is left out, suggested or measured', () => {
    view(waste({ apps: [app('api', 'shop', 80, 0.9)] }), null);

    expect(stats()['Lower CPU requests']).toEqual(['—', 'No app has one to suggest']);
    expect(stats()['Not ranked']).toEqual(['0', 'Every app measured is ranked']);
    expect(stats()['Cluster idle spend']).toEqual(['—', "Not measured yet. Measured at each namespace's next sync"]);
  });

  it('says when the cluster idle spend covers only some namespaces', () => {
    view(waste({ apps }), { idle: 120, measured: 1, namespaces: 3 });

    expect(stats()['Cluster idle spend']).toEqual(['$120.00', 'In the 1 of 3 namespaces measured']);
  });

  it('narrows the ranking, not the figures above it, to one namespace', () => {
    view(waste({ apps }));

    fireEvent.change(screen.getByLabelText('Namespace'), { target: { value: 'shop' } });

    expect(rankedApps()).toEqual(['web', 'api']);
    expect(stats()["Apps' idle spend"][0]).toBe('$230.00');
    expect(screen.getByRole('status').textContent).toBe('2 apps deploying to shop');
    expect(new URLSearchParams(window.location.search).get('costScope')).toBe('shop');
  });

  it('opens on the namespace a link names, even once its apps are gone', () => {
    window.history.replaceState(null, '', '/?costScope=gone');
    view(waste({ apps }));

    const select = screen.getByLabelText('Namespace') as HTMLSelectElement;
    expect(select.value).toBe('gone');
    expect(Array.from(select.options).map(option => option.value)).toEqual(['', 'billing', 'shop', 'gone']);
    expect(screen.queryByRole('table')).toBeNull();
    expect(document.body.textContent).toContain('No ranked app deploys to gone.');
  });

  it('offers no filter when every app deploys to one namespace', () => {
    view(waste({ apps: apps.slice(0, 2) }));

    expect(screen.queryByLabelText('Namespace')).toBeNull();
  });

  it('shows an error in place of the ranking', () => {
    const error = "Couldn't list the Argo CD applications, so apps on other clusters can't be left out.";
    view(waste({ apps }, { error }));

    expect(screen.getByRole('alert').textContent).toContain(error);
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('says it is loading until the first ranking arrives', () => {
    view(waste(null, { loading: true }));

    expect(document.body.textContent).toContain('Loading the ranking…');
  });
});
