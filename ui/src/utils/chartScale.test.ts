import { describe, expect, it } from 'vitest';
import { niceTicks, tickDecimals } from './chartScale';

describe('niceTicks', () => {
  it.each([
    [0.597, [0, 0.2, 0.4, 0.6]],
    [0.8, [0, 0.2, 0.4, 0.6, 0.8]],
    [16.28, [0, 5, 10, 15, 20]],
    [3525.9, [0, 1000, 2000, 3000, 4000]],
  ])('max %s', (max, want) => {
    expect(niceTicks(max)).toEqual(want);
  });

  it('falls back to a unit axis when there is no positive value', () => {
    expect(niceTicks(0)).toEqual([0, 1]);
    expect(niceTicks(Number.NaN)).toEqual([0, 1]);
  });
});

describe('tickDecimals', () => {
  it.each([
    [[0, 1000, 2000], 0],
    [[0, 5, 10, 15, 20], 0],
    [[0, 0.2, 0.4, 0.6], 2],
    [[0, 2.5, 5, 7.5], 2],
    [[0, 0.002, 0.004, 0.006], 3],
    [[0, 0.0005, 0.001], 4],
  ])('ticks %j need %i decimals', (ticks, want) => {
    expect(tickDecimals(ticks)).toBe(want);
  });

  it('gives every tick of a tiny axis a different label', () => {
    const ticks = niceTicks(0.0087);
    const labels = ticks.map(tick => tick.toFixed(tickDecimals(ticks)));
    expect(new Set(labels).size).toBe(ticks.length);
  });

  it('stops at a sensible number of decimals', () => {
    expect(tickDecimals([0, 1e-9])).toBe(6);
  });
});
