import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { EfficiencyCard } from './EfficiencyCard';
import { EfficiencyMetrics } from '../utils/api';

const MIB = 1024 ** 2;

function efficiency(overrides: Partial<EfficiencyMetrics> = {}): EfficiencyMetrics {
  return {
    cpuEfficiency: 0.4,
    ramEfficiency: 0.7,
    totalEfficiency: 0.55,
    cpuCoresRequested: 1,
    cpuCoresUsed: 0.4,
    ramBytesRequested: 512 * MIB,
    ramBytesUsed: 375809638,
    cpuIdleCost: 195.3,
    ramIdleCost: 45.27,
    cpuCoresLimit: 2,
    ramBytesLimit: 1024 * MIB,
    cpuCoresUsedMax: 0,
    ramBytesUsedMax: 0,
    ...overrides,
  };
}

// meter returns the CPU or memory bar with its fill and markers, found by its accessible label.
function meter(label: 'CPU' | 'Memory') {
  const bar = screen.getByRole('img', { name: new RegExp(`^${label}:`) });
  const [fill, ...markers] = [...bar.children] as HTMLElement[];
  return { name: bar.getAttribute('aria-label'), fill: fill.style.width, markers: markers.map(marker => marker.style.left) };
}

describe('EfficiencyCard', () => {
  it('warns when peak use is above the request and marks the peak', () => {
    render(<EfficiencyCard efficiency={efficiency({ cpuCoresUsedMax: 1.4 })} />);

    expect(meter('CPU')).toEqual({
      name: 'CPU: Avg 0.40 cores, Peak 1.40 cores, Request 1.00 cores, Limit 2.00 cores',
      fill: '20%',
      markers: ['70%', '50%'],
    });
    expect(screen.getAllByText('Peak use is above the request, so pods may be throttled.')).toHaveLength(1);
    expect(screen.getByText('Peak')).toBeTruthy();
  });

  it('does not warn when the peak stays within the request', () => {
    render(<EfficiencyCard efficiency={efficiency({ cpuCoresUsedMax: 0.8, ramBytesUsedMax: 400 * MIB })} />);

    expect(screen.queryByText(/Peak use is above the request/)).toBeNull();
    expect(meter('Memory').name).toBe('Memory: Avg 358 MB, Peak 400 MB, Request 512 MB, Limit 1.0 GB');
  });

  it('leaves peaks out when they are not collected', () => {
    render(<EfficiencyCard efficiency={efficiency()} />);

    expect(screen.queryByText('Peak')).toBeNull();
    expect(meter('CPU')).toEqual({
      name: 'CPU: Avg 0.40 cores, Request 1.00 cores, Limit 2.00 cores',
      fill: '20%',
      markers: ['50%'],
    });
  });

  it('scales the bar to the largest value when no limit is set', () => {
    render(<EfficiencyCard efficiency={efficiency({ cpuCoresLimit: 0 })} />);

    expect(meter('CPU')).toEqual({ name: 'CPU: Avg 0.40 cores, Request 1.00 cores', fill: '40%', markers: ['100%'] });
  });

  it('labels each resource by how its use compares with the request', () => {
    render(<EfficiencyCard efficiency={efficiency({ cpuEfficiency: 1.3, cpuCoresUsed: 1.3, ramEfficiency: 0.9 })} />);

    expect(screen.getByText('Above request')).toBeTruthy();
    expect(screen.getByText('Healthy')).toBeTruthy();
    expect(screen.getByText('$240.57 / mo')).toBeTruthy();
  });

  it('gives no verdict or percentage for a resource the pods request none of', () => {
    render(
      <EfficiencyCard
        efficiency={efficiency({ cpuEfficiency: 0, cpuCoresRequested: 0, cpuCoresLimit: 0, cpuCoresUsedMax: 0.6, cpuIdleCost: 0, totalEfficiency: 0.7 })}
      />
    );

    expect(meter('CPU')).toEqual({ name: 'CPU: Avg 0.40 cores, Peak 0.60 cores, No request', fill: '66.66666666666667%', markers: ['100%'] });
    expect(screen.getByText('No request set')).toBeTruthy();
    expect(screen.queryByText('Over-provisioned')).toBeNull();
    expect(screen.queryByText(/Peak use is above the request/)).toBeNull();
    expect(screen.getByText('Moderate')).toBeTruthy();
    expect(screen.getByText('70% of requested resources used')).toBeTruthy();
  });

  it('shows no efficiency when the pods set no requests at all', () => {
    render(
      <EfficiencyCard
        efficiency={efficiency({
          cpuEfficiency: 0,
          ramEfficiency: 0,
          totalEfficiency: 0,
          cpuCoresRequested: 0,
          ramBytesRequested: 0,
          cpuIdleCost: 0,
          ramIdleCost: 0,
        })}
      />
    );

    expect(screen.getByText('The pods set no CPU or memory requests')).toBeTruthy();
    expect(screen.queryByText(/0% of requested/)).toBeNull();
    expect(screen.getAllByText('No request set')).toHaveLength(2);
    expect(screen.queryByText('Request')).toBeNull();
  });
});
