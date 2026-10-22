import React from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CostActual } from './CostActual';
import { useDataQuality } from '../hooks/useDataQuality';
import { CostData, CostTrend } from '../utils/api';
import { shopRightsizing } from '../test/rightsizing';

vi.mock('../hooks/useDataQuality', () => ({ useDataQuality: vi.fn() }));

const DAY_MS = 86_400_000;

function fixture(overrides: Partial<CostData> = {}): CostData {
  return {
    applicationName: 'argo-rollouts',
    totalMonthlyCost: 488.4,
    measuredAt: '2026-09-01T14:57:05Z',
    opencostOutput: {
      allocationTotals: {
        cpuCost: 325.5,
        memoryCost: 150.9,
        storageCost: 3,
        gpuCost: 0,
        networkCost: 9,
        loadBalancerCost: 0,
        totalCost: 488.4,
        carbonCost: 8.79,
      },
    },
    efficiency: {
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
    },
    pods: [
      {
        name: 'simple-deployment-abc-1',
        namespace: 'argo-rollouts',
        controller: 'simple-deployment',
        controllerKind: 'Deployment',
        cpuCost: 325.5,
        ramCost: 150.9,
        totalCost: 488.4,
      },
    ],
    cloudEstimate: {
      source: 'infracost',
      totalMonthlyCost: 47.2,
      currency: 'USD',
      resources: [
        {
          name: 'aws_s3_bucket.artifacts',
          resourceType: 'aws_s3_bucket',
          monthlyCost: 22.8,
        },
        {
          name: 'aws_sqs_queue.jobs',
          resourceType: 'aws_sqs_queue',
          monthlyCost: 14.6,
        },
        {
          name: 'aws_cloudwatch_log_group.app',
          resourceType: 'aws_cloudwatch_log_group',
          monthlyCost: 9.8,
        },
      ],
      pushedAt: new Date(Date.now() - 14 * DAY_MS).toISOString(),
    },
    ...overrides,
  };
}

// The previous measurement ran the same controller at a higher cost, so it explains the whole change.
const trendWithPrevious = (data: CostData, previous: Partial<CostData> = {}): CostTrend => ({
  applicationName: data.applicationName,
  trend: [
    data,
    {
      ...data,
      totalMonthlyCost: 3525.9,
      measuredAt: '2026-09-01T14:48:42Z',
      pods: (data.pods ?? []).map(pod => ({ ...pod, totalCost: 3525.9 })),
      ...previous,
    },
  ],
  statistics: { average: 0, maximum: 0, minimum: 0, total: 0 },
  count: 2,
  days: 30,
});

const pod = (controller: string, totalCost: number) => ({
  name: `${controller}-abc-1`,
  namespace: 'argo-rollouts',
  controller,
  controllerKind: 'deployment',
  cpuCost: totalCost,
  ramCost: 0,
  totalCost,
});

const stat = (label: string) => {
  const term = screen.getByText(label, { selector: 'dt' });
  return {
    value: term.nextElementSibling?.textContent,
    detail: term.nextElementSibling?.nextElementSibling?.textContent,
  };
};

