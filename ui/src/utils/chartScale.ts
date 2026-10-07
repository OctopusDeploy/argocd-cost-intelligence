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
