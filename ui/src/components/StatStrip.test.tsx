import React from 'react';
import { render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { StatStrip } from './StatStrip';

const stats = [
  { label: 'Cluster monthly cost', value: '$3,762.90' },
  { label: 'Idle spend', value: '$1,752.48' },
  { label: 'Control plane fee', value: 'Not available' },
  { label: 'Pricing', value: 'Estimated' },
];

// dividers lists, for each stat, whether a divider separates it from the stat before it. jsdom does
// not echo "none" back for a cleared border, so this looks for the drawn line itself.
function dividers(container: HTMLElement): boolean[] {
  return [...(container.querySelector('dl') as HTMLElement).children].map(cell =>
    (cell as HTMLElement).style.borderLeft.includes('solid')
  );
}

function renderAtWidth(width: number) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({ width }) as DOMRect);
  return render(<StatStrip stats={stats} />);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('StatStrip', () => {
  it('puts every stat on one row when there is room', () => {
    const { container } = renderAtWidth(1600);

    expect(dividers(container)).toEqual([false, true, true, true]);
  });

  it('folds into two columns, dividing only stats on the same row', () => {
    const { container } = renderAtWidth(600);

    expect(dividers(container)).toEqual([false, true, false, true]);
  });

  it('stacks the stats when narrow', () => {
    const { container } = renderAtWidth(300);

    expect(dividers(container)).toEqual([false, false, false, false]);
  });
});
