import { describe, expect, it } from 'vitest';
import { CostData, PodCost } from './api';
import { ArgoApplication, splitNamespaceByApp } from './namespaceApps';

const pod = (name: string, controller: string, totalCost: number): PodCost => ({
  name,
  namespace: 'argocd',
  controller,
  controllerKind: 'Deployment',
  cpuCost: totalCost * 0.6,
  ramCost: totalCost * 0.4,
  totalCost,
});

const record = (applicationName: string, measuredAt: string, pods: PodCost[]): CostData =>
  ({ applicationName, applicationNamespace: 'argocd', measuredAt, totalMonthlyCost: 0, pods }) as CostData;

const argoApp = (name: string, resources: ArgoApplication['resources'], namespace = 'argocd'): ArgoApplication => ({
  name,
  namespace,
  resources,
});

const deployment = (name: string, namespace = 'argocd') => ({ kind: 'Deployment', name, namespace });

// Both apps deploy to argocd, so each record carries the same snapshot of the namespace's pods.
const snapshot = [pod('redis-66b8cb-1', 'redis', 1642.5), pod('guestbook-ui-abc-1', 'guestbook-ui', 375.6)];
const records = [record('redis', '2026-09-01T14:57:05Z', snapshot), record('test-app', '2026-09-01T14:57:05Z', snapshot)];
const argoApps = [
  argoApp('redis', [deployment('redis'), { kind: 'Service', name: 'redis', namespace: 'argocd' }]),
  argoApp('test-app', [deployment('guestbook-ui'), { kind: 'Service', name: 'guestbook-ui', namespace: 'argocd' }]),
];

describe('splitNamespaceByApp', () => {
  it('counts each pod once, under the app that manages its controller', () => {
    const { apps, totalCost } = splitNamespaceByApp('argocd', records, argoApps);

    expect(totalCost).toBeCloseTo(2018.1);
    expect(apps.map(app => [app.name, app.podCount, Number(app.totalCost.toFixed(2))])).toEqual([
      ['redis', 1, 1642.5],
      ['test-app', 1, 375.6],
    ]);
    expect(apps[0].share).toBeCloseTo(0.8139, 4);
    expect(apps[0].controllers.map(group => group.name)).toEqual(['redis']);
    expect(apps[0].namespace).toBe('argocd');
    expect(apps[0].measuredAt).toBe('2026-09-01T14:57:05Z');
  });

  it('takes the pods from the most recent measurement only', () => {
    const older = record('redis', '2026-08-30T10:00:00Z', [...snapshot, pod('old-worker-1', 'redis', 500)]);
    const { apps, latest } = splitNamespaceByApp('argocd', [older, records[1]], argoApps);

    expect(latest?.applicationName).toBe('test-app');
    expect(apps.find(app => app.name === 'redis')?.totalCost).toBeCloseTo(1642.5);
  });

  it('groups pods that no application manages last', () => {
    const withStray = [record('redis', '2026-09-01T14:57:05Z', [...snapshot, pod('debug-shell', '', 12)])];
    const { apps, totalCost } = splitNamespaceByApp('argocd', withStray, argoApps);

    expect(apps.map(app => app.name)).toEqual(['redis', null]);
    expect(apps[1].controllers.map(group => group.name)).toEqual(['guestbook-ui', 'debug-shell']);
    expect(totalCost).toBeCloseTo(2030.1);
  });

  it('lists an app Argo CD did not return with no pods, and counts its pods as unmanaged', () => {
    const { apps } = splitNamespaceByApp('argocd', records, [argoApps[0]]);

    expect(apps.map(app => [app.name, app.visible, app.podCount])).toEqual([
      ['redis', true, 1],
      ['test-app', false, 0],
      [null, true, 1],
    ]);
  });

  it('ignores resources of other kinds or namespaces', () => {
    const serviceOnly = argoApp('test-app', [
      { kind: 'Service', name: 'guestbook-ui', namespace: 'argocd' },
      deployment('guestbook-ui', 'elsewhere'),
    ]);
    const { apps } = splitNamespaceByApp('argocd', records, [argoApps[0], serviceOnly]);

    expect(apps.find(app => app.name === 'test-app')?.podCount).toBe(0);
    expect(apps.find(app => app.name === null)?.podCount).toBe(1);
  });

  it('matches applications by namespace as well as name', () => {
    const sameNameElsewhere = argoApp('test-app', [deployment('guestbook-ui')], 'tenant-a');
    const { apps } = splitNamespaceByApp('argocd', records, [argoApps[0], sameNameElsewhere]);

    expect(apps.find(app => app.name === 'test-app')?.visible).toBe(false);
  });

  it('gives a pod claimed by two applications to the first by name', () => {
    const both = [argoApp('redis', [deployment('redis')]), argoApp('test-app', [deployment('redis'), deployment('guestbook-ui')])];
    const { apps } = splitNamespaceByApp('argocd', records, both);

    expect(apps.find(app => app.name === 'redis')?.podCount).toBe(1);
    expect(apps.find(app => app.name === 'test-app')?.controllers.map(group => group.name)).toEqual(['guestbook-ui']);
  });

  it('returns nothing without records', () => {
    expect(splitNamespaceByApp('argocd', [], argoApps)).toEqual({ apps: [], totalCost: 0, latest: null });
  });
});
