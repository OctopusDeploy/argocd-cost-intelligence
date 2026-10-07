import { describe, expect, it } from 'vitest';
import { DailyCost } from './api';
import {
  bucketDays,
  customRangeError,
  fromFirstCost,
  granularity,
  includesToday,
  presetRange,
  queryRange,
  spanDays,
} from './ranges';

const day = (date: string, totalCost: number): DailyCost => ({
  date,
  totalCost,
  cpuCost: 0,
  memoryCost: 0,
  networkCost: 0,
  storageCost: 0,
});

const TODAY = '2026-10-03';

describe('presetRange', () => {
  it.each([
    ['30d', '2026-09-04', '2026-10-03', 30],
    ['month', '2026-10-01', '2026-10-31', 31],
    ['last', '2026-09-01', '2026-09-30', 30],
    ['90d', '2026-07-06', '2026-10-03', 90],
    ['6m', '2026-04-04', '2026-10-03', 183],
    ['1y', '2025-10-04', '2026-10-03', 365],
  ] as const)('resolves %s', (id, from, to, days) => {
    const range = presetRange(id, TODAY);
    expect(range).toEqual({ from, to });
    expect(spanDays(range)).toBe(days);
  });

  it('finds last month across a year boundary and in February', () => {
    expect(presetRange('last', '2027-01-15')).toEqual({ from: '2026-12-01', to: '2026-12-31' });
    expect(presetRange('last', '2028-03-01')).toEqual({ from: '2028-02-01', to: '2028-02-29' });
  });
});

describe('customRangeError', () => {
  it.each([
    [{ from: '2026-09-01', to: '2026-09-30' }, null],
    [{ from: '2026-09-01', to: '2026-10-31' }, null],
    [{ from: '', to: '2026-09-30' }, 'Pick both a start and an end date.'],
    [{ from: '2026-13-01', to: '2026-09-30' }, 'Pick both a start and an end date.'],
    [{ from: '2026-09-30', to: '2026-09-01' }, 'The start must be on or before the end.'],
    [{ from: '2026-10-04', to: '2026-10-10' }, 'The start cannot be in the future.'],
    [{ from: '2025-10-03', to: '2026-10-03' }, 'Pick at most 365 days.'],
  ])('%j → %s', (range, want) => {
    expect(customRangeError(range, TODAY)).toBe(want);
  });
});

describe('includesToday and queryRange', () => {
  it('asks only for days up to today', () => {
    const month = presetRange('month', TODAY);
    expect(includesToday(month, TODAY)).toBe(true);
    expect(queryRange(month, TODAY)).toEqual({ from: '2026-10-01', to: TODAY });

    const last = presetRange('last', TODAY);
    expect(includesToday(last, TODAY)).toBe(false);
    expect(queryRange(last, TODAY)).toEqual(last);
  });
});

describe('granularity', () => {
  it('draws days up to about three months, then weeks, then months', () => {
    expect(granularity(presetRange('90d', TODAY))).toBe('day');
    expect(granularity({ from: '2026-01-01', to: '2026-04-02' })).toBe('day'); // 92 days
    expect(granularity(presetRange('6m', TODAY))).toBe('week');
    expect(granularity(presetRange('1y', TODAY))).toBe('month');
  });
});

describe('fromFirstCost', () => {
  it('drops the $0 days before the first cost and keeps later ones', () => {
    const days = [day('2026-09-01', 0), day('2026-09-02', 1), day('2026-09-03', 0)];
    expect(fromFirstCost(days).map(d => d.date)).toEqual(['2026-09-02', '2026-09-03']);
    expect(fromFirstCost([day('2026-09-01', 0)])).toEqual([]);
  });
});

describe('bucketDays', () => {
  it('sums weeks from Monday to Sunday, marking the ones the range or today cuts short', () => {
    // Thu Sep 17 – Sat Oct 3: a cut week, two whole weeks, then the current week.
    const range = { from: '2026-09-17', to: TODAY };
    const days = Array.from({ length: 17 }, (_, i) => day(`2026-${i < 14 ? '09' : '10'}-${String(i < 14 ? 17 + i : i - 13).padStart(2, '0')}`, 1));
    const buckets = bucketDays(days, range, 'week', TODAY, '2026-09-17');

    expect(buckets.map(b => [b.start, b.end, b.cost, b.complete])).toEqual([
      ['2026-09-14', '2026-09-20', 4, false],
      ['2026-09-21', '2026-09-27', 7, true],
      ['2026-09-28', '2026-10-04', 6, false],
    ]);
  });

  it('marks the week still in progress when a range runs past today', () => {
    const buckets = bucketDays([], { from: '2026-06-01', to: '2026-10-31' }, 'week', TODAY, null);
    const current = buckets.find(b => b.start === '2026-09-28')!;
    const previous = buckets.find(b => b.start === '2026-09-21')!;

    expect(current.complete).toBe(false);
    expect(previous.complete).toBe(true);
  });

  it('keeps empty months in place and marks the one where the data starts', () => {
    const range = presetRange('1y', TODAY);
    const days = [day('2026-09-18', 2), day('2026-09-19', 3), day('2026-10-01', 4)];
    const buckets = bucketDays(days, range, 'month', TODAY, '2026-09-18');

    expect(buckets).toHaveLength(13);
    expect(buckets[0]).toMatchObject({ start: '2025-10-01', cost: 0, measuredDays: 0, complete: false });
    expect(buckets[1]).toMatchObject({ start: '2025-11-01', cost: 0, complete: false }); // before the data
    expect(buckets[11]).toMatchObject({ start: '2026-09-01', end: '2026-09-30', cost: 5, measuredDays: 2, complete: false });
    expect(buckets[12]).toMatchObject({ start: '2026-10-01', cost: 4, complete: false });

    const withHistory = bucketDays(days, range, 'month', TODAY, '2025-10-04');
    expect(withHistory[11].complete).toBe(true);
  });
});
