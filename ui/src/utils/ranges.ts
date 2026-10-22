import { DailyCost } from './api';

// A DayRange is a run of UTC calendar days ("2026-09-01"), both ends included.
export interface DayRange {
  from: string;
  to: string;
}

export type RangeId = '30d' | 'month' | 'last' | '90d' | '6m' | '1y' | 'custom';

export const DEFAULT_RANGE: RangeId = '30d';

// The presets in the order the picker shows them; Custom comes last.
export const PRESETS: Array<{ id: Exclude<RangeId, 'custom'>; label: string }> = [
  { id: '30d', label: '30 days' },
  { id: 'month', label: 'This month' },
  { id: 'last', label: 'Last month' },
  { id: '90d', label: '90 days' },
  { id: '6m', label: '6 months' },
  { id: '1y', label: '1 year' },
];

export const RANGE_IDS: readonly RangeId[] = [...PRESETS.map(preset => preset.id), 'custom'];

// MAX_RANGE_DAYS matches the backend's limit on /daily.
export const MAX_RANGE_DAYS = 365;

const DAY_MS = 86_400_000;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

const parse = (date: string) => Date.parse(`${date}T00:00:00Z`);
const format = (ms: number) => new Date(ms).toISOString().slice(0, 10);

export function addDays(date: string, days: number): string {
  return format(parse(date) + days * DAY_MS);
}

// spanDays counts the days in a range, both ends included.
export function spanDays({ from, to }: DayRange): number {
  return Math.round((parse(to) - parse(from)) / DAY_MS) + 1;
}

// monthStart and monthEnd bound the calendar month a day falls in.
export function monthStart(date: string): string {
  return `${date.slice(0, 7)}-01`;
}

export function monthEnd(date: string): string {
  const d = new Date(parse(date));
  return format(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0));
}

// presetRange resolves a preset against today. "This month" runs to the month's last day, so the
// forecast has somewhere to go; the rolling presets end today.
export function presetRange(id: Exclude<RangeId, 'custom'>, today: string): DayRange {
  switch (id) {
    case 'month':
      return { from: monthStart(today), to: monthEnd(today) };
    case 'last': {
      const end = addDays(monthStart(today), -1);
      return { from: monthStart(end), to: end };
    }
    case '90d':
      return { from: addDays(today, -89), to: today };
    case '6m':
      return { from: addDays(today, -182), to: today };
    case '1y':
      return { from: addDays(today, -(MAX_RANGE_DAYS - 1)), to: today };
    default:
      return { from: addDays(today, -29), to: today };
  }
}

// customRangeError says why a custom range cannot be shown, or returns null when it can.
export function customRangeError(range: DayRange, today: string): string | null {
  if (!DATE.test(range.from) || !DATE.test(range.to) || Number.isNaN(parse(range.from)) || Number.isNaN(parse(range.to))) {
    return 'Pick both a start and an end date.';
  }
  if (range.from > range.to) return 'The start must be on or before the end.';
  if (range.from > today) return 'The start cannot be in the future.';
  if (spanDays(range) > MAX_RANGE_DAYS) return `Pick at most ${MAX_RANGE_DAYS} days.`;
  return null;
}

// includesToday is whether a range reaches the day OpenCost is still measuring.
export function includesToday(range: DayRange, today: string): boolean {
  return range.from <= today && range.to >= today;
}

// queryRange is the part of a range there can be data for: nothing after today.
export function queryRange(range: DayRange, today: string): DayRange {
  return { from: range.from, to: range.to > today ? today : range.to };
}

export type Granularity = 'day' | 'week' | 'month';

// granularity picks how finely to draw a range: daily points up to about three months, then a bar
// per week, and a bar per month beyond about six months, where daily points are only noise.
export function granularity(range: DayRange): Granularity {
  const days = spanDays(range);
  if (days <= 92) return 'day';
  if (days <= 200) return 'week';
  return 'month';
}

// fromFirstCost drops the days before the first one with any cost. Before OpenCost has data for a
// namespace it reports exactly $0, which is not a measurement; a $0 day after costs began is real.
export function fromFirstCost(days: DailyCost[]): DailyCost[] {
  const first = days.findIndex(day => day.totalCost !== 0);
  return first < 0 ? [] : days.slice(first);
}

// completeDays is the part of a series asked for from `from` whose days were measured in full: the
// days before today, from the first one with any cost. When that first day comes after `from`,
// measuring began on it, when the pods started or where OpenCost's Prometheus retention ends, so
// it covers only part of the day and is left out too.
export function completeDays(days: DailyCost[], today: string, from: string): DailyCost[] {
  const measured = fromFirstCost(days.filter(day => day.date < today));
  return measured.length > 0 && measured[0].date > from ? measured.slice(1) : measured;
}

export interface PeriodBucket {
  // start and end are the whole week (Monday to Sunday) or calendar month.
  start: string;
  end: string;
  cost: number;
  // measuredDays is how many days in it have data.
  measuredDays: number;
  // complete is false when the range, today, or the start of the data cuts the period short.
  complete: boolean;
}

// bucketDays sums a daily series into one bucket per week or month overlapping the range, empty
// ones included, so the bars keep their places. firstDataDay is where the data starts; when that is
// after range.from, measuring began on it, so it is partial too, as completeDays has it.
export function bucketDays(
  days: DailyCost[],
  range: DayRange,
  unit: Exclude<Granularity, 'day'>,
  today: string,
  firstDataDay: string | null
): PeriodBucket[] {
  const startOf = (date: string) => {
    if (unit === 'month') return monthStart(date);
    const weekday = (new Date(parse(date)).getUTCDay() + 6) % 7; // Monday is 0
    return addDays(date, -weekday);
  };
  const endOf = (start: string) => (unit === 'month' ? monthEnd(start) : addDays(start, 6));

  const buckets: PeriodBucket[] = [];
  for (let start = startOf(range.from); start <= range.to; start = addDays(endOf(start), 1)) {
    const end = endOf(start);
    const beforeData = firstDataDay !== null && firstDataDay > range.from && start <= firstDataDay;
    const cut = start < range.from || end > range.to || end >= today || beforeData;
    buckets.push({ start, end, cost: 0, measuredDays: 0, complete: !cut });
  }
  for (const day of days) {
    if (day.date < range.from || day.date > range.to) continue;
    const bucket = buckets.find(b => b.start <= day.date && day.date <= b.end);
    if (!bucket) continue;
    bucket.cost += day.totalCost;
    bucket.measuredDays++;
  }
  return buckets;
}
