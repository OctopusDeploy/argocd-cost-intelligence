import { CostData, EfficiencyMetrics, PodCost } from './api';
import { ControllerGroup, groupPodsByController } from './podGroups';

export interface CostChange {
  // previous is the Kubernetes monthly cost at the sync before the latest one.
  previous: number;
  // fraction is the signed relative change, e.g. -0.86 for a drop of 86%.
  fraction: number;
}

// previousMeasurement finds the most recent measurement taken before `latest`. The API returns
// history newest first and it can include the latest record itself, so the lookup goes by timestamp.
export function previousMeasurement(latest: CostData, history: CostData[]): CostData | null {
  const latestTime = Date.parse(latest.measuredAt);
  let previous: CostData | null = null;
  let previousTime = -Infinity;
  for (const record of history) {
    const time = Date.parse(record.measuredAt);
    if (time < latestTime && time > previousTime) {
      previous = record;
      previousTime = time;
    }
  }
  return previous;
}

// changeSinceLastSync compares the latest measurement with the previous one. Returns null without an
// earlier, non-zero measurement.
export function changeSinceLastSync(latest: CostData, history: CostData[]): CostChange | null {
  return costChange(latest, previousMeasurement(latest, history));
}

// costChange compares two measurements. Returns null without a previous, non-zero measurement.
export function costChange(latest: CostData, previous: CostData | null): CostChange | null {
  if (!previous || previous.totalMonthlyCost <= 0) return null;
  return {
    previous: previous.totalMonthlyCost,
    fraction: (latest.totalMonthlyCost - previous.totalMonthlyCost) / previous.totalMonthlyCost,
  };
}

// SharedNamespace describes the deployment namespace of an application that shares it with workloads
// the application does not manage.
export interface SharedNamespace {
  namespace: string;
  // cost is the whole namespace's Kubernetes monthly cost.
  cost: number;
  // otherWorkloads counts the controllers in the namespace that the application does not manage.
  otherWorkloads: number;
}

const ownsPod = (managedControllers: Set<string>, pod: PodCost) => managedControllers.has(pod.controller || pod.name);

// sharedNamespace reports whether the latest measurement has pods that none of the application's
// controllers own. Null when the application has its namespace to itself, or when ownership is unknown
// (Argo CD lists no controllers for it, or the record has no pods): the namespace figures are then the
// application's own.
export function sharedNamespace(data: CostData, managedControllers?: Set<string>): SharedNamespace | null {
  if (!managedControllers || managedControllers.size === 0 || !data.pods?.length) return null;
  const others = groupPodsByController(data.pods).filter(group => !managedControllers.has(group.name));
  if (others.length === 0) return null;
  return {
    namespace: data.deploymentNamespace || data.pods[0].namespace,
    cost: data.totalMonthlyCost,
    otherWorkloads: others.length,
  };
}

// asApplication returns what a measurement says about the application alone. A record measured for
// the application already is. An older one measured its whole namespace: that is the application's own
// cost when nothing else ran there, and is otherwise narrowed to the pods of the controllers Argo CD
// lists for it. Null when ownership is unknown (Argo CD lists no controllers) for an older record.
export function asApplication(record: CostData, managedControllers?: Set<string>): CostData | null {
  if (record.scope === 'application') return record;
  if (!managedControllers || managedControllers.size === 0) return null;
  return sharedNamespace(record, managedControllers) ? scopeToApp(record, managedControllers) : record;
}

// scopeToApp narrows a measurement to the pods the application's controllers own, so its total is
// the application's cost rather than the namespace's. Null for a record without pods, which cannot be
// narrowed.
export function scopeToApp(data: CostData, managedControllers: Set<string>): CostData | null {
  if (!data.pods) return null;
  const pods = data.pods.filter(pod => ownsPod(managedControllers, pod));
  return { ...data, pods, totalMonthlyCost: pods.reduce((sum, pod) => sum + pod.totalCost, 0) };
}

