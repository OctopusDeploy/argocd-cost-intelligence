import { PodCost } from './api';

export interface ControllerGroup {
  // key is `${kind}/${name}`, so same-named controllers of different kinds stay apart.
  key: string;
  kind: string;
  name: string;
  // pods are ordered most expensive first.
  pods: PodCost[];
  cpuCost: number;
  ramCost: number;
  totalCost: number;
  // share is the group's fraction (0–1) of all listed groups, so shares add up to 100%.
  share: number;
}

// Pods cheaper than this are rounding noise in OpenCost's allocation and are left out.
const MIN_POD_COST = 0.001;

// groupPodsByController groups pods under the controller that owns them, most expensive first.
// A pod without a controller forms its own group of kind "Pod".
export function groupPodsByController(pods: PodCost[]): ControllerGroup[] {
  const groups = new Map<string, ControllerGroup>();
  for (const pod of pods) {
    if (pod.totalCost < MIN_POD_COST) continue;
    const kind = pod.controllerKind || 'Pod';
    const name = pod.controller || pod.name;
    const key = `${kind}/${name}`;
    let group = groups.get(key);
    if (!group) {
      group = { key, kind, name, pods: [], cpuCost: 0, ramCost: 0, totalCost: 0, share: 0 };
      groups.set(key, group);
    }
    group.pods.push(pod);
    group.cpuCost += pod.cpuCost;
    group.ramCost += pod.ramCost;
    group.totalCost += pod.totalCost;
  }

  const sorted = [...groups.values()].sort((a, b) => b.totalCost - a.totalCost);
  const total = sorted.reduce((sum, group) => sum + group.totalCost, 0);
  for (const group of sorted) {
    group.pods.sort((a, b) => b.totalCost - a.totalCost);
    group.share = total > 0 ? group.totalCost / total : 0;
  }
  return sorted;
}
