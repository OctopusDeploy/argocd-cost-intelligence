import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CostBreakdownCard } from './CostBreakdownCard';
import { CostBreakdown, CostData } from '../utils/api';

const DAY_MS = 86_400_000;

function withTotals(totals: Partial<CostBreakdown>, extra: Partial<CostData> = {}): CostData {
  return {
    applicationName: 'app',
    totalMonthlyCost: 100,
    measuredAt: '2026-09-01T00:00:00Z',
    opencostOutput: {
      allocationTotals: {
        cpuCost: 60,
        memoryCost: 30,
        storageCost: 0,
        gpuCost: 0,
        networkCost: 10,
        loadBalancerCost: 0,
        totalCost: 100,
        ...totals,
      },
    },
    ...extra,
  };
}

const rows = () => screen.getAllByRole('row').map(row => row.textContent);

describe('CostBreakdownCard', () => {
  it('lists network sub-costs under network', () => {
    render(<CostBreakdownCard data={withTotals({ networkCrossZoneCost: 6, networkInternetCost: 4 })} />);

    expect(rows()).toEqual([
      'CPU$60.0060%',
      'Memory$30.0030%',
      'Network$10.0010%',
      'Cross-zone$6.006.0%',
      'Internet$4.004.0%',
      'Total$100.00100%',
    ]);
  });

  it('rounds the rows so they add up to the total, and the sub-costs to their row', () => {
    // Each rounded on its own, the rows would show as $0.09, $0.09 and $0.17 above a total of $0.34,
    // and the sub-costs as $0.09 each under network.
    render(
      <CostBreakdownCard
        data={withTotals({
          cpuCost: 0.085,
          memoryCost: 0.085,
          networkCost: 0.174,
          networkCrossZoneCost: 0.087,
          networkInternetCost: 0.087,
          totalCost: 0.344,
        })}
      />
    );

    expect(rows()).toEqual([
      'CPU$0.0925%',
      'Memory$0.0825%',
      'Network$0.1751%',
      'Cross-zone$0.0925%',
      'Internet$0.0825%',
      'Total$0.34100%',
    ]);
  });

  it('keeps shared costs collapsed and lists the largest services first when expanded', () => {
    render(<CostBreakdownCard data={withTotals({ sharedCost: 12.5, sharedCostBreakdown: { dns: 2.5, monitoring: 10 } })} />);
    const toggle = screen.getByRole('button', { name: /Shared costs/ });

    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(toggle.textContent).toContain('$12.50');
    expect(screen.queryByText('monitoring')).toBeNull();

    fireEvent.click(toggle);

    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getAllByText(/^(monitoring|dns)$/).map(name => name.textContent)).toEqual(['monitoring', 'dns']);
    expect(screen.getByText(/Attributed proportionally from cluster-wide services/)).toBeTruthy();
  });

  it('leaves out shared costs when there are none', () => {
    render(<CostBreakdownCard data={withTotals({})} />);

    expect(screen.queryByRole('button', { name: /Shared costs/ })).toBeNull();
  });

  it('adds up to the monthly cost, with the cloud estimate rounded on its own', () => {
    // Rounded as parts of one total, CPU would show as $1.01 above a total of $2.01, where the
    // monthly cost shows Kubernetes $1.00 + cloud $1.00.
    render(
      <CostBreakdownCard
        data={withTotals(
          { cpuCost: 1.004, memoryCost: 0, networkCost: 0, totalCost: 1.004 },
          {
            totalMonthlyCost: 1.004,
            cloudEstimate: {
              source: 'infracost',
              totalMonthlyCost: 1.004,
              currency: 'USD',
              resources: [],
              pushedAt: new Date().toISOString(),
            },
          }
        )}
      />
    );

    expect(rows()[0]).toBe('CPU$1.0050%');
    expect(rows().find(row => row?.startsWith('Cloud services'))).toContain('$1.00');
    expect(rows().find(row => row?.startsWith('Total'))).toBe('Total$2.00100%');
  });

  it('flags a cloud estimate only once it is a week old', () => {
    const estimate = (days: number) => ({
      cloudEstimate: {
        source: 'infracost' as const,
        totalMonthlyCost: 5,
        currency: 'USD',
        resources: [],
        pushedAt: new Date(Date.now() - days * DAY_MS).toISOString(),
      },
    });

    const { rerender } = render(<CostBreakdownCard data={withTotals({}, estimate(2))} />);
    expect(screen.getByText('Infracost estimate from 2 days ago').querySelector('svg')).toBeNull();

    rerender(<CostBreakdownCard data={withTotals({}, estimate(8))} />);
    expect(screen.getByText('Infracost estimate from 8 days ago').querySelector('svg')).not.toBeNull();
  });

  it('explains when there is nothing to break down', () => {
    render(<CostBreakdownCard data={{ applicationName: 'app', totalMonthlyCost: 0, measuredAt: '' }} />);

    expect(screen.getByText('No cost breakdown for this measurement.')).toBeTruthy();
  });
});