export interface ChangeDriver {
  // key is the controller's `${kind}/${name}`, or NOT_TIED_TO_A_POD for the rest of the namespace cost.
  key: string;
  kind: string;
  name: string;
  // added: no pods in the previous measurement; removed: none in the latest one. Null for the rest
  // of the namespace cost.
  status: 'added' | 'removed' | 'changed' | null;
  before: number;
  after: number;
  delta: number;
}

export interface ChangeDrivers {
  // drivers are the largest changes first, at most MAX_DRIVERS of them.
  drivers: ChangeDriver[];
  // smaller is the sum of the changes not listed, and smallerCount how many there are.
  smaller: number;
  smallerCount: number;
  // total is the change in the namespace's monthly cost; drivers and smaller add up to it.
  total: number;
}

export const NOT_TIED_TO_A_POD = 'not-tied-to-a-pod';
export const MAX_DRIVERS = 5;
// A change is listed when it reaches either threshold: $1 a month, or 2% of the namespace's cost.
const MIN_DRIVER_AMOUNT = 1;
const MIN_DRIVER_SHARE = 0.02;
// Differences below this are floating-point noise, not a change worth counting.
const HALF_A_CENT = 0.005;

// changeDrivers explains the change in a namespace's monthly cost between two measurements by
// controller. Whatever OpenCost does not attribute to a pod (unmounted volumes, and the difference
// between its namespace and pod figures) is one more entry, so the entries add up to the total.
// Returns null when either measurement has no pods, since an older record or a failed pod query
// would otherwise read as every controller being removed or added.
export function changeDrivers(latest: CostData, previous: CostData): ChangeDrivers | null {
  if (!latest.pods || !previous.pods) return null;

  const before = new Map(groupPodsByController(previous.pods).map(group => [group.key, group]));
  const after = new Map(groupPodsByController(latest.pods).map(group => [group.key, group]));
  const all: ChangeDriver[] = [];

  for (const key of new Set([...before.keys(), ...after.keys()])) {
    const was = before.get(key);
    const now = after.get(key);
    const group = (now ?? was) as ControllerGroup;
    const beforeCost = was?.totalCost ?? 0;
    const afterCost = now?.totalCost ?? 0;
    all.push({
      key,
      kind: group.kind,
      name: group.name,
      status: !was ? 'added' : !now ? 'removed' : 'changed',
      before: beforeCost,
      after: afterCost,
      delta: afterCost - beforeCost,
    });
  }

  const podTotal = (groups: Map<string, ControllerGroup>) => [...groups.values()].reduce((sum, g) => sum + g.totalCost, 0);
  const restBefore = previous.totalMonthlyCost - podTotal(before);
  const restAfter = latest.totalMonthlyCost - podTotal(after);
  all.push({
    key: NOT_TIED_TO_A_POD,
    kind: '',
    name: 'Not tied to a pod',
    status: null,
    before: restBefore,
    after: restAfter,
    delta: restAfter - restBefore,
  });

  // A namespace that dropped to nothing has no share threshold, only the amount.
  const minShare = latest.totalMonthlyCost * MIN_DRIVER_SHARE;
  const significant = (delta: number) =>
    Math.abs(delta) >= MIN_DRIVER_AMOUNT || (minShare > 0 && Math.abs(delta) >= minShare);
  const listed = all
    .filter(driver => significant(driver.delta))
    .sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta) || a.key.localeCompare(b.key))
    .slice(0, MAX_DRIVERS);
  const listedKeys = new Set(listed.map(driver => driver.key));
  const unlisted = all.filter(driver => !listedKeys.has(driver.key) && Math.abs(driver.delta) >= HALF_A_CENT);

  // The entries sum to the total by construction, so the rest is whatever the listed ones leave.
  const total = latest.totalMonthlyCost - previous.totalMonthlyCost;
  return {
    drivers: listed,
    smaller: total - listed.reduce((sum, driver) => sum + driver.delta, 0),
    smallerCount: unlisted.length,
    total,
  };
}

export type EfficiencyLevel ='over-request' | 'healthy' | 'moderate' | 'over-provisioned';

