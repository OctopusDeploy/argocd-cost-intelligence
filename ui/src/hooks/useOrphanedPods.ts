import { useState, useEffect } from 'react';
import { AppContext, OrphanedPod, fetchAppOrphanedPods, fetchOrphanedPods } from '../utils/api';

// useOrphanedPods loads the orphaned pods in namespace. Given appName, it asks for them as that
// application, which any viewer of the application may do; otherwise it asks for the namespace,
// which only viewers of cluster-wide costs may do.
export function useOrphanedPods(
  namespace: string | null,
  appName?: string,
  appCtx?: AppContext
): {
  pods: OrphanedPod[];
  loading: boolean;
} {
  const [pods, setPods] = useState<OrphanedPod[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!namespace) {
      setPods([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    (appName ? fetchAppOrphanedPods(appName, appCtx) : fetchOrphanedPods(namespace))
      .then(data => {
        if (!cancelled) {
          setPods(data.orphanedPods ?? []);
        }
      })
      .catch(() => {
        if (!cancelled) setPods([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [namespace, appName, appCtx]);

  return { pods, loading };
}
