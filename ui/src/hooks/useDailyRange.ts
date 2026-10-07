import { useCallback, useEffect, useRef, useState } from 'react';
import api, { AppContext, DailyCost } from '../utils/api';
import { DayRange, granularity } from '../utils/ranges';

export interface UseDailyRangeResult {
  loading: boolean;
  error: string | null;
  dailyCosts: DailyCost[] | null;
  refetch: () => Promise<void>;
}

interface State {
  key: string;
  dailyCosts: DailyCost[] | null;
  error: string | null;
  loading: boolean;
}

const IDLE: State = { key: '', dailyCosts: null, error: null, loading: false };

// useDailyRange fetches the daily series for a range of days, or nothing while range is null. A new
// range shows a spinner; a refresh of the same one keeps its data on screen. Ranges up to about three
// months that reach today refresh in the background every refetchInterval, like the rest of the tab;
// longer ones change by a fraction of a day in that time, and with the service's daily history off
// (DAILY_HISTORY_DAYS=0) each refresh would be a year-long OpenCost query. A background refresh that fails keeps the last good data; only a reload
// the viewer asked for reports the error.
export const useDailyRange = (
  appName: string | undefined,
  appCtx: AppContext | undefined,
  range: DayRange | null,
  today: string,
  refetchInterval: number = 30000
): UseDailyRangeResult => {
  const from = range?.from;
  const to = range?.to;
  const key = from && to && appName ? `${appName}|${from}|${to}` : '';
  const [state, setState] = useState<State>(IDLE);
  // Only the latest request may update the state, so a slow answer for an old range is dropped.
  const latest = useRef(0);

  const load = useCallback(
    async (background: boolean) => {
      if (!from || !to || !appName) return;
      const request = ++latest.current;
      if (!background) setState(current => ({ ...current, key, loading: true, error: null }));
      try {
        const data = await api.getDailyCosts(appName, { from, to }, appCtx);
        if (request !== latest.current) return;
        setState({ key, dailyCosts: data?.dailyCosts ?? [], error: null, loading: false });
      } catch (err) {
        if (request !== latest.current) return;
        const message = err instanceof Error ? err.message : 'Failed to fetch daily costs';
        setState(current => (background ? { ...current, loading: false } : { ...current, key, loading: false, error: message }));
      }
    },
    [key, appName, from, to, appCtx]
  );

  const live = Boolean(range && range.to >= today && granularity(range) === 'day');

  useEffect(() => {
    if (!key) return;
    setState(current => (current.key === key ? current : { ...IDLE, key }));
    load(false);
    if (!live) return;
    const interval = setInterval(() => load(true), refetchInterval);
    return () => clearInterval(interval);
  }, [key, live, load, refetchInterval]);

  const current = state.key === key ? state : { ...IDLE, loading: Boolean(key) };
  return {
    loading: current.loading,
    error: current.error,
    dailyCosts: current.dailyCosts,
    refetch: () => load(false),
  };
};
