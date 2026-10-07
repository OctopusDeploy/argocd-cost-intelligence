import { useState, useEffect } from 'react';
import { ChargebackEntry, fetchChargeback } from '../utils/api';

interface ChargebackState {
  entries: ChargebackEntry[];
  totalCost: number;
  loading: boolean;
  error: string | null;
}

export function useChargeback(
  labelKey: string | null,
  namespace: string | null
): ChargebackState {
  const [state, setState] = useState<ChargebackState>({
    entries: [],
    totalCost: 0,
    loading: false,
    error: null,
  });

  useEffect(() => {
    if (!labelKey) return;
    let cancelled = false;

    setState(s => ({ ...s, loading: true, error: null }));

    fetchChargeback(labelKey, namespace ?? undefined)
      .then(data => {
        if (!cancelled) {
          setState({
            entries: data.entries ?? [],
            totalCost: data.totalCost ?? 0,
            loading: false,
            error: null,
          });
        }
      })
      .catch((err: Error) => {
        if (!cancelled) {
          setState({ entries: [], totalCost: 0, loading: false, error: err.message });
        }
      });

    return () => { cancelled = true; };
  }, [labelKey, namespace]);

  return state;
}
