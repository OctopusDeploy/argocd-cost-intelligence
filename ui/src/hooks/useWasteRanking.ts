import { useCallback, useEffect, useState } from 'react';
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

// useWasteRanking loads the cluster's applications ranked by idle spend, and refreshes them in the
// background, keeping the last good ranking when a refresh fails.
export function useWasteRanking(): UseWasteRankingResult {
  const [ranking, setRanking] = useState<WasteRanking | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (background = false) => {
    if (!background) {
      setLoading(true);
      setError(null);
    }
    try {
      setRanking(await fetchWasteRanking());
      setError(null);
    } catch (err) {
      if (!background) setError(errorMessage(err));
    } finally {
      if (!background) setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const interval = setInterval(() => load(true), REFRESH_MS);
    return () => clearInterval(interval);
  }, [load]);

  const reload = useCallback(() => load(), [load]);

  return { ranking, loading, error, reload };
}
