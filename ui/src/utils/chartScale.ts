// niceTicks returns evenly spaced y-axis ticks from 0 up to a round ceiling at or above max,
// using steps of 1, 2, 2.5 or 5 × 10^k. The axis always starts at zero so small day-to-day
// changes are not exaggerated.
export function niceTicks(max: number, targetCount = 4): number[] {
  if (!(max > 0)) return [0, 1];
  const rough = max / targetCount;
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const step = ([1, 2, 2.5, 5, 10].find(s => rough / magnitude <= s) ?? 10) * magnitude;
  const count = Math.ceil(max / step - 1e-9);
  const decimals = Math.max(0, 1 - Math.floor(Math.log10(step)));
  return Array.from({ length: count + 1 }, (_, i) => Number((i * step).toFixed(decimals)));
}

const MAX_TICK_DECIMALS = 6;

// tickDecimals is how many decimals the labels of `ticks` need so that no two read the same: none for
// whole amounts, cents for ordinary ones (never one decimal: $0.2 reads oddly), and more when the steps are fractions of a cent (an app
// costing $0.003 a day would otherwise label every tick "$0.00").
export function tickDecimals(ticks: number[]): number {
  const fits = (decimals: number) => ticks.every(tick => Math.abs(tick - Number(tick.toFixed(decimals))) < 1e-9);
  if (fits(0)) return 0;
  for (let decimals = 2; decimals < MAX_TICK_DECIMALS; decimals++) {
    if (fits(decimals)) return decimals;
  }
  return MAX_TICK_DECIMALS;
}
