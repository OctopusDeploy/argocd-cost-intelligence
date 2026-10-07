import { useCallback, useEffect, useState } from 'react';
import api, { AppContext, CostData, CostTrend, DailyCost } from '../utils/api';

export interface UseCostDataResult {
  loading: boolean;
  error: string | null;
  data: CostData | null;
  trend: CostTrend | null;
  dailyCosts: DailyCost[] | null;
  refetch: () => Promise<void>;
}

export const useCostData = (
  appName: string | undefined,
  appCtx: AppContext | undefined,
  autoRefetch: boolean = true,
  refetchInterval: number = 30000
): UseCostDataResult => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [data, setData] = useState<CostData | null>(null);
  const [trend, setTrend] = useState<CostTrend | null>(null);
  const [dailyCosts, setDailyCosts] = useState<DailyCost[] | null>(null);

  const fetchData = useCallback(async () => {
    if (!appName) return;

    setLoading(true);
    setError(null);

    try {
      const [costData, trendData, dailyData] = await Promise.all([
        api.getLatestCost(appName, appCtx),
        api.getCostTrend(appName, 30, appCtx),
        api.getDailyCosts(appName, 30, appCtx),
      ]);

      setData(costData);
      setTrend(trendData);
      setDailyCosts(dailyData?.dailyCosts ?? null);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to fetch cost data';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [appName, appCtx]);

  useEffect(() => {
    fetchData();

    if (autoRefetch) {
      const interval = setInterval(fetchData, refetchInterval);
      return () => clearInterval(interval);
    }
  }, [fetchData, autoRefetch, refetchInterval]);

  return {
    loading,
    error,
    data,
    trend,
    dailyCosts,
    refetch: fetchData,
  };
};
