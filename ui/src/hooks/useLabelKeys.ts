import { useEffect, useState } from 'react';
import { fetchChargebackLabels } from '../utils/api';

interface LabelKeysState {
  // keys is null until the first answer arrives, then the label keys pods carry in the scope.
  keys: string[] | null;
  // failed is true when the backend could not list the keys, so the caller should fall back.
  failed: boolean;
}

// useLabelKeys lists the label keys in a namespace, or across the cluster when namespace is null.
// Changing the namespace keeps the previous keys on screen until the new ones arrive, so the
// suggestions do not blink.
export function useLabelKeys(namespace: string | null): LabelKeysState {
  const [state, setState] = useState<LabelKeysState>({ keys: null, failed: false });

  useEffect(() => {
    let cancelled = false;

    fetchChargebackLabels(namespace ?? undefined)
      .then(keys => {
        if (!cancelled) setState({ keys, failed: false });
      })
      .catch(() => {
        if (!cancelled) setState({ keys: null, failed: true });
      });

    return () => {
      cancelled = true;
    };
  }, [namespace]);

  return state;
}
