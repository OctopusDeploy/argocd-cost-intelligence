import React from 'react';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { RightsizingSection } from './RightsizingSection';
import { shopRightsizing } from '../test/rightsizing';

const section = () => screen.getByRole('region', { name: 'Rightsizing' });

describe('RightsizingSection', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('renders nothing without a VPA', () => {
    const none = shopRightsizing();
    none.workloads = none.workloads.filter(w => w.status === 'none');
    const { container, rerender } = render(<RightsizingSection rightsizing={none} />);
    expect(container.textContent).toBe('');

    rerender(<RightsizingSection rightsizing={shopRightsizing({ available: false })} />);
    expect(container.textContent).toBe('');
    rerender(<RightsizingSection rightsizing={null} />);
    expect(container.textContent).toBe('');
  });

  it('shows every workload, with the VPA state of those it cannot advise on', () => {
    render(<RightsizingSection rightsizing={shopRightsizing()} />);
    const headings = within(section())
      .getAllByRole('heading', { level: 4 })
      .map(h => h.textContent);

    expect(headings).toEqual([
      'nightly-report · CronJob',
      'redis · StatefulSet',
      'web · Deployment · 3 pods',
      'worker · Deployment · 2 pods',
    ]);
    expect(section().textContent).toContain('No VPA');
    expect(section().textContent).toContain('Managed by VPA (Auto)');
    expect(section().textContent).toContain('−$37.21/mo');
  });

  it("puts each request against the VPA's range in words", () => {
    render(<RightsizingSection rightsizing={shopRightsizing()} />);
    const text = section().textContent;

    expect(text).toContain('CPU: requests 500m, above the range');
    expect(text).toContain('VPA 120m (range 80m–210m)');
    expect(text).toContain('Memory: requests 1Gi, above the range');
    expect(text).toContain('CPU: requests 1 core, within the range');
    expect(text).toContain('VPA 1.38 cores (range 970m–2.2 cores)');
    expect(text).toContain('−$32.08/mo');
  });

  it('warns when a memory limit is below what the VPA expects the container to need', () => {
    render(<RightsizingSection rightsizing={shopRightsizing()} />);

    expect(
      screen.getByText(
        'The memory limit of 512Mi is below the most the VPA expects it to need, 640Mi. Raise it to at least 640Mi so the container is not killed for running out of memory.'
      )
    ).toBeTruthy();
    expect(section().textContent?.match(/The memory limit of/g)).toHaveLength(1);
  });

  it('leaves out an upper bound the VPA has not settled, and its memory-limit warning', () => {
    const rightsizing = shopRightsizing();
    const web = rightsizing.workloads[2].containers[0];
    web.unsettled = true;
    web.upperBound = { cpuCores: 16.4, memoryBytes: 16 * 1024 ** 3 };
    render(<RightsizingSection rightsizing={rightsizing} />);
    const text = section().textContent;

    expect(text).toContain('VPA 120m (from 80m; upper bound not settled yet)');
    expect(text).toContain('VPA 384Mi (from 260Mi; upper bound not settled yet)');
    expect(text).not.toContain('16.4 cores');
    expect(text).not.toContain('The memory limit of');
    // istio-proxy's range is settled.
    expect(text).toContain('VPA 30m (range 16m–60m)');
  });

  it('copies the requests to paste and says so', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    render(<RightsizingSection rightsizing={shopRightsizing()} />);

    const button = screen.getByRole('button', { name: 'Copy the suggested requests for istio-proxy' });
    await act(async () => {
      fireEvent.click(button);
    });

    expect(writeText).toHaveBeenCalledWith(
      ['- name: istio-proxy', '  resources:', '    requests:', '      cpu: 30m', '      memory: 96Mi'].join('\n')
    );
    expect(button.textContent).toBe('Copied');
    expect(screen.getAllByRole('status').map(s => s.textContent)).toEqual([
      '',
      'Copied the suggested requests for istio-proxy',
      '',
    ]);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(button.textContent).toBe('Copy');
  });

  it('offers no copy button without a clipboard', () => {
    vi.stubGlobal('navigator', {});
    render(<RightsizingSection rightsizing={shopRightsizing()} />);

    expect(screen.queryByRole('button', { name: /^Copy/ })).toBeNull();
    expect(section().textContent).toContain('cpu: 120m');
  });
});
