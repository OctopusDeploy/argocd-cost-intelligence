import { DailyCost } from './api';

export interface DailySummary {
  average: number;
  total: number;
  highest: DailyCost;
  lowest: DailyCost;
  firstDay: string;
  lastDay: string;
}

// summarizeDaily computes the Trends statistics from a day-by-day series ordered oldest first,
// as the /daily endpoint returns it. It returns null for an empty series so callers render an
// empty state instead of NaN. Ties keep the earliest day.
export function summarizeDaily(days: DailyCost[]): DailySummary | null {
  if (days.length === 0) return null;
  let total = 0;
  let highest = days[0];
  let lowest = days[0];
  for (const day of days) {
    total += day.totalCost;
    if (day.totalCost > highest.totalCost) highest = day;
    if (day.totalCost < lowest.totalCost) lowest = day;
  }
  return {
    average: total / days.length,
    total,
    highest,
    lowest,
    firstDay: days[0].date,
    lastDay: days[days.length - 1].date,
  };
}
