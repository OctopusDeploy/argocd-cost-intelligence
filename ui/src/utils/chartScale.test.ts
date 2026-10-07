import { describe, expect, it } from 'vitest';
import { niceTicks } from './chartScale';

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
