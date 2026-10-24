import React from 'react';
import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ChangeDriversCard } from './ChangeDriversCard';
import { ChangeDriver, ChangeDrivers, NOT_TIED_TO_A_POD } from '../utils/costInsights';

const driver = (overrides: Partial<ChangeDriver>): ChangeDriver => ({
  key: 'deployment/worker',
  kind: 'deployment',
  name: 'worker',
  status: 'changed',
  before: 60,
  after: 101,
  delta: 41,
  ...overrides,
});

const rows = () =>
  within(screen.getByRole('table', { name: 'What changed' }))
    .getAllByRole('row')
    .map(row => row.textContent);

describe('ChangeDriversCard', () => {
  const drivers: ChangeDrivers = {
    drivers: [
      driver({}),
      driver({ key: 'cronjob/report', kind: 'cronjob', name: 'report', status: 'removed', before: 30, after: 0, delta: -30 }),
      driver({ key: 'statefulset/cache', kind: 'statefulset', name: 'cache', status: 'added', before: 0, after: 12, delta: 12 }),
      driver({ key: NOT_TIED_TO_A_POD, kind: '', name: 'Not tied to a pod', status: null, before: 3, after: 8, delta: 5 }),
    ],
    smaller: 0.75,
    smallerCount: 2,
    total: 28.75,
  };

  it('lists each change with its kind, status and costs, then the rest and the total', () => {
    render(<ChangeDriversCard drivers={drivers} />);

    expect(rows()).toEqual([
      'workerdeployment · $60.00 → $101.00+$41.00',
      'reportcronjob · Removed · $30.00 → $0.00−$30.00',
      'cachestatefulset · New · $0.00 → $12.00+$12.00',
      'Not tied to a podUnmounted volumes and other costs OpenCost does not assign to a pod+$5.00',
      '2 smaller changes+$0.75',
      'Total change+$28.75',
    ]);
    expect(screen.getByText(/covers the 24 hours before it/)).toBeTruthy();
  });

  it('leaves out the smaller changes row when there are none', () => {
    render(<ChangeDriversCard drivers={{ drivers: [driver({})], smaller: 0, smallerCount: 0, total: 41 }} />);

    expect(rows()).toEqual(['workerdeployment · $60.00 → $101.00+$41.00', 'Total change+$41.00']);
  });

  it('rounds the changes so they add up to the total change', () => {
    // Each rounded on its own, the two changes would show as +$1.01 above a total of +$2.01.
    render(
      <ChangeDriversCard
        drivers={{
          drivers: [
            driver({ before: 1, after: 2.006, delta: 1.006 }),
            driver({ key: 'deployment/api', name: 'api', before: 1, after: 2.006, delta: 1.006 }),
          ],
          smaller: 0,
          smallerCount: 0,
          total: 2.012,
        }}
      />
    );

    expect(rows().map(row => row?.slice(-6))).toEqual(['+$1.01', '+$1.00', '+$2.01']);
  });

  it('renders nothing when no change is large enough to list', () => {
    const { container } = render(
      <ChangeDriversCard drivers={{ drivers: [], smaller: 0.4, smallerCount: 3, total: 0.4 }} />
    );

    expect(container.textContent).toBe('');
  });
});
