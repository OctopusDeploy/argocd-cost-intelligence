import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { DailyChart } from './DailyChart';

describe('DailyChart', () => {
  it("rounds the tooltip's CPU, memory and other costs so they add up to the day's cost", () => {
    // Each rounded on its own, CPU and memory would show as $5.00 each beside $0.01 of storage, under
    // a day of $10.00.
    render(
      <DailyChart
        dailyCosts={[
          { date: '2026-09-14', totalCost: 9.998, cpuCost: 4.995, memoryCost: 4.995, networkCost: 0, storageCost: 0.008 },
        ]}
        today="2026-09-15"
      />
    );
    fireEvent.keyDown(screen.getByRole('group', { name: /daily cost chart/i }), { key: 'ArrowLeft' });

    expect(screen.getByText('$10.00 / day')).toBeTruthy();
    expect(screen.getByText('CPU $5.00 · Memory $4.99 · Other $0.01')).toBeTruthy();
  });
});
