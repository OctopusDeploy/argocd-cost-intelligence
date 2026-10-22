import React from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { RightsizingCard } from './RightsizingCard';
import { shopRightsizing } from '../test/rightsizing';

describe('RightsizingCard', () => {
  it('names what to lower and what to raise, with the saving and the increase apart', () => {
    const onShowResources = vi.fn();
    render(<RightsizingCard rightsizing={shopRightsizing()} onShowResources={onShowResources} />);
    const card = screen.getByRole('region', { name: 'Recommendation' });

    expect(within(card).getByRole('heading').textContent).toBe('Right-size web and worker to save about $38/mo');
    expect(card.textContent).toContain('The VPA suggests lower requests for web and worker (−$41.31/mo).');
    expect(card.textContent).toContain('worker uses more than it requests (+$3.69/mo), so its pods can run short.');
    expect(card.textContent).toContain('Requests only; limits are left as they are.');
    expect(card.textContent).not.toContain('Low confidence');

    fireEvent.click(within(card).getByRole('button', { name: 'View resources' }));
    expect(onShowResources).toHaveBeenCalledTimes(1);
  });

  it('asks to raise requests when that outweighs the saving', () => {
    const rightsizing = shopRightsizing({ monthlySaving: 0, monthlyIncrease: 12 });
    rightsizing.workloads = rightsizing.workloads.filter(w => w.name === 'worker');
    rightsizing.workloads[0].containers[0].monthlyChange = { cpu: 12, memory: 0 };
    render(<RightsizingCard rightsizing={rightsizing} />);
    const card = screen.getByRole('region', { name: 'Recommendation' });

    expect(within(card).getByRole('heading').textContent).toBe('Raise the requests of worker');
    expect(card.textContent).not.toContain('suggests lower requests');
    expect(within(card).queryByRole('button')).toBeNull();
  });

  it('names every workload to change, including one that only needs more', () => {
    const rightsizing = shopRightsizing();
    // worker only needs more CPU now; web is the only one to lower.
    rightsizing.workloads[3].containers[0].monthlyChange = { cpu: 3.69, memory: 0 };
    render(<RightsizingCard rightsizing={rightsizing} />);

    expect(screen.getByRole('heading').textContent).toBe('Right-size web and worker to save about $38/mo');
    expect(screen.getByRole('region').textContent).toContain('The VPA suggests lower requests for web (');
  });

  it('asks to raise requests when the saving is under a dollar more than the increase', () => {
    render(<RightsizingCard rightsizing={shopRightsizing({ monthlySaving: 4, monthlyIncrease: 3.5 })} />);

    expect(screen.getByRole('heading').textContent).toBe('Raise the requests of worker');
  });

  it("says when the VPA's range is still wide", () => {
    const rightsizing = shopRightsizing();
    rightsizing.workloads[2].lowConfidence = true;
    render(<RightsizingCard rightsizing={rightsizing} />);

    expect(screen.getByText(/^Low confidence: the VPA's range is still wide for some containers/)).toBeTruthy();
  });
});
