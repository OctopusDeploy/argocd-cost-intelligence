import { DailyCost } from './api';
import { fromFirstCost } from './ranges';

// MIN_TREND_DAYS is how many complete days a trend needs before it is worth drawing: fewer than a
// week cannot tell a slope from a weekend.
export const MIN_TREND_DAYS = 7;

const DAY_MS = 86_400_000;

export interface ForecastDay {
  date: string;
  cost: number;
}

export interface MonthForecast {
  // today is the UTC day still being measured; it and every day after it are forecast.
  today: string;
  monthStart: string;
  monthEnd: string;
  // spent is what the month's complete days cost; projected is the trend's cost for the rest of it.
  spent: number;
  projected: number;
  total: number;
  // slope is how much the trend's daily cost changes each day.
  slope: number;
  basedOnDays: number;
  // trend runs from the last complete day to the month's last day, one entry per day: where the
  // dashed line starts, then the days the projection is summed over.
  trend: ForecastDay[];
}

const dayNumber = (date: string) => Date.parse(`${date}T00:00:00Z`) / DAY_MS;
const dateOf = (day: number) => new Date(day * DAY_MS).toISOString().slice(0, 10);

// utcDay is the calendar day OpenCost is still measuring: days are UTC, as OpenCost reports them.
export function utcDay(now: Date = new Date()): string {
  return now.toISOString().slice(0, 10);
}

// trendDays is the part of a series, ordered oldest first, that a trend can be fitted to: the
// complete days, from the first one with any cost. OpenCost's day for today is still accumulating,
// and counting the $0 days before it had data would drag the trend down for anything it only
// recently started measuring.
export function trendDays(days: DailyCost[], today: string): DailyCost[] {
  return fromFirstCost(days.filter(day => day.date < today));
}

// forecastMonthEnd estimates what the current calendar month will cost: the month's complete days as
// measured, plus a least-squares trend over trendDays for each day left, today included. Days the
// series skips (an application none of whose controllers ran) count as costing nothing, and are
// placed by date, not by position. It returns null with fewer than MIN_TREND_DAYS days to fit.
export function forecastMonthEnd(days: DailyCost[], now: Date = new Date()): MonthForecast | null {
  const today = utcDay(now);
  const fitted = trendDays(days, today);
  if (fitted.length < MIN_TREND_DAYS) return null;

  const xs = fitted.map(day => dayNumber(day.date));
  const ys = fitted.map(day => day.totalCost);
  const meanX = xs.reduce((sum, x) => sum + x, 0) / xs.length;
  const meanY = ys.reduce((sum, y) => sum + y, 0) / ys.length;
  let sxy = 0;
  let sxx = 0;
  xs.forEach((x, i) => {
    sxy += (x - meanX) * (ys[i] - meanY);
    sxx += (x - meanX) ** 2;
  });
  const slope = sxx === 0 ? 0 : sxy / sxx;
  // A falling trend bottoms out at zero rather than projecting a refund.
  const trendAt = (day: number) => Math.max(0, meanY + slope * (day - meanX));

  const todayNumber = dayNumber(today);
  const monthStart = `${today.slice(0, 8)}01`;
  const monthEnd = dateOf(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 0) / DAY_MS);

  // The $0 days trendDays leaves out add nothing to what was spent.
  const spent = fitted.filter(day => day.date >= monthStart).reduce((sum, day) => sum + day.totalCost, 0);
  const lastComplete = xs[xs.length - 1];
  const trend: ForecastDay[] = [];
  let projected = 0;
  for (let day = lastComplete; day <= dayNumber(monthEnd); day++) {
    const cost = trendAt(day);
    if (day >= todayNumber) projected += cost;
    // Only the last complete day and the days still to come are drawn; any gap between is skipped.
    if (day === lastComplete || day >= todayNumber) trend.push({ date: dateOf(day), cost });
  }

  return {
    today,
    monthStart,
    monthEnd,
    spent,
    projected,
    total: spent + projected,
    slope,
    basedOnDays: fitted.length,
    trend,
  };
}
