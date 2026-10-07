import { describe, expect, it } from 'vitest';
import { PodCost } from './api';
import { groupPodsByController } from './podGroups';

const pod = (name: string, controller: string, controllerKind: string, totalCost: number): PodCost => ({
  name,
  namespace: 'shop',
  controller,
  controllerKind,
  cpuCost: totalCost * 0.6,
  ramCost: totalCost * 0.4,
  totalCost,
});

describe('groupPodsByController', () => {
  it('groups pods by controller, most expensive controller and pod first', () => {
    const groups = groupPodsByController([
      pod('web-1', 'web', 'Deployment', 10),
      pod('db-0', 'db', 'StatefulSet', 40),
      pod('web-2', 'web', 'Deployment', 20),
    ]);

    expect(groups.map(group => group.key)).toEqual(['StatefulSet/db', 'Deployment/web']);
    expect(groups[1].pods.map(p => p.name)).toEqual(['web-2', 'web-1']);
    expect(groups[1].totalCost).toBe(30);
    expect(groups[1].cpuCost).toBeCloseTo(18);
    expect(groups[1].ramCost).toBeCloseTo(12);
  });

  it('keeps same-named controllers of different kinds apart', () => {
    const groups = groupPodsByController([pod('a-1', 'a', 'Deployment', 5), pod('a-0', 'a', 'StatefulSet', 4)]);

    expect(groups.map(group => group.key)).toEqual(['Deployment/a', 'StatefulSet/a']);
  });

  it('treats a pod without a controller as its own group of kind Pod', () => {
    const [group] = groupPodsByController([pod('debug', '', '', 2)]);

    expect(group).toMatchObject({ key: 'Pod/debug', kind: 'Pod', name: 'debug' });
  });

  it('leaves out pods that cost less than a tenth of a cent', () => {
    const groups = groupPodsByController([pod('idle', 'x', 'Job', 0.0005), pod('run', 'y', 'Job', 1)]);

    expect(groups.map(group => group.name)).toEqual(['y']);
  });

  it("computes each controller's share of the listed total", () => {
    const groups = groupPodsByController([pod('a-0', 'a', 'Deployment', 40), pod('b-0', 'b', 'Deployment', 10)]);

    expect(groups.map(group => group.share)).toEqual([0.8, 0.2]);
  });

  it('returns no groups for no pods', () => {
    expect(groupPodsByController([])).toEqual([]);
  });
});
