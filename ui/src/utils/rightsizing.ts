import { ContainerRightsizing, Rightsizing, WorkloadRightsizing } from './api';
import { formatMoney } from './format';

const MI = 1024 ** 2;
const GI = 1024 ** 3;

// MIN_MONTHLY_CHANGE is the smallest saving or increase worth a recommendation card, as for the CPU
// suggestion.
const MIN_MONTHLY_CHANGE = 1;

// formatCpu writes CPU the way a manifest would: "120m" under a core, "1.38 cores" above.
export function formatCpu(cores: number): string {
  if (cores < 1) return `${Math.round(cores * 1000)}m`;
  const rounded = Number(cores.toFixed(2));
  return `${rounded} ${rounded === 1 ? 'core' : 'cores'}`;
}

// formatMemory writes memory in Kubernetes units: "384Mi", or "1Gi" / "1.5Gi" for whole and half GiB.
export function formatMemory(bytes: number): string {
  if (bytes >= GI && (bytes / GI) % 0.5 === 0) return `${bytes / GI}Gi`;
  return `${Math.round(bytes / MI)}Mi`;
}

// formatMonthlyChange signs a monthly change: "−$37.12/mo" saves, "+$14.00/mo" costs.
export function formatMonthlyChange(amount: number): string {
  return `${amount < 0 ? '−' : '+'}${formatMoney(Math.abs(amount))}/mo`;
}

export const containerChange = (c: ContainerRightsizing) => c.monthlyChange.cpu + c.monthlyChange.memory;

// recommendedWorkloads are the workloads a recommendation-only VPA has advice for.
export const recommendedWorkloads = (r: Rightsizing | null | undefined): WorkloadRightsizing[] =>
  r?.available ? r.workloads.filter(w => w.status === 'recommended' && w.containers.length > 0) : [];

// hasAdvice is whether the VPA's recommendations are worth a card: they save or cost at least a
// dollar a month.
export function hasAdvice(r: Rightsizing | null | undefined): boolean {
  return (
    recommendedWorkloads(r).length > 0 &&
    (r!.monthlySaving >= MIN_MONTHLY_CHANGE || r!.monthlyIncrease >= MIN_MONTHLY_CHANGE)
  );
}

// hasVpa is whether any of the application's workloads has a VPA, which is when the Resources tab
// shows the rightsizing section. Without one it stays silent.
export const hasVpa = (r: Rightsizing | null | undefined): boolean =>
  Boolean(r?.available && r.workloads.some(w => w.status !== 'none'));

export interface AdviceSummary {
  // lower names the workloads with a saving, raise those whose requests should go up.
  lower: string[];
  raise: string[];
  saving: number;
  increase: number;
  lowConfidence: boolean;
}

// summarize says which workloads the VPA would lower and which it would raise.
export function summarize(r: Rightsizing): AdviceSummary {
  const workloads = recommendedWorkloads(r);
  const parts = (w: WorkloadRightsizing) => w.containers.flatMap(c => [c.monthlyChange.cpu, c.monthlyChange.memory]);
  return {
    lower: workloads.filter(w => parts(w).some(part => part <= -0.005)).map(w => w.name),
    raise: workloads.filter(w => parts(w).some(part => part >= 0.005)).map(w => w.name),
    saving: r.monthlySaving,
    increase: r.monthlyIncrease,
    lowConfidence: workloads.some(w => w.lowConfidence),
  };
}

// listNames joins names into a sentence: "web", "web and worker", "web, worker and 2 more".
export function listNames(names: string[]): string {
  if (names.length <= 1) return names.join('');
  if (names.length === 2) return `${names[0]} and ${names[1]}`;
  if (names.length === 3) return `${names[0]}, ${names[1]} and ${names[2]}`;
  return `${names[0]}, ${names[1]} and ${names.length - 2} more`;
}

// Where a container's current request sits against the VPA's range.
export type RangePosition = 'above' | 'within' | 'below';

export function rangePosition(request: number, lower: number, upper: number): RangePosition {
  if (request > upper) return 'above';
  if (request < lower) return 'below';
  return 'within';
}

// memoryLimitTooLow is whether a container's memory limit is under the VPA's upper bound, the most it
// expects the container to need: below it, the container risks being killed for memory. An unsettled
// upper bound is not yet a reason to raise a limit.
export const memoryLimitTooLow = (c: ContainerRightsizing) =>
  !c.unsettled && c.memoryLimitBytes > 0 && c.memoryLimitBytes < c.upperBound.memoryBytes;

// minimumMemoryLimit is the VPA's upper bound rounded up to 16Mi, a limit that leaves it room.
export const minimumMemoryLimit = (c: ContainerRightsizing) => Math.ceil(c.upperBound.memoryBytes / (16 * MI)) * 16 * MI;

// requestsYaml is the resources stanza to paste into a pod template for the given containers.
export function requestsYaml(containers: ContainerRightsizing[]): string {
  return containers
    .map(c =>
      [
        `- name: ${c.name}`,
        '  resources:',
        '    requests:',
        `      cpu: ${Math.round(c.suggested.cpuCores * 1000)}m`,
        `      memory: ${Math.round(c.suggested.memoryBytes / MI)}Mi`,
      ].join('\n')
    )
    .join('\n');
}
