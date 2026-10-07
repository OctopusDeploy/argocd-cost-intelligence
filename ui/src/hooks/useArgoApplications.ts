import { useEffect, useState } from 'react';
import { fetchArgoApplications } from '../utils/argoApi';
import { ArgoApplication } from '../utils/namespaceApps';

export interface UseArgoApplicationsResult {
  applications: ArgoApplication[];
  loading: boolean;
  error: string | null;
}

// useArgoApplications loads the viewer's Argo CD applications once, to tell which app manages a pod.
export function useArgoApplications(): UseArgoApplicationsResult {
  const [state, setState] = useState<UseArgoApplicationsResult>({ applications: [], loading: true, error: null });

  useEffect(() => {
    let cancelled = false;
    fetchArgoApplications()
      .then(applications => {
        if (!cancelled) setState({ applications, loading: false, error: null });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({ applications: [], loading: false, error: err instanceof Error ? err.message : 'Failed to list applications' });
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}
