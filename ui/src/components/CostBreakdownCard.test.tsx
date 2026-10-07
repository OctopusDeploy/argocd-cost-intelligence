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
