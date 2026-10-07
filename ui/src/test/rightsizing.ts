import { ContainerRightsizing, Rightsizing } from '../utils/api';

const MI = 1024 ** 2;

function container(
  name: string,
  requests: [number, number],
  suggested: [number, number],
  bounds: { lower: [number, number]; upper: [number, number] },
  change: [number, number],
  memoryLimitMi = 0
): ContainerRightsizing {
  const res = ([cpu, mi]: [number, number]) => ({ cpuCores: cpu, memoryBytes: mi * MI });
  return {
    name,
    requests: res(requests),
    memoryLimitBytes: memoryLimitMi * MI,
    target: res(suggested),
    lowerBound: res(bounds.lower),
    upperBound: res(bounds.upper),
    suggested: res(suggested),
    unsettled: false,
    monthlyChange: { cpu: change[0], memory: change[1] },
  };
}

// shopRightsizing is the example from the mockups: web and worker have recommendation-only VPAs, redis
// a VPA in Auto mode, nightly-report none. Lowering saves $41.31, raising worker's CPU costs $3.69.
export function shopRightsizing(overrides: Partial<Rightsizing> = {}): Rightsizing {
  return {
    available: true,
    monthlySaving: 41.31,
    monthlyIncrease: 3.69,
    workloads: [
      { kind: 'CronJob', name: 'nightly-report', status: 'none', lowConfidence: false, pods: 1, containers: [], monthlyChange: 0 },
      { kind: 'StatefulSet', name: 'redis', status: 'managed', updateMode: 'Auto', lowConfidence: false, pods: 1, containers: [], monthlyChange: 0 },
      {
        kind: 'Deployment',
        name: 'web',
        status: 'recommended',
        updateMode: 'Off',
        lowConfidence: false,
        pods: 3,
        monthlyChange: -37.21,
        containers: [
          container('web', [0.5, 1024], [0.12, 384], { lower: [0.08, 260], upper: [0.21, 640] }, [-26.27, -5.81], 512),
          container('istio-proxy', [0.1, 128], [0.03, 96], { lower: [0.016, 63], upper: [0.06, 160] }, [-4.84, -0.29], 1024),
        ],
      },
      {
        kind: 'Deployment',
        name: 'worker',
        status: 'recommended',
        updateMode: 'Off',
        lowConfidence: false,
        pods: 2,
        monthlyChange: 0.4,
        containers: [container('worker', [1, 2048], [1.38, 1504], { lower: [0.97, 1043], upper: [2.2, 1900] }, [3.69, -3.29], 2048)],
      },
    ],
    ...overrides,
  };
}