describe('CostActual', () => {
  beforeEach(() => {
    vi.mocked(useDataQuality).mockReturnValue({
      pricingSource: 'estimated',
      message: 'Public list prices (on-demand)',
    });
  });

  it('summarises cost, change, idle spend and pricing', () => {
    const data = fixture();
    render(<CostActual data={data} trend={trendWithPrevious(data)} loading={false} error={null} />);

    expect(stat('Monthly cost')).toEqual({
      value: '$535.60',
      detail: 'Kubernetes $488.40 + cloud $47.20',
    });
    expect(stat('Since last sync')).toEqual({
      value: '−86%',
      detail: 'Kubernetes, down from $3,525.90, mostly simple-deployment',
    });
    expect(stat('Idle spend')).toEqual({
      value: '$240.57',
      detail: '49% of Kubernetes cost',
    });
    expect(stat('Pricing')).toEqual({
      value: 'Estimated',
      detail: 'Public list prices (on-demand)',
    });
  });

  it('says so when there is no earlier measurement to compare with', () => {
    render(<CostActual data={fixture()} trend={null} loading={false} error={null} />);

    expect(stat('Since last sync')).toEqual({
      value: '—',
      detail: 'No earlier measurement',
    });
    expect(screen.queryByRole('heading', { name: 'What changed' })).toBeNull();
  });

  describe('measured at install', () => {
    it('compares nothing with a measurement taken before the application synced', () => {
      const data = fixture({ measuredOn: 'install' });
      render(<CostActual data={data} trend={trendWithPrevious(data)} loading={false} error={null} />);

      expect(stat('Since last sync')).toEqual({ value: '—', detail: 'Measured at install' });
      expect(screen.queryByRole('heading', { name: 'What changed' })).toBeNull();
      expect(screen.getByText(/measured with OpenCost when the service started/)).toBeTruthy();
    });

    it('does not compare the first sync with the measurement taken at install', () => {
      const data = fixture({ measuredOn: 'sync' });
      render(
        <CostActual data={data} trend={trendWithPrevious(data, { measuredOn: 'install' })} loading={false} error={null} />,
      );

      expect(stat('Since last sync')).toEqual({ value: '—', detail: 'No earlier measurement' });
      expect(screen.getByText('Costs are measured after each sync with OpenCost.')).toBeTruthy();
    });
  });

  describe('what changed since the last sync', () => {
    const data = fixture({
      totalMonthlyCost: 300,
      pods: [pod('api', 200), pod('worker', 100)],
    });

    it("compares with the application's share of an earlier measurement of a shared namespace", () => {
      // api, which belongs to something else, has since left the namespace.
      const latest = fixture({
        totalMonthlyCost: 100,
        pods: [pod('worker', 100)],
      });
      const trend = trendWithPrevious(latest, {
        totalMonthlyCost: 250,
        pods: [pod('api', 200), pod('worker', 50)],
      });
      render(
        <CostActual data={latest} trend={trend} loading={false} error={null} managedControllers={new Set(['worker'])} />
      );

      expect(stat('Since last sync')).toEqual({
        value: '+100%',
        detail: 'Kubernetes, up from $50.00, mostly worker',
      });
      const card = screen.getByRole('region', { name: 'What changed' });
      const rows = within(card)
        .getAllByRole('row')
        .map(row => row.textContent);
      expect(rows).toEqual(['workerdeployment · $50.00 → $100.00+$50.00', 'Total change+$50.00']);
    });

    it('leaves the driver out of the summary when the largest change went against the total', () => {
      // api fell by $60 while worker and another controller rose by $40 each: up overall, led by a fall.
      const latest = fixture({
        totalMonthlyCost: 320,
        pods: [pod('api', 140), pod('worker', 90), pod('cache', 90)],
      });
      const trend = trendWithPrevious(latest, {
        totalMonthlyCost: 300,
        pods: [pod('api', 200), pod('worker', 50), pod('cache', 50)],
      });
      render(<CostActual data={latest} trend={trend} loading={false} error={null} />);

      expect(stat('Since last sync').detail).toBe('Kubernetes, up from $300.00');
      expect(screen.getByRole('region', { name: 'What changed' })).toBeTruthy();
    });

    it('shows no card when the previous measurement has no pods to compare', () => {
      render(
        <CostActual data={data} trend={trendWithPrevious(data, { pods: undefined })} loading={false} error={null} />
      );

      expect(stat('Since last sync').detail).toBe('Kubernetes, down from $3,525.90');
      expect(screen.queryByRole('region', { name: 'What changed' })).toBeNull();
    });
  });

  describe('measured for the application alone', () => {
    // The backend measured worker and the Job its CronJob ran, which Argo CD lists only as report.
    const latest = fixture({
      scope: 'application',
      totalMonthlyCost: 110,
      efficiency: { ...fixture().efficiency!, cpuIdleCost: 40, ramIdleCost: 15 },
      pods: [pod('worker', 100), pod('report-29312345', 10)],
    });
    const managed = new Set(['worker', 'report']);
    const renderAlone = (previous: Partial<CostData> | null, managedControllers: Set<string> = managed) =>
      render(
        <CostActual
          data={latest}
          trend={previous ? trendWithPrevious(latest, previous) : null}
          loading={false}
          error={null}
          onShowResources={vi.fn()}
          managedControllers={managedControllers}
        />
      );

    it("shows the backend's figures as they are, about the application only", () => {
      renderAlone(null);

      expect(stat('Monthly cost')).toEqual({
        value: '$157.20',
        detail: 'Kubernetes $110.00 + cloud $47.20',
      });
      expect(stat('Idle spend')).toEqual({ value: '$55.00', detail: '50% of Kubernetes cost' });
      expect(screen.queryByText(/shares the .* namespace/)).toBeNull();
      expect(screen.queryByText(/Whole .* namespace/)).toBeNull();
      const recommendation = screen.getByRole('region', {
        name: 'Recommendation',
      });
      expect(within(recommendation).getByRole('heading').textContent).toBe(
        'Lower the CPU requests of this application'
      );
      expect(within(recommendation).getByRole('button', { name: 'View resources' })).toBeTruthy();
    });

    it('compares with an earlier measurement of the application', () => {
      renderAlone({
        scope: 'application',
        totalMonthlyCost: 100,
        pods: [pod('worker', 90), pod('report-29312345', 10)],
      });

      expect(stat('Since last sync')).toEqual({
        value: '+10%',
        detail: 'Kubernetes, up from $100.00, mostly worker',
      });
    });

    // The older record had no run of the CronJob; worker is the application's share of it.
    it("compares with the application's share of an older measurement of its namespace", () => {
      renderAlone({
        scope: 'namespace',
        totalMonthlyCost: 300,
        pods: [pod('api', 200), pod('worker', 100)],
      });

      expect(stat('Since last sync')).toEqual({
        value: '+10%',
        detail: 'Kubernetes, up from $100.00, mostly report-29312345',
      });
    });

    it('does not compare with an older measurement of its namespace it cannot narrow', () => {
      renderAlone(
        {
          scope: 'namespace',
          totalMonthlyCost: 300,
          pods: [pod('api', 200), pod('worker', 100)],
        },
        new Set()
      );

      expect(stat('Since last sync')).toEqual({
        value: '—',
        detail: 'No earlier measurement',
      });
    });
  });

  describe('in a namespace shared with other workloads', () => {
    // worker is the application's; api belongs to something else in the same namespace.
    const latest = fixture({
      totalMonthlyCost: 300,
      pods: [pod('api', 200), pod('worker', 100)],
    });
    const renderShared = (previous: Partial<CostData>) =>
      render(
        <CostActual
          data={latest}
          trend={trendWithPrevious(latest, previous)}
          loading={false}
          error={null}
          onShowResources={vi.fn()}
          managedControllers={new Set(['worker'])}
        />
      );

    it("counts only the application's pods in its cost and change", () => {
      renderShared({
        totalMonthlyCost: 250,
        pods: [pod('api', 200), pod('worker', 50)],
      });

      expect(stat('Monthly cost')).toEqual({
        value: '$147.20',
        detail: 'Kubernetes $100.00 + cloud $47.20',
      });
      expect(stat('Since last sync')).toEqual({
        value: '+100%',
        detail: 'Kubernetes, up from $50.00, mostly worker',
      });
      const card = screen.getByRole('region', { name: 'What changed' });
      expect(
        within(card)
          .getAllByRole('row')
          .map(row => row.textContent)
      ).toEqual(['workerdeployment · $50.00 → $100.00+$50.00', 'Total change+$50.00']);
    });

    it('ignores a change that happened outside the application', () => {
      renderShared({
        totalMonthlyCost: 150,
        pods: [pod('api', 50), pod('worker', 100)],
      });

      expect(stat('Since last sync')).toEqual({
        value: '0%',
        detail: 'Kubernetes, unchanged at $100.00',
      });
      expect(screen.queryByRole('region', { name: 'What changed' })).toBeNull();
    });

    it('says which figures cover the whole namespace', () => {
      renderShared({
        totalMonthlyCost: 250,
        pods: [pod('api', 200), pod('worker', 50)],
      });

      expect(screen.getByText(/This app shares the argo-rollouts namespace with 1 other workload/)).toBeTruthy();
      expect(stat('Idle spend')).toEqual({
        value: '$240.57',
        detail: '80% of the argo-rollouts namespace',
      });
      for (const name of ['Cost breakdown', 'Resource efficiency']) {
        const card = screen.getByRole('region', { name });
        expect(card.textContent).toContain('Whole argo-rollouts namespace, not only this app');
      }
      const recommendation = screen.getByRole('region', {
        name: 'Recommendation',
      });
      expect(within(recommendation).getByRole('heading').textContent).toBe('Lower CPU requests in argo-rollouts');
      // Resources lists only the application's controllers, so it cannot show what this advice covers.
      expect(
        within(recommendation).queryByRole('button', {
          name: 'View resources',
        })
      ).toBeNull();
    });
  });

  it('recommends a lower CPU request for the single controller and links to Resources', () => {
    const onShowResources = vi.fn();
    render(<CostActual data={fixture()} trend={null} loading={false} error={null} onShowResources={onShowResources} />);
    const card = screen.getByRole('region', { name: 'Recommendation' });

    expect(within(card).getByRole('heading').textContent).toBe('Lower the CPU request of simple-deployment');
    expect(card.textContent).toContain('It requests 1.00 cores and uses 0.40 cores on average.');
    expect(card.textContent).toContain('Requesting 500m would save about $163/mo.');
    expect(card.textContent).toContain("Peak CPU isn't collected");

    fireEvent.click(within(card).getByRole('button', { name: 'View resources' }));
    expect(onShowResources).toHaveBeenCalledTimes(1);
  });

  it('suggests no CPU request from a few minutes of data', () => {
    const data = fixture();
    render(
      <CostActual
        data={{ ...data, opencostOutput: { ...data.opencostOutput!, measuredHours: 0.05 } }}
        trend={null}
        loading={false}
        error={null}
      />
    );

    expect(screen.queryByRole('region', { name: 'Recommendation' })).toBeNull();
  });

  it("replaces the CPU suggestion with the VPA's recommendation when it has one", () => {
    render(<CostActual data={fixture()} trend={null} loading={false} error={null} rightsizing={shopRightsizing()} />);
    const cards = screen.getAllByRole('region', { name: 'Recommendation' });

    expect(cards).toHaveLength(1);
    expect(within(cards[0]).getByRole('heading').textContent).toBe('Right-size web and worker to save about $38/mo');
  });

  it('keeps the CPU suggestion when the VPA has nothing worth a dollar to suggest', () => {
    render(
      <CostActual
        data={fixture()}
        trend={null}
        loading={false}
        error={null}
        rightsizing={shopRightsizing({ monthlySaving: 0.4, monthlyIncrease: 0 })}
      />
    );
    const card = screen.getByRole('region', { name: 'Recommendation' });

    expect(within(card).getByRole('heading').textContent).toBe('Lower the CPU request of simple-deployment');
  });

  describe('layout', () => {
    afterEach(() => {
      vi.restoreAllMocks();
    });

    // follows reports whether `later` comes after `earlier` in reading order.
    const follows = (earlier: Element, later: Element) =>
      Boolean(earlier.compareDocumentPosition(later) & Node.DOCUMENT_POSITION_FOLLOWING);

    const renderAtWidth = (width: number, onShowResources = vi.fn()) => {
      vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({ width }) as DOMRect);
      render(
        <CostActual data={fixture()} trend={null} loading={false} error={null} onShowResources={onShowResources} />
      );
      return {
        recommendation: screen.getByRole('region', { name: 'Recommendation' }),
        breakdown: screen.getByRole('region', { name: 'Cost breakdown' }),
        efficiency: screen.getByRole('region', { name: 'Resource efficiency' }),
      };
    };

    it('leads with the recommendation above the cards at a normal width', () => {
      const { recommendation, breakdown } = renderAtWidth(1280);

      expect(follows(recommendation, breakdown)).toBe(true);
    });

    it('moves the recommendation beside the cards, last, on a wide screen', () => {
      const onShowResources = vi.fn();
      const { recommendation, breakdown, efficiency } = renderAtWidth(1880, onShowResources);

      expect(follows(breakdown, efficiency)).toBe(true);
      expect(follows(efficiency, recommendation)).toBe(true);
      expect(screen.getAllByRole('region', { name: 'Recommendation' })).toHaveLength(1);

      fireEvent.click(within(recommendation).getByRole('button', { name: 'View resources' }));
      expect(onShowResources).toHaveBeenCalledTimes(1);
    });

    it('keeps the banner just below the wide threshold', () => {
      const { recommendation, breakdown } = renderAtWidth(1879);

      expect(follows(recommendation, breakdown)).toBe(true);
    });
  });

  it('does not recommend anything when CPU use is close to the request', () => {
    const data = fixture();
    render(
      <CostActual
        data={{
          ...data,
          efficiency: {
            ...data.efficiency!,
            cpuEfficiency: 0.9,
            cpuCoresUsed: 0.9,
          },
        }}
        trend={null}
        loading={false}
        error={null}
      />
    );

    expect(screen.queryByRole('region', { name: 'Recommendation' })).toBeNull();
  });

  it('breaks the cost down with cloud services, their age and resources', () => {
    render(<CostActual data={fixture()} trend={null} loading={false} error={null} />);
    const breakdown = screen.getByRole('region', { name: 'Cost breakdown' });
    const rows = within(breakdown)
      .getAllByRole('row')
      .map(row => row.textContent);

    expect(rows).toContain('CPU$325.5061%');
    expect(rows).toContain('Total$535.60100%');
    expect(within(breakdown).getByText('Infracost estimate from 14 days ago')).toBeTruthy();
    expect(breakdown.textContent).toContain('Carbon footprint: 8.8 kg CO₂e per month, about 42 km by car');

    const toggle = within(breakdown).getByRole('button', {
      name: '3 resources',
    });
    expect(within(breakdown).queryByText('aws_s3_bucket.artifacts')).toBeNull();
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(within(breakdown).getByText('aws_s3_bucket.artifacts')).toBeTruthy();
  });

  it('shows CPU and memory efficiency with idle cost', () => {
    render(<CostActual data={fixture()} trend={null} loading={false} error={null} />);
    const card = screen.getByRole('region', { name: 'Resource efficiency' });

    expect(within(card).getByText('Over-provisioned')).toBeTruthy();
    expect(within(card).getByText('Moderate')).toBeTruthy();
    expect(
      within(card).getByRole('img', {
        name: 'CPU: Avg 0.40 cores, Request 1.00 cores, Limit 2.00 cores',
      })
    ).toBeTruthy();
    expect(within(card).getByText('$195.30 / mo idle')).toBeTruthy();
    expect(within(card).getByText('$240.57 / mo')).toBeTruthy();
  });

  it('reports a cost anomaly above the summary', () => {
    render(
      <CostActual
        data={fixture({
          anomaly: {
            severity: 'alert',
            score: 3,
            rollingAvg: 120,
            recordCount: 5,
            message: 'Cost is 4× the average',
          },
        })}
        trend={null}
        loading={false}
        error={null}
      />
    );

    const notice = screen.getByRole('status');
    expect(notice.textContent).toContain('Cost alert');
    expect(notice.textContent).toContain('Cost is 4× the average');
    expect(notice.textContent).toContain('Rolling average over the last 5 syncs: $120.00/mo');
  });

  it('shows loading, error and empty states', () => {
    const { rerender } = render(<CostActual data={null} trend={null} loading={true} error={null} />);
    expect(screen.getByText('Loading cost data…')).toBeTruthy();

    rerender(<CostActual data={null} trend={null} loading={false} error="proxy returned 502" />);
    expect(screen.getByRole('alert').textContent).toContain('proxy returned 502');

    rerender(<CostActual data={null} trend={null} loading={false} error={null} />);
    expect(screen.getByText(/No cost data yet/)).toBeTruthy();
  });
});
