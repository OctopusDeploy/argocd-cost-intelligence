import { useCallback, useEffect, useRef, useState } from 'react';
import api, { AppContext, Rightsizing } from '../utils/api';

export interface UseRightsizingResult {
  rightsizing: Rightsizing | null;
  refetch: () => Promise<void>;
}

// useRightsizing fetches the VPA's recommendations for an application. They change over hours, not
// seconds, so it refreshes every refetchInterval (5 minutes) rather than with the tab's 30-second
// cycle. Rightsizing is advice on top of the costs: a failure leaves the last answer on screen, or
// nothing, and never an error, so the tab falls back to its CPU suggestion.
export const useRightsizing = (
  appName: string | undefined,
  appCtx: AppContext | undefined,
  refetchInterval: number = 5 * 60 * 1000
): UseRightsizingResult => {
  const [rightsizing, setRightsizing] = useState<Rightsizing | null>(null);
  // Only the latest request may update the state, so a slow answer for another app is dropped.
  const latest = useRef(0);

  const load = useCallback(async () => {
    if (!appName) return;
    const request = ++latest.current;
    try {
      const data = await api.getRightsizing(appName, appCtx);
      if (request === latest.current) setRightsizing(data);
    } catch (err) {
      console.error('Failed to fetch rightsizing:', err);
    }
  }, [appName, appCtx]);

  useEffect(() => {
    setRightsizing(null);
    load();
    const interval = setInterval(load, refetchInterval);
    return () => clearInterval(interval);
  }, [load, refetchInterval]);

  return { rightsizing, refetch: load };
};
