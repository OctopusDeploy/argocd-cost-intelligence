import { useCallback, useEffect, useRef, useState } from 'react';
import { ApplicationListError, fetchWasteRanking, WasteRanking } from '../utils/api';

export interface UseWasteRankingResult {
  ranking: WasteRanking | null;
  loading: boolean;
  // error is worded for the viewer.
  error: string | null;
  reload: () => Promise<void>;
}

// Matches the namespace list's refresh, so the ranking and the namespaces beside it agree.
const REFRESH_MS = 30_000;

const errorMessage = (err: unknown) => {
  if (err instanceof ApplicationListError) return err.message;
  return `Couldn't load the ranking (${err instanceof Error ? err.message : 'unknown error'}).`;
};

// useWasteRanking loads the cluster's applications ranked by idle spend while enabled, and refreshes
// them in the background, keeping the last good ranking when a refresh fails. Disabled, it fetches
// nothing and reload does nothing, so viewers who never open the ranking don't load it.
export function useWasteRanking(enabled: boolean): UseWasteRankingResult {
  const [ranking, setRanking] = useState<WasteRanking | null>(null);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);
  // request counts the loads started, so only the latest one's answer is kept; it is bumped when
  // the hook is disabled or unmounted, so a load still in flight then is dropped.
  const request = useRef(0);
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;

  const load = useCallback(async (background = false) => {
    if (!enabledRef.current) return;
    const id = ++request.current;
    if (!background) {
      setLoading(true);
      setError(null);
    }
    try {
      const next = await fetchWasteRanking();
      if (id !== request.current) return;
      setRanking(next);
      setError(null);
    } catch (err) {
      if (id === request.current && !background) setError(errorMessage(err));
    } finally {
      if (id === request.current && !background) setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return undefined;
    // The ref itself never changes; only the count it holds does.
    const requests = request;
    load();
    const interval = setInterval(() => load(true), REFRESH_MS);
    return () => {
      clearInterval(interval);
      requests.current++;
    };
  }, [enabled, load]);

  const reload = useCallback(() => load(), [load]);

  return { ranking, loading, error, reload };
}
