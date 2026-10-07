import { describe, expect, it } from 'vitest';
import { DailyCost } from './api';
import { summarizeDaily } from './dailyStats';

const day = (date: string, totalCost: number): DailyCost => ({
  date,
  totalCost,
  cpuCost: 0,
  memoryCost: 0,
  networkCost: 0,
  storageCost: 0,
});

describe('summarizeDaily', () => {
  it('returns null for an empty series', () => {
    expect(summarizeDaily([])).toBeNull();
  });

  it('computes average, total and extremes, keeping the earliest day on ties', () => {
    const summary = summarizeDaily([
      day('2026-09-01', 0.5),
      day('2026-09-02', 0.6),
      day('2026-09-03', 0.4),
      day('2026-09-04', 0.6),
    ]);

    expect(summary).not.toBeNull();
    expect(summary!.total).toBeCloseTo(2.1);
    expect(summary!.average).toBeCloseTo(0.525);
    expect(summary!.highest.date).toBe('2026-09-02');
    expect(summary!.lowest.date).toBe('2026-09-03');
    expect(summary!.firstDay).toBe('2026-09-01');
    expect(summary!.lastDay).toBe('2026-09-04');
  });
});
