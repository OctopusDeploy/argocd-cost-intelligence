import { describe, expect, it } from 'vitest';
import {
  formatCpu,
  formatMemory,
  formatMonthlyChange,
  hasAdvice,
  hasVpa,
  listNames,
  memoryLimitTooLow,
  minimumMemoryLimit,
  rangePosition,
  requestsYaml,
  summarize,
} from './rightsizing';
import { shopRightsizing } from '../test/rightsizing';

const MI = 1024 ** 2;

describe('formatting', () => {
  it('writes CPU and memory the way a manifest does', () => {
    expect(formatCpu(0.12)).toBe('120m');
    expect(formatCpu(1)).toBe('1 core');
    expect(formatCpu(1.38)).toBe('1.38 cores');
    expect(formatMemory(384 * MI)).toBe('384Mi');
    expect(formatMemory(1024 * MI)).toBe('1Gi');
    expect(formatMemory(1536 * MI)).toBe('1.5Gi');
    expect(formatMemory(1504 * MI)).toBe('1504Mi');
  });

  it('signs a monthly change', () => {
    expect(formatMonthlyChange(-37.21)).toBe('−$37.21/mo');
    expect(formatMonthlyChange(3.69)).toBe('+$3.69/mo');
  });

  it('lists names in a sentence', () => {
    expect(listNames(['web'])).toBe('web');
    expect(listNames(['web', 'worker'])).toBe('web and worker');
    expect(listNames(['web', 'worker', 'api'])).toBe('web, worker and api');
    expect(listNames(['web', 'worker', 'api', 'db'])).toBe('web, worker and 2 more');
  });
});

describe('hasAdvice and hasVpa', () => {
  it('advises when the VPA recommends a change worth a dollar a month', () => {
    expect(hasAdvice(shopRightsizing())).toBe(true);
    expect(hasAdvice(shopRightsizing({ monthlySaving: 0.4, monthlyIncrease: 0.9 }))).toBe(false);
    expect(hasAdvice(shopRightsizing({ monthlySaving: 0, monthlyIncrease: 1 }))).toBe(true);
    expect(hasAdvice(shopRightsizing({ available: false }))).toBe(false);
    expect(hasAdvice(null)).toBe(false);
  });

  it('gives no advice when no workload has a recommendation', () => {
    const rightsizing = shopRightsizing();
    rightsizing.workloads = rightsizing.workloads.filter(w => w.status !== 'recommended');
    expect(hasAdvice(rightsizing)).toBe(false);
    // redis still has a VPA.
    expect(hasVpa(rightsizing)).toBe(true);
  });

  it('gives no advice from recommendations with too little history', () => {
    const rightsizing = shopRightsizing();
    rightsizing.workloads = rightsizing.workloads.map(w => (w.status === 'recommended' ? { ...w, status: 'learning' } : w));
    expect(hasAdvice(rightsizing)).toBe(false);
    expect(hasVpa(rightsizing)).toBe(true);
  });

  it('gives no advice for a recommendation none of whose containers ran', () => {
    const rightsizing = shopRightsizing();
    rightsizing.workloads = rightsizing.workloads.map(w => ({ ...w, containers: [] }));
    expect(hasAdvice(rightsizing)).toBe(false);
  });

  it('shows the section only when a workload has a VPA', () => {
    const none = shopRightsizing();
    none.workloads = none.workloads.filter(w => w.status === 'none');
    expect(hasVpa(none)).toBe(false);
    expect(hasVpa(shopRightsizing({ available: false }))).toBe(false);
  });
});

describe('summarize', () => {
  it('names the workloads to lower and those to raise', () => {
    expect(summarize(shopRightsizing())).toEqual({
      lower: ['web', 'worker'],
      raise: ['worker'],
      saving: 41.31,
      increase: 3.69,
      lowConfidence: false,
    });
  });

  it('flags a low-confidence recommendation', () => {
    const rightsizing = shopRightsizing();
    rightsizing.workloads[3].lowConfidence = true;
    expect(summarize(rightsizing).lowConfidence).toBe(true);
  });

  it('ignores a change that rounds to nothing', () => {
    const rightsizing = shopRightsizing();
    rightsizing.workloads[2].containers.forEach(c => (c.monthlyChange = { cpu: -0.004, memory: 0.004 }));
    expect(summarize(rightsizing)).toMatchObject({ lower: ['worker'], raise: ['worker'] });
  });
});

describe('ranges and limits', () => {
  it('places a request against the range', () => {
    expect(rangePosition(0.5, 0.08, 0.21)).toBe('above');
    expect(rangePosition(0.1, 0.08, 0.21)).toBe('within');
    expect(rangePosition(0.05, 0.08, 0.21)).toBe('below');
  });

  it('warns when a memory limit is under the upper bound', () => {
    const [web, istio] = shopRightsizing().workloads[2].containers;
    expect(memoryLimitTooLow(web)).toBe(true);
    expect(minimumMemoryLimit(web)).toBe(640 * MI);
    expect(memoryLimitTooLow(istio)).toBe(false);
    expect(memoryLimitTooLow({ ...web, memoryLimitBytes: 0 })).toBe(false);
    expect(memoryLimitTooLow({ ...web, unsettled: true })).toBe(false);
    // A limit is rounded up to whole 16Mi steps.
    expect(minimumMemoryLimit({ ...web, upperBound: { ...web.upperBound, memoryBytes: 650 * MI } })).toBe(656 * MI);
  });
});

describe('requestsYaml', () => {
  it('writes the requests to paste into the pod template', () => {
    expect(requestsYaml(shopRightsizing().workloads[2].containers)).toBe(
      [
        '- name: web',
        '  resources:',
        '    requests:',
        '      cpu: 120m',
        '      memory: 384Mi',
        '- name: istio-proxy',
        '  resources:',
        '    requests:',
        '      cpu: 30m',
        '      memory: 96Mi',
      ].join('\n')
    );
  });
});
