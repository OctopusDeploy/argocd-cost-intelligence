import { useEffect, useState } from 'react';
import { ClusterOverhead, fetchClusterOverhead } from '../utils/api';

export function useClusterOverhead() {
  const [overhead, setOverhead] = useState<ClusterOverhead | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetchClusterOverhead()
      .then(data => {
        if (!cancelled) { setOverhead(data); setLoading(false); }
      })
      .catch(err => {
        if (!cancelled) { setError(err.message); setLoading(false); }
      });
    return () => { cancelled = true; };
  }, []);

  return { overhead, loading, error };
}