// efficiencyLevel classifies usage as a fraction of the request. Above 100% risks throttling or
// eviction, 80% and up is healthy, 50% and up is moderate, and anything lower is over-provisioned.
export function efficiencyLevel(ratio: number): EfficiencyLevel {
  if (ratio > 1) return 'over-request';
  if (ratio >= 0.8) return 'healthy';
  if (ratio >= 0.5) return 'moderate';
  return 'over-provisioned';
}

export interface CostSlice {
  id: 'cpu' | 'memory' | 'storage' | 'network' | 'cloud' | 'loadBalancer' | 'gpu';
  label: string;
  value: number;
  // slot is a fixed index into the categorical palette, so each cost keeps its colour.
  slot: number;
  children?: Array<{ label: string; value: number }>;
}

// costSlices lists the non-zero parts of the monthly cost in a fixed order: the OpenCost
// allocation, with the cloud services estimate after network when one has been pushed.
export function costSlices(data: CostData): CostSlice[] {
  const totals = data.opencostOutput?.allocationTotals;
  const networkParts = [
    { label: 'Cross-zone', value: totals?.networkCrossZoneCost ?? 0 },
    { label: 'Cross-region', value: totals?.networkCrossRegionCost ?? 0 },
    { label: 'Internet', value: totals?.networkInternetCost ?? 0 },
    { label: 'NAT gateway', value: totals?.networkNatGatewayCost ?? 0 },
  ].filter(part => part.value > 0);

  const slices: CostSlice[] = [
    { id: 'cpu', label: 'CPU', value: totals?.cpuCost ?? 0, slot: 0 },
    { id: 'memory', label: 'Memory', value: totals?.memoryCost ?? 0, slot: 1 },
    { id: 'storage', label: 'Storage', value: totals?.storageCost ?? 0, slot: 2 },
    {
      id: 'network',
      label: 'Network',
      value: totals?.networkCost ?? 0,
      slot: 3,
      children: networkParts.length > 0 ? networkParts : undefined,
    },
    { id: 'cloud', label: 'Cloud services', value: data.cloudEstimate?.totalMonthlyCost ?? 0, slot: 4 },
    { id: 'loadBalancer', label: 'Load balancer', value: totals?.loadBalancerCost ?? 0, slot: 5 },
    { id: 'gpu', label: 'GPU', value: totals?.gpuCost ?? 0, slot: 6 },
  ];
  return slices.filter(slice => slice.value > 0);
}

export interface CpuRecommendation {
  requestedCores: number;
  averageCores: number;
  // peakCores is null when peak usage is not collected.
  peakCores: number | null;
  suggestedCores: number;
  monthlySavings: number;
}

const CPU_HEADROOM = 1.25;
const CPU_STEP_CORES = 0.05;
const MIN_MONTHLY_SAVINGS = 1;

// recommendCpuRequest suggests a lower CPU request when pods use less than half of what they
// request. The suggestion leaves 25% headroom above the higher of average and peak use, rounded
// up to 50m, and the saving assumes the CPU cost scales with the requested cores.
export function recommendCpuRequest(
  efficiency: EfficiencyMetrics | undefined,
  cpuCost: number
): CpuRecommendation | null {
  if (!efficiency || efficiency.cpuCoresRequested <= 0 || cpuCost <= 0) return null;
  if (efficiencyLevel(efficiency.cpuEfficiency) !== 'over-provisioned') return null;

  const requested = efficiency.cpuCoresRequested;
  const peak = efficiency.cpuCoresUsedMax > 0 ? efficiency.cpuCoresUsedMax : null;
  const basis = Math.max(efficiency.cpuCoresUsed, peak ?? 0);
  const steps = Math.max(1, Math.ceil((basis * CPU_HEADROOM) / CPU_STEP_CORES - 1e-9));
  const suggested = Number((steps * CPU_STEP_CORES).toFixed(2));
  if (suggested >= requested) return null;

  const monthlySavings = (cpuCost * (requested - suggested)) / requested;
  if (monthlySavings < MIN_MONTHLY_SAVINGS) return null;

  return {
    requestedCores: requested,
    averageCores: efficiency.cpuCoresUsed,
    peakCores: peak,
    suggestedCores: suggested,
    monthlySavings,
  };
}
