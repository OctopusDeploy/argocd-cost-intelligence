import { useCallback, useEffect } from 'react';
import { useUrlState } from './useUrlState';
import {
  customRangeError,
  DayRange,
  DEFAULT_RANGE,
  presetRange,
  RANGE_IDS,
  RangeId,
} from '../utils/ranges';

export interface TrendRange {
  id: RangeId;
  range: DayRange;
  choose: (id: Exclude<RangeId, 'custom'>) => void;
  setCustom: (range: DayRange) => void;
}

// useTrendRange keeps the Trends tab's range in the URL: costRange names a preset, or "custom" with
// costFrom and costTo. Choosing a range is navigation, so Back undoes it; editing a custom range's
// dates refines it in place. A custom range the URL cannot express falls back to the default, and
// the URL is corrected.
export function useTrendRange(today: string): TrendRange {
  const [id, setId] = useUrlState<RangeId>('costRange', DEFAULT_RANGE, RANGE_IDS);
  const [from, setFrom] = useUrlState<string>('costFrom', '');
  const [to, setTo] = useUrlState<string>('costTo', '');

  const customValid = id === 'custom' && customRangeError({ from, to }, today) === null;
  const invalid = id === 'custom' && !customValid;

  useEffect(() => {
    if (!invalid) return;
    setId(DEFAULT_RANGE, 'replace');
    setFrom('', 'replace');
    setTo('', 'replace');
  }, [invalid, setId, setFrom, setTo]);

  const choose = useCallback(
    (next: Exclude<RangeId, 'custom'>) => {
      setId(next, 'push');
      setFrom('', 'replace');
      setTo('', 'replace');
    },
    [setId, setFrom, setTo]
  );

  const setCustom = useCallback(
    (next: DayRange) => {
      if (id !== 'custom') setId('custom', 'push');
      setFrom(next.from, 'replace');
      setTo(next.to, 'replace');
    },
    [id, setId, setFrom, setTo]
  );

  const effective: RangeId = invalid ? DEFAULT_RANGE : id;
  const range = effective === 'custom' ? { from, to } : presetRange(effective, today);
  return { id: effective, range, choose, setCustom };
}
