import { describe, expect, it } from 'vitest';
import { CostData, EfficiencyMetrics, PodCost } from './api';
import {
  changeDrivers,
  changeSinceLastSync,
  costSlices,
  efficiencyLevel,
  NOT_TIED_TO_A_POD,
  previousMeasurement,
  recommendCpuRequest,
  scopeToApp,
  sharedNamespace,
} from './costInsights';

const measurement = (measuredAt: string, totalMonthlyCost: number) =>
  ({ applicationName: 'app', measuredAt, totalMonthlyCost }) as CostData;

describe('changeSinceLastSync', () => {
  const latest = measurement('2026-09-01T14:57:05Z', 488.4);

  it('compares with the most recent earlier measurement, whatever the order', () => {
    const history = [
      measurement('2026-08-20T10:00:00Z', 100),
      latest,
      measurement('2026-09-01T14:48:42Z', 3525.9),
    ];

    const change = changeSinceLastSync(latest, history);

    expect(change?.previous).toBe(3525.9);
    expect(change?.fraction).toBeCloseTo(-0.8615, 4);
  });

  it('returns null for a first measurement', () => {
    expect(changeSinceLastSync(latest, [latest])).toBeNull();
    expect(changeSinceLastSync(latest, [])).toBeNull();
  });

  it('returns null when the previous measurement cost nothing', () => {
    expect(changeSinceLastSync(latest, [measurement('2026-08-31T00:00:00Z', 0)])).toBeNull();
  });
});

describe('sharedNamespace and scopeToApp', () => {
  const pod = (controller: string, totalCost: number, name = `${controller}-pod`): PodCost => ({
    name,
    namespace: 'argocd',
    controller,
    controllerKind: 'deployment',
    cpuCost: totalCost,
    ramCost: 0,
    totalCost,
  });
  const record = (pods: PodCost[] | undefined, extra: Partial<CostData> = {}): CostData => ({
    applicationName: 'redis',
    measuredAt: '2026-10-02T13:07:37Z',
    totalMonthlyCost: 9.55,
    pods,
    ...extra,
  });
  const namespacePods = [pod('argocd-redis', 7.4), pod('redis', 0.4), pod('redis', 0.23, 'redis-pod-2'), pod('argocd-server', 0.13)];

  it('reports the namespace and the controllers the application does not manage', () => {
    // The record's deployment namespace wins over the pods' when both are known.
    expect(sharedNamespace(record(namespacePods, { deploymentNamespace: 'platform' }), new Set(['redis']))).toEqual({
      namespace: 'platform',
      cost: 9.55,
      otherWorkloads: 2,
    });
  });

  it("falls back to the pods' namespace when the record does not name one", () => {
    expect(sharedNamespace(record(namespacePods), new Set(['redis']))?.namespace).toBe('argocd');
  });

  it('is null when the application has the namespace to itself, ignoring rounding noise', () => {
    expect(sharedNamespace(record([pod('redis', 0.63), pod('leftover', 0.0004)]), new Set(['redis']))).toBeNull();
  });

  it('is null when ownership is unknown', () => {
    expect(sharedNamespace(record(namespacePods), new Set())).toBeNull();
    expect(sharedNamespace(record(namespacePods), undefined)).toBeNull();
    expect(sharedNamespace(record(undefined), new Set(['redis']))).toBeNull();
  });

  it("narrows a measurement to the application's pods, matching a bare pod by its name", () => {
    const bare = { ...pod('', 0.05), name: 'debug' };
    const scoped = scopeToApp(record([...namespacePods, bare]), new Set(['redis', 'debug']));

    expect(scoped?.pods?.map(p => p.name)).toEqual(['redis-pod', 'redis-pod-2', 'debug']);
    expect(scoped?.totalMonthlyCost).toBeCloseTo(0.68, 10);
    expect(scopeToApp(record(undefined), new Set(['redis']))).toBeNull();
  });
});

describe('previousMeasurement', () => {
  it('finds the latest record before the given one, ignoring the record itself and later ones', () => {
    const latest = measurement('2026-09-01T14:57:05Z', 488.4);
    const previous = measurement('2026-09-01T14:48:42Z', 3525.9);
    const history = [measurement('2026-09-02T00:00:00Z', 1), latest, measurement('2026-08-20T10:00:00Z', 100), previous];

    expect(previousMeasurement(latest, history)).toBe(previous);
    expect(previousMeasurement(latest, [latest])).toBeNull();
  });
});

