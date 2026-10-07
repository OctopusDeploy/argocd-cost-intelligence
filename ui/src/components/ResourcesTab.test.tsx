import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ResourcesTab } from './ResourcesTab';
import { useOrphanedPods } from '../hooks/useOrphanedPods';
import { CostData, PodCost } from '../utils/api';
import { shopRightsizing } from '../test/rightsizing';

vi.mock('../hooks/useOrphanedPods', () => ({ useOrphanedPods: vi.fn() }));

const pod = (name: string, controller: string, controllerKind: string, totalCost: number): PodCost => ({
  name,
  namespace: 'shop',
  controller,
  controllerKind,
  cpuCost: totalCost * 0.6,
  ramCost: totalCost * 0.4,
  totalCost,
});

const data = {
  totalMonthlyCost: 75,
  pods: [
    pod('web-1', 'web', 'Deployment', 20),
    pod('web-2', 'web', 'Deployment', 10),
    pod('db-0', 'db', 'StatefulSet', 40),
    pod('other-1', 'other', 'Deployment', 5),
  ],
} as CostData;

const managed = new Set(['web', 'db']);

const rowHeaders = () => screen.getAllByRole('rowheader').map(cell => cell.textContent);

describe('ResourcesTab', () => {
  beforeEach(() => {
    vi.mocked(useOrphanedPods).mockReturnValue({
      pods: [],
      totalWasted: 0,
      loading: false,
    });
  });

  it("lists this application's controllers, most expensive first, with their share", () => {
    render(<ResourcesTab data={data} loading={false} error={null} managedControllers={managed} />);

    expect(rowHeaders()).toEqual(['db', 'web']);
    expect(screen.getByText('2 controllers · 3 pods · monthly cost')).toBeTruthy();
    expect(screen.getByText('57%')).toBeTruthy();
    expect(screen.getByText('43%')).toBeTruthy();
  });

  it('shows and hides the pods of a controller', () => {
    render(<ResourcesTab data={data} loading={false} error={null} managedControllers={managed} />);
    const toggle = screen.getByRole('button', { name: 'Pods of web' });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');

    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(rowHeaders()).toEqual(['db', 'web', 'web-1', 'web-2']);

    fireEvent.click(toggle);
    expect(rowHeaders()).toEqual(['db', 'web']);
  });

  it('lists every pod of a measurement the backend already limited to the application', () => {
    // The backend matched the Job a CronJob ran, which Argo CD lists only under the CronJob.
    const measured = {
      ...data,
      scope: 'application',
      pods: [...data.pods!, pod('report-29312345-x', 'report-29312345', 'job', 3)],
    } as CostData;
    render(
      <ResourcesTab
        data={measured}
        loading={false}
        error={null}
        managedControllers={new Set(['web', 'db', 'report'])}
      />
    );

    expect(rowHeaders()).toEqual(['db', 'web', 'other', 'report-29312345']);
  });

  it('shows every pod when Argo CD lists no controllers', () => {
    render(<ResourcesTab data={data} loading={false} error={null} />);

    expect(rowHeaders()).toEqual(['db', 'web', 'other']);
  });

  it("explains when none of the pods belong to the application's controllers", () => {
    render(<ResourcesTab data={data} loading={false} error={null} managedControllers={new Set(['missing'])} />);

    expect(screen.getByText(/No costs found for this application's controllers/)).toBeTruthy();
  });

  it('reports orphaned pods in the namespace behind a toggle', () => {
    vi.mocked(useOrphanedPods).mockReturnValue({
      pods: [
        {
          name: 'stuck-0',
          namespace: 'shop',
          controller: 'batch',
          controllerKind: 'Job',
          cpuCost: 0,
          ramCost: 0,
          storageCost: 3.5,
          totalCost: 3.5,
        },
      ],
      totalWasted: 3.5,
      loading: false,
    });
    const ctx = { namespace: 'argocd', name: 'shop-web', project: 'shop' };
    render(
      <ResourcesTab
        data={data}
        loading={false}
        error={null}
        managedControllers={managed}
        appName="shop-web"
        appCtx={ctx}
      />
    );

    // Asked as the application, so a viewer of the application alone can see them.
    expect(vi.mocked(useOrphanedPods)).toHaveBeenCalledWith('shop', 'shop-web', ctx);
    const toggle = screen.getByRole('button', {
      name: /1 orphaned pod in shop/,
    });
    expect(screen.queryByText('stuck-0')).toBeNull();

    fireEvent.click(toggle);

    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('stuck-0')).toBeTruthy();
  });

  it('shows loading, error and empty states', () => {
    const { rerender } = render(<ResourcesTab data={null} loading={true} error={null} />);
    expect(screen.getByText('Loading resource costs…')).toBeTruthy();

    rerender(<ResourcesTab data={null} loading={false} error="proxy returned 502" />);
    expect(screen.getByRole('alert').textContent).toContain('proxy returned 502');

    rerender(<ResourcesTab data={{ ...data, pods: [] }} loading={false} error={null} />);
    expect(screen.getByText(/No resource costs yet/)).toBeTruthy();
  });

  it("shows the VPA's recommendations below the controllers", () => {
    const { rerender } = render(
      <ResourcesTab data={data} loading={false} error={null} managedControllers={managed} rightsizing={shopRightsizing()} />
    );
    const section = screen.getByRole('region', { name: 'Rightsizing' });
    expect(section.textContent).toContain('web · Deployment · 3 pods');

    rerender(<ResourcesTab data={data} loading={false} error={null} managedControllers={managed} rightsizing={null} />);
    expect(screen.queryByRole('region', { name: 'Rightsizing' })).toBeNull();
  });
});
