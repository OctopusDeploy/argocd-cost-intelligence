import { describe, expect, it } from 'vitest';
import { AppWaste, EfficiencyMetrics } from './api';
import { recommendCpuRequest } from './costInsights';
import { deploymentNamespaces, notRankedDetail, wasteRow, wasteTable } from './waste';

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

const app = (name: string, idleCost: number, cpuEfficiency = 0.2): AppWaste => ({
  applicationNamespace: 'argocd',
  applicationName: name,
  deploymentNamespace: 'shop',
  measuredAt: '2026-10-20T08:00:00Z',
  monthlyCost: 400,
  cpuCost: 250,
  idleCost,
  efficiency: efficiency(cpuEfficiency),
});

describe('wasteRow', () => {
  it("prices the lower CPU request with the rule the app's tab uses", () => {
    const row = wasteRow(app('web', 100));

    expect(row.saving).toBe(recommendCpuRequest(row.efficiency, 250, row.measuredHours)?.monthlySavings);
    expect(row.saving).toBeGreaterThan(0);
    expect(row.share).toBe(0.25);
  });

  it('suggests no saving from less than a day of data, as its tab does', () => {
    expect(wasteRow({ ...app('web', 100), measuredHours: 0.05 }).saving).toBeNull();
    expect(wasteRow({ ...app('web', 100), measuredHours: 24 }).saving).toBeGreaterThan(0);
  });

  it('suggests no saving when the app uses most of its CPU request', () => {
    expect(wasteRow(app('web', 100, 0.8)).saving).toBeNull();
  });

  it('gives an app with no cost no share rather than dividing by zero', () => {
    expect(wasteRow({ ...app('web', 0), monthlyCost: 0 }).share).toBe(0);
  });
});

describe('wasteTable', () => {
  const apps = [50, 40, 30, 20, 10, 5, 1].map((idle, index) => app(`app-${index}`, idle, index < 6 ? 0.2 : 0.9));

  it('shows every app, and adds them up to the total', () => {
    const table = wasteTable(apps);

    expect(table.rows.map(row => row.applicationName)).toEqual(apps.map(a => a.applicationName));
    expect(table.total.count).toBe(7);
    expect(table.total.idleCost).toBe(156);
    // Each saving is shown in whole dollars, so the total adds up the rows as shown, not the exact sum.
    expect(table.rows.map(row => row.saving)).toEqual([187.5, 187.5, 187.5, 187.5, 187.5, 187.5, null]);
    expect(table.total.saving).toBe(1128);
  });

  it('counts only the apps with a lower request to suggest', () => {
    expect(wasteTable(apps).total.suggested).toBe(6);
  });
});

describe('deploymentNamespaces', () => {
  it('lists each namespace once, in order', () => {
    const apps = [
      { ...app('a', 1), deploymentNamespace: 'shop' },
      { ...app('b', 1), deploymentNamespace: 'argocd' },
      { ...app('c', 1), deploymentNamespace: 'shop' },
    ];

    expect(deploymentNamespaces(apps)).toEqual(['argocd', 'shop']);
  });
});

describe('notRankedDetail', () => {
  it('names both kinds of app the ranking leaves out', () => {
    expect(notRankedDetail(3, 1)).toBe('3 measured only as their whole namespace, until their next sync, and 1 with no pods');
  });

  it('agrees in number with a single app', () => {
    expect(notRankedDetail(1, 0)).toBe('1 measured only as its whole namespace, until its next sync');
    expect(notRankedDetail(0, 2)).toBe('2 with no pods');
  });

  it('says nothing when every app is ranked', () => {
    expect(notRankedDetail(0, 0)).toBeNull();
  });
});
