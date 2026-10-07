import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CostPanel } from './CostPanel';
import { useCostData } from '../hooks/useCostData';
import { CostData } from '../utils/api';

vi.mock('../hooks/useCostData', () => ({ useCostData: vi.fn() }));
vi.mock('./CostActual', async () => {
  const { createElement } = await vi.importActual<typeof import('react')>('react');
  return {
    CostActual: ({ onShowResources }: { onShowResources?: () => void }) =>
      createElement('button', { type: 'button', onClick: onShowResources }, 'actual view'),
  };
});
vi.mock('./ResourcesTab', () => ({ ResourcesTab: () => 'resources view' }));

const DAY_MS = 86_400_000;
const refetch = vi.fn(() => Promise.resolve());

function withMeasurementAge(days: number, measuredHours?: number, record: Partial<CostData> = {}) {
  vi.mocked(useCostData).mockReturnValue({
    loading: false,
    error: null,
    data: {
      measuredAt: new Date(Date.now() - days * DAY_MS).toISOString(),
      opencostOutput: { allocationTotals: {}, measuredHours },
      ...record,
    } as CostData,
    trend: null,
    dailyCosts: [
      {
        date: '2026-09-15',
        totalCost: 0.5,
        cpuCost: 0.3,
        memoryCost: 0.2,
        networkCost: 0,
        storageCost: 0,
      },
    ],
    refetch,
  });
}

describe('CostPanel', () => {
  beforeEach(() => {
    refetch.mockClear();
  });

  it('moves between tabs with the arrow keys', () => {
    withMeasurementAge(1);
    render(<CostPanel appName="demo" />);
    const actual = screen.getByRole('tab', { name: 'Actual costs' });
    expect(actual.getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('tabpanel').textContent).toBe('actual view');

    fireEvent.keyDown(actual, { key: 'ArrowLeft' });

    const trends = screen.getByRole('tab', { name: 'Trends' });
    expect(trends.getAttribute('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(trends);
    expect(screen.getByRole('tabpanel').textContent).toContain('Daily cost');
  });

  it('switches to Resources from the Actual costs view and keeps focus in the tab bar', () => {
    withMeasurementAge(1);
    render(<CostPanel appName="demo" />);

    fireEvent.click(screen.getByRole('button', { name: 'actual view' }));

    const resources = screen.getByRole('tab', { name: 'Resources' });
    expect(resources.getAttribute('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(resources);
    expect(screen.getByRole('tabpanel').textContent).toBe('resources view');
  });

  it('opens on the tab a link names, and records each tab it moves to', () => {
    withMeasurementAge(1);
    window.history.replaceState(null, '', '/applications/argocd/redis?view=Cost+Intelligence&costTab=resources');

    render(<CostPanel appName="demo" />);

    expect(screen.getByRole('tab', { name: 'Resources' }).getAttribute('aria-selected')).toBe('true');

    fireEvent.click(screen.getByRole('tab', { name: 'Trends' }));

    const params = new URLSearchParams(window.location.search);
    expect(params.get('costTab')).toBe('trends');
    // Argo CD's own parameter survives.
    expect(params.get('view')).toBe('Cost Intelligence');
  });

  it('flags a measurement older than a week as stale', () => {
    withMeasurementAge(10);
    render(<CostPanel appName="demo" />);

    expect(screen.getByText('Stale')).toBeTruthy();
    expect(screen.getByText('Measured 10 days ago')).toBeTruthy();
  });

  it('does not flag a recent measurement', () => {
    withMeasurementAge(2);
    render(<CostPanel appName="demo" />);

    expect(screen.queryByText('Stale')).toBeNull();
    expect(screen.getByText('Measured 2 days ago')).toBeTruthy();
  });

  it('flags a measurement projected from less than a day of data', () => {
    withMeasurementAge(1, 6.2);
    render(<CostPanel appName="demo" />);

    expect(screen.getByText('Projected from 6 hours of data')).toBeTruthy();
    expect(screen.getByText('Measured yesterday')).toBeTruthy();
    expect(screen.getByText('Measured yesterday').parentElement?.getAttribute('title')).toContain(
      'OpenCost had 6 hours of data for this namespace'
    );
  });

  it('says whose data a partial measurement covers', () => {
    withMeasurementAge(1, 6.2, { scope: 'application' });
    render(<CostPanel appName="demo" />);

    expect(screen.getByText('Measured yesterday').parentElement?.getAttribute('title')).toContain(
      'OpenCost had 6 hours of data for this application'
    );
  });

  it('labels the daily series as the namespace only for an older record of a shared namespace', () => {
    const pod = (name: string, controller: string, controllerKind: string) => ({
      name,
      namespace: 'shop',
      controller,
      controllerKind,
      cpuCost: 1,
      ramCost: 0,
      totalCost: 1,
    });
    const managed = new Set(['web', 'report']);
    const label = /Whole shop namespace/;

    withMeasurementAge(1, undefined, {
      scope: 'namespace',
      pods: [pod('web-1', 'web', 'deployment'), pod('db-0', 'db', 'statefulset')],
    });
    const { unmount } = render(<CostPanel appName="demo" managedControllers={managed} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Trends' }));
    expect(screen.getByText(label)).toBeTruthy();
    unmount();

    // The backend counts the Job the CronJob ran, which Argo CD lists only as report.
    withMeasurementAge(1, undefined, {
      scope: 'application',
      pods: [pod('web-1', 'web', 'deployment'), pod('report-29312345-x', 'report-29312345', 'job')],
    });
    render(<CostPanel appName="demo" managedControllers={managed} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Trends' }));
    expect(screen.queryByText(label)).toBeNull();
  });

  it('does not flag a full day of data, or a record that predates measuredHours', () => {
    withMeasurementAge(1, 23.9);
    const { unmount } = render(<CostPanel appName="demo" />);
    expect(screen.queryByText(/Projected from/)).toBeNull();
    unmount();

    withMeasurementAge(1);
    render(<CostPanel appName="demo" />);
    expect(screen.queryByText(/Projected from/)).toBeNull();
  });

  it('shows how fresh the daily series is on the Trends tab', () => {
    withMeasurementAge(10);
    render(<CostPanel appName="demo" />);

    fireEvent.click(screen.getByRole('tab', { name: 'Trends' }));

    expect(screen.getByText('Daily data through Sep 15')).toBeTruthy();
    expect(screen.queryByText('Stale')).toBeNull();
  });

  it('reloads on demand', async () => {
    withMeasurementAge(1);
    render(<CostPanel appName="demo" />);
    const reload = screen.getByRole('button', { name: 'Reload cost data' });

    fireEvent.click(reload);

    expect(refetch).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(reload.hasAttribute('disabled')).toBe(false));
  });
});
