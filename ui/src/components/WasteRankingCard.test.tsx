import React from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { WasteRankingCard } from './WasteRankingCard';
import { AppWaste, EfficiencyMetrics } from '../utils/api';

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
const app = (applicationNamespace: string, applicationName: string, idleCost: number, cpuEfficiency = 0.2): AppWaste => ({
  applicationNamespace,
  applicationName,
  deploymentNamespace: 'shop',
  measuredAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
  monthlyCost: 400,
  cpuCost: 200,
  idleCost,
  efficiency: efficiency(cpuEfficiency),
});

const seven = [
  app('team-a', 'web', 100),
  app('team-b', 'web', 90),
  app('argocd', 'ledger', 80, 0.9),
  app('argocd', 'api', 70),
  app('argocd', 'worker', 60),
  app('argocd', 'cron', 5),
  app('argocd', 'docs', 1, 0.9),
];

const rowTexts = () =>
  within(screen.getByRole('table'))
    .getAllByRole('row')
    .slice(1)
    .map(row => Array.from(row.querySelectorAll('[role="cell"], [role="rowheader"]')).map(cell => cell.textContent));

const card = (apps: AppWaste[], props: { namespace?: string; noPods?: number; onSelectNamespace?: (ns: string) => void } = {}) =>
  render(
    <WasteRankingCard
      apps={apps}
      namespace={props.namespace ?? ''}
      noPods={props.noPods ?? 0}
      onSelectNamespace={props.onSelectNamespace ?? vi.fn()}
    />
  );

describe('WasteRankingCard', () => {
  it('ranks every app, then the total', () => {
    card(seven);

    const rows = rowTexts();
    expect(rows).toHaveLength(8);
    expect(rows[0]).toEqual(['1', 'webteam-a · deploys to shop', '$100.00', '25%', 'save ~$150', '2 hours ago']);
    expect(rows[2].slice(2, 5)).toEqual(['$80.00', '20%', '—']);
    expect(rows[6][1]).toBe('docsargocd · deploys to shop');
    expect(rows[7]).toEqual(['', 'Total, 7 apps', '$406.00', '', '~$750', '']);
  });

  it('tells two apps of the same name apart by their namespace', () => {
    card(seven);

    const links = screen.getAllByRole('link', { name: 'web' }).map(link => link.getAttribute('href'));
    expect(links).toEqual([
      '/applications/team-a/web?view=Cost+Intelligence',
      '/applications/team-b/web?view=Cost+Intelligence',
    ]);
  });

  it('selects the namespace an app deploys to', () => {
    const onSelectNamespace = vi.fn();
    card(seven.slice(0, 1), { onSelectNamespace });

    fireEvent.click(screen.getByRole('button', { name: 'Show namespace shop' }));

    expect(onSelectNamespace).toHaveBeenCalledWith('shop');
  });

  it('explains an empty ranking', () => {
    const { container } = card([]);

    expect(screen.queryByRole('table')).toBeNull();
    expect(container.textContent).toContain('No app has been measured on its own yet.');
  });

  it('says when the apps measured have no pods', () => {
    const { container } = card([], { noPods: 2 });

    expect(container.textContent).toContain('No app measured on its own has pods running.');
  });

  it('says when no ranked app deploys to the chosen namespace', () => {
    const { container } = card([], { namespace: 'shop', noPods: 2 });

    expect(container.textContent).toContain('No ranked app deploys to shop.');
  });

  it('says when no app has idle spend', () => {
    const { container } = card([app('argocd', 'web', 0), app('argocd', 'api', 0)]);

    expect(screen.queryByRole('table')).toBeNull();
    expect(container.textContent).toContain('No idle spend in the 2 apps measured on their own');
  });
});