describe('changeDrivers', () => {
  const pod = (controller: string, totalCost: number, controllerKind = 'deployment', name = `${controller}-pod`): PodCost => ({
    name,
    namespace: 'shop',
    controller,
    controllerKind,
    cpuCost: totalCost,
    ramCost: 0,
    totalCost,
  });
  const record = (measuredAt: string, pods: PodCost[] | undefined, rest = 0): CostData => ({
    applicationName: 'shop',
    measuredAt,
    totalMonthlyCost: (pods ?? []).reduce((sum, p) => sum + p.totalCost, 0) + rest,
    pods,
  });
  const before = (pods: PodCost[] | undefined, rest = 0) => record('2026-09-01T10:00:00Z', pods, rest);
  const after = (pods: PodCost[] | undefined, rest = 0) => record('2026-09-01T12:00:00Z', pods, rest);
  const sum = (result: NonNullable<ReturnType<typeof changeDrivers>>) =>
    result.drivers.reduce((total, driver) => total + driver.delta, 0) + result.smaller;

  it('names added, removed and changed controllers, largest change first', () => {
    const result = changeDrivers(
      after([pod('api', 100), pod('worker', 90), pod('worker', 51, 'deployment', 'worker-2')]),
      before([pod('api', 120), pod('cron', 30, 'cronjob'), pod('worker', 60)])
    );

    expect(result?.drivers.map(d => [d.key, d.status, d.delta])).toEqual([
      ['deployment/worker', 'changed', 81],
      ['cronjob/cron', 'removed', -30],
      ['deployment/api', 'changed', -20],
    ]);
    expect(result?.drivers[0]).toMatchObject({ name: 'worker', kind: 'deployment', before: 60, after: 141 });
    expect(result?.total).toBe(31);
    expect(result?.smallerCount).toBe(0);
  });

  it('marks a controller that was not there before as added', () => {
    const result = changeDrivers(after([pod('api', 100), pod('cache', 40, 'statefulset')]), before([pod('api', 100)]));

    expect(result?.drivers).toEqual([
      expect.objectContaining({ key: 'statefulset/cache', status: 'added', before: 0, after: 40, delta: 40 }),
    ]);
  });

  it('keeps same-named controllers of different kinds apart', () => {
    const result = changeDrivers(after([pod('web', 50, 'deployment')]), before([pod('web', 50, 'statefulset')]));

    expect(result?.drivers.map(d => [d.key, d.status])).toEqual([
      ['deployment/web', 'added'],
      ['statefulset/web', 'removed'],
    ]);
  });

  it('accounts for the cost no pod carries, so the entries add up to the total', () => {
    const result = changeDrivers(after([pod('api', 100)], 25), before([pod('api', 100)], 5));

    expect(result?.drivers).toEqual([
      expect.objectContaining({ key: NOT_TIED_TO_A_POD, status: null, before: 5, after: 25, delta: 20 }),
    ]);
    expect(result?.total).toBe(20);
  });

  it('lists a change of $1 or 2% of the namespace and folds anything below both into smaller changes', () => {
    // The namespace costs $100 after the sync, so 2% is $2 and the $1 threshold is the lower one.
    const result = changeDrivers(
      after([pod('a', 31), pod('b', 20.99), pod('c', 20), pod('d', 28.01)]),
      before([pod('a', 30), pod('b', 20), pod('c', 20.5), pod('d', 28)])
    );

    expect(result?.drivers.map(d => d.key)).toEqual(['deployment/a']);
    expect(result?.smallerCount).toBe(3);
    expect(result?.smaller).toBeCloseTo(0.5, 6);
    expect(sum(result!)).toBeCloseTo(result!.total, 6);
  });

  it('lists a change under $1 when it is at least 2% of a small namespace', () => {
    // $20 namespace: 2% is $0.40, so a $0.40 change is listed and a $0.39 one is not.
    const result = changeDrivers(after([pod('a', 10.4), pod('b', 9.6)]), before([pod('a', 10), pod('b', 9.99)]));

    expect(result?.drivers.map(d => d.key)).toEqual(['deployment/a']);
    expect(result?.smallerCount).toBe(1);
  });

  it('lists at most five changes and adds the rest to smaller changes', () => {
    const names = ['a', 'b', 'c', 'd', 'e', 'f', 'g'];
    const result = changeDrivers(
      after(names.map((n, i) => pod(n, 100 + (i + 1) * 10))),
      before(names.map(n => pod(n, 100)))
    );

    expect(result?.drivers.map(d => d.name)).toEqual(['g', 'f', 'e', 'd', 'c']);
    expect(result?.smallerCount).toBe(2);
    expect(result?.smaller).toBeCloseTo(30, 6);
    expect(sum(result!)).toBeCloseTo(result!.total, 6);
  });

  it('cannot explain a change when either measurement has no pods', () => {
    expect(changeDrivers(after(undefined, 50), before([pod('api', 100)]))).toBeNull();
    expect(changeDrivers(after([pod('api', 100)]), before(undefined, 50))).toBeNull();
  });

  it('lists nothing when no controller moved', () => {
    const result = changeDrivers(after([pod('api', 100)]), before([pod('api', 100)]));

    expect(result).toEqual({ drivers: [], smaller: 0, smallerCount: 0, total: 0 });
  });
});

