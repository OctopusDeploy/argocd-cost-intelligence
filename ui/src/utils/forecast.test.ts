import { describe, expect, it } from 'vitest';
import { DailyCost } from './api';
import { forecastMonthEnd, MIN_TREND_DAYS, trendDays } from './forecast';

const day = (date: string, totalCost: number): DailyCost => ({
  date,
  totalCost,
  cpuCost: 0,
  memoryCost: 0,
  networkCost: 0,
  storageCost: 0,
});

// series builds consecutive days from start, with cost(i) for the i-th day.
const series = (start: string, count: number, cost: (i: number) => number): DailyCost[] =>
  Array.from({ length: count }, (_, i) => {
    const date = new Date(Date.parse(`${start}T00:00:00Z`) + i * 86_400_000).toISOString().slice(0, 10);
    return day(date, cost(i));
  });

const at = (iso: string) => new Date(iso);

describe('forecastMonthEnd', () => {
  it('adds the month so far to a flat trend for every day left, today included', () => {
    // Sep 19 – Oct 17 complete at $10/day, Oct 18 still being measured.
    const days = [...series('2026-09-19', 29, () => 10), day('2026-10-18', 3)];
    const forecast = forecastMonthEnd(days, at('2026-10-18T09:30:00Z'))!;

    expect(forecast.today).toBe('2026-10-18');
    expect(forecast.monthStart).toBe('2026-10-01');
    expect(forecast.monthEnd).toBe('2026-10-31');
    expect(forecast.basedOnDays).toBe(29);
    expect(forecast.spent).toBeCloseTo(170); // Oct 1–17
    expect(forecast.projected).toBeCloseTo(140); // Oct 18–31
    expect(forecast.total).toBeCloseTo(310);
    expect(forecast.slope).toBeCloseTo(0);
  });

  it('leaves today out of the fit, so a partly measured day does not bend the trend down', () => {
    const complete = series('2026-09-19', 29, () => 10);
    const withToday = forecastMonthEnd([...complete, day('2026-10-18', 0.5)], at('2026-10-18T01:00:00Z'))!;
    const without = forecastMonthEnd(complete, at('2026-10-18T01:00:00Z'))!;

    expect(withToday.slope).toBeCloseTo(0);
    expect(withToday.total).toBeCloseTo(without.total);
  });

  it('follows a rising trend to the end of the month', () => {
    // $10 on Oct 1, rising $1 a day; today is Oct 11.
    const days = series('2026-09-21', 20, i => i);
    const forecast = forecastMonthEnd(days, at('2026-10-11T12:00:00Z'))!;

    expect(forecast.slope).toBeCloseTo(1);
    expect(forecast.spent).toBeCloseTo(10 + 11 + 12 + 13 + 14 + 15 + 16 + 17 + 18 + 19);
    // Oct 11 ($20) through Oct 31 ($40): 21 days averaging $30.
    expect(forecast.projected).toBeCloseTo(21 * 30);
    expect(forecast.trend[0]).toEqual({ date: '2026-10-10', cost: expect.closeTo(19) });
    expect(forecast.trend[forecast.trend.length - 1]).toEqual({ date: '2026-10-31', cost: expect.closeTo(40) });
  });

  it('never projects a negative cost when the trend falls', () => {
    const days = series('2026-10-01', 10, i => 100 - i * 20); // reaches zero on Oct 6, then negative
    const forecast = forecastMonthEnd(days, at('2026-10-11T00:00:00Z'))!;

    expect(forecast.slope).toBeLessThan(0);
    expect(forecast.trend.every(d => d.cost >= 0)).toBe(true);
    expect(forecast.projected).toBe(0);
  });

  it('places days by date, so skipped days neither stretch nor squash the trend', () => {
    // An app that ran every other day at $10, rising $2 between runs: $1 a day by date.
    const days = series('2026-09-01', 8, i => 10 + 2 * i).map((d, i) => ({
      ...d,
      date: new Date(Date.parse('2026-09-01T00:00:00Z') + i * 2 * 86_400_000).toISOString().slice(0, 10),
    }));
    const forecast = forecastMonthEnd(days, at('2026-09-20T00:00:00Z'))!;

    expect(forecast.slope).toBeCloseTo(1);
  });

  it('starts the trend line on the last complete day and skips any gap before today', () => {
    const days = series('2026-10-01', 7, () => 5); // last complete day Oct 7
    const forecast = forecastMonthEnd(days, at('2026-10-29T00:00:00Z'))!;

    expect(forecast.trend.map(d => d.date)).toEqual(['2026-10-07', '2026-10-29', '2026-10-30', '2026-10-31']);
    // Oct 8–28 are not in the series: nothing was measured, so nothing counts as spent.
    expect(forecast.spent).toBeCloseTo(35);
    expect(forecast.projected).toBeCloseTo(15);
  });

  it('handles the last day of a month and months of other lengths', () => {
    const feb = forecastMonthEnd(series('2027-02-01', 27, () => 1), at('2027-02-28T10:00:00Z'))!;
    expect(feb.monthEnd).toBe('2027-02-28');
    expect(feb.projected).toBeCloseTo(1);
    expect(feb.total).toBeCloseTo(28);
  });

  it('ignores the $0 days before OpenCost had data, so a new namespace gets its own trend', () => {
    const measured = series('2026-10-01', 10, () => 4);
    const now = at('2026-10-11T08:00:00Z');
    const withLeadingZeros = forecastMonthEnd([...series('2026-09-18', 13, () => 0), ...measured], now)!;

    expect(withLeadingZeros.basedOnDays).toBe(10);
    expect(withLeadingZeros.slope).toBeCloseTo(0);
    expect(withLeadingZeros.total).toBeCloseTo(forecastMonthEnd(measured, now)!.total);
  });

  it(`needs ${MIN_TREND_DAYS} complete days`, () => {
    const now = at('2026-10-18T12:00:00Z');
    const six = [...series('2026-10-11', MIN_TREND_DAYS - 1, () => 1), day('2026-10-18', 1)];
    expect(forecastMonthEnd(six, now)).toBeNull();
    expect(forecastMonthEnd(series('2026-10-10', MIN_TREND_DAYS, () => 1), now)).not.toBeNull();
    expect(forecastMonthEnd([], now)).toBeNull();
  });
});

describe('trendDays', () => {
  it('keeps complete days from the first with a cost, $0 days after it included', () => {
    const days = [day('2026-10-01', 0), day('2026-10-02', 0), day('2026-10-03', 2), day('2026-10-04', 0), day('2026-10-05', 3)];

    expect(trendDays(days, '2026-10-05').map(d => d.date)).toEqual(['2026-10-03', '2026-10-04']);
    expect(trendDays(days.slice(0, 2), '2026-10-05')).toEqual([]);
  });
});