describe('efficiencyLevel', () => {
  it.each([
    [1.2, 'over-request'],
    [1, 'healthy'],
    [0.8, 'healthy'],
    [0.7, 'moderate'],
    [0.5, 'moderate'],
    [0.4, 'over-provisioned'],
  ])('%s is %s', (ratio, want) => {
    expect(efficiencyLevel(ratio)).toBe(want);
  });
});

describe('costSlices', () => {
  const totals = {
    cpuCost: 325.5,
    memoryCost: 150.9,
    storageCost: 3,
    gpuCost: 0,
    networkCost: 9,
    networkCrossZoneCost: 6,
    networkInternetCost: 0,
    loadBalancerCost: 0,
    totalCost: 488.4,
  };

  it('lists non-zero costs in a fixed order with cloud services after network', () => {
    const data = {
      opencostOutput: { allocationTotals: totals },
      cloudEstimate: { source: 'infracost', totalMonthlyCost: 47.2, currency: 'USD', resources: [], pushedAt: '' },
    } as unknown as CostData;

    const slices = costSlices(data);

    expect(slices.map(slice => [slice.id, slice.value, slice.slot])).toEqual([
      ['cpu', 325.5, 0],
      ['memory', 150.9, 1],
      ['storage', 3, 2],
      ['network', 9, 3],
      ['cloud', 47.2, 4],
    ]);
    expect(slices[3].children).toEqual([{ label: 'Cross-zone', value: 6 }]);
  });

  it('keeps each cost on its own colour slot when others are missing', () => {
    const data = { opencostOutput: { allocationTotals: { ...totals, memoryCost: 0, gpuCost: 2 } } } as unknown as CostData;

    expect(costSlices(data).map(slice => [slice.id, slice.slot])).toEqual([
      ['cpu', 0],
      ['storage', 2],
      ['network', 3],
      ['gpu', 6],
    ]);
  });

  it('returns nothing without an allocation or cloud estimate', () => {
    expect(costSlices({} as CostData)).toEqual([]);
  });
});

describe('recommendCpuRequest', () => {
  const efficiency: EfficiencyMetrics = {
    cpuEfficiency: 0.4,
    ramEfficiency: 0.7,
    totalEfficiency: 0.55,
    cpuCoresRequested: 1,
    cpuCoresUsed: 0.4,
    ramBytesRequested: 536870912,
    ramBytesUsed: 375809638,
    cpuIdleCost: 195.3,
    ramIdleCost: 45.27,
    cpuCoresLimit: 2,
    ramBytesLimit: 1073741824,
    cpuCoresUsedMax: 0,
    ramBytesUsedMax: 0,
  };

  it('suggests 25% headroom over average use when peaks are not collected', () => {
    const recommendation = recommendCpuRequest(efficiency, 325.5);

    expect(recommendation).toMatchObject({ requestedCores: 1, averageCores: 0.4, peakCores: null, suggestedCores: 0.5 });
    expect(recommendation?.monthlySavings).toBeCloseTo(162.75);
  });

  it('sizes from the peak when it is known, rounding up to 50m', () => {
    const recommendation = recommendCpuRequest({ ...efficiency, cpuCoresUsedMax: 0.7 }, 325.5);

    expect(recommendation?.peakCores).toBe(0.7);
    expect(recommendation?.suggestedCores).toBe(0.9);
    expect(recommendation?.monthlySavings).toBeCloseTo(32.55);
  });

  it('suggests at least 50m for idle pods', () => {
    expect(recommendCpuRequest({ ...efficiency, cpuEfficiency: 0, cpuCoresUsed: 0 }, 325.5)?.suggestedCores).toBe(0.05);
  });

  it('stays quiet unless usage is below half the request', () => {
    expect(recommendCpuRequest({ ...efficiency, cpuEfficiency: 0.6, cpuCoresUsed: 0.6 }, 325.5)).toBeNull();
  });

  it('stays quiet when headroom over the peak would not lower the request', () => {
    expect(recommendCpuRequest({ ...efficiency, cpuEfficiency: 0.1, cpuCoresUsed: 0.1, cpuCoresUsedMax: 0.9 }, 325.5)).toBeNull();
  });

  it('stays quiet when the saving is under a dollar a month, or data is missing', () => {
    expect(recommendCpuRequest(efficiency, 1)).toBeNull();
    expect(recommendCpuRequest(undefined, 325.5)).toBeNull();
    expect(recommendCpuRequest({ ...efficiency, cpuCoresRequested: 0 }, 325.5)).toBeNull();
  });
});
