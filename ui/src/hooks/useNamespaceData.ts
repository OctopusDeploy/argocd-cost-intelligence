import { useCallback, useEffect, useRef, useState } from 'react';
import { CostData, fetchNamespaceApps, fetchNamespaces, NamespaceSummary } from '../utils/api';

export interface UseNamespaceDataResult {
  namespaces: NamespaceSummary[];
  namespacesLoading: boolean;
  namespacesError: string | null;
  selectedNs: string | null;
  setSelectedNs: (namespace: string) => void;
  // apps holds the latest cost record of each application in the selected namespace.
  apps: CostData[];
  appsLoading: boolean;
  appsError: string | null;
  reload: () => Promise<void>;
}

const errorMessage = (err: unknown, fallback: string) => (err instanceof Error ? err.message : fallback);

// How often the page picks up costs measured by a sync that happened while it was open. It matches
// the application view's own refresh, so both pages go stale at the same rate.
const REFRESH_MS = 30_000;

// useNamespaceData loads the namespace list, selects the most expensive namespace by default, and
// loads the selected namespace's cost records. The two load separately, so a slow namespace never
// hides the list, and a response for a namespace that is no longer selected is dropped.
// initialNamespace starts on a named namespace, such as one taken from the URL; it loads alongside
// the list, and gives way to the default if the list turns out not to have it.
export function useNamespaceData(initialNamespace: string | null = null): UseNamespaceDataResult {
  const [namespaces, setNamespaces] = useState<NamespaceSummary[]>([]);
  const [namespacesLoading, setNamespacesLoading] = useState(true);
  const [namespacesError, setNamespacesError] = useState<string | null>(null);
  const [selectedNs, setSelectedNs] = useState<string | null>(initialNamespace);
  const [apps, setApps] = useState<{ namespace: string | null; records: CostData[] }>({ namespace: null, records: [] });
  const [appsLoading, setAppsLoading] = useState(false);
  const [appsError, setAppsError] = useState<string | null>(null);
  const appsRequest = useRef(0);

  // background is a refresh the viewer did not ask for: it never shows a spinner, and if it fails it
  // leaves the last good data on screen rather than replacing the page with an error. The measured
  // time already on the page is what says how current the figures are.
  const loadNamespaces = useCallback(async (background = false) => {
    if (!background) {
      setNamespacesLoading(true);
      setNamespacesError(null);
    }
    try {
      const list = await fetchNamespaces();
      setNamespaces(list);
      setNamespacesError(null);
      setSelectedNs(current =>
        current && list.some(ns => ns.namespace === current) ? current : (list[0]?.namespace ?? null)
      );
    } catch (err) {
      if (!background) setNamespacesError(errorMessage(err, 'Failed to fetch namespaces'));
    } finally {
      if (!background) setNamespacesLoading(false);
    }
  }, []);

  const loadApps = useCallback(async (namespace: string | null, background = false) => {
    const request = ++appsRequest.current;
    if (!namespace) {
      setApps({ namespace: null, records: [] });
      return;
    }
    if (!background) {
      setAppsLoading(true);
      setAppsError(null);
    }
    try {
      const records = await fetchNamespaceApps(namespace);
      if (request === appsRequest.current) {
        setApps({ namespace, records });
        setAppsError(null);
      }
    } catch (err) {
      if (request === appsRequest.current && !background) {
        setApps({ namespace, records: [] });
        setAppsError(errorMessage(err, 'Failed to fetch namespace apps'));
      }
    } finally {
      if (request === appsRequest.current && !background) setAppsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadNamespaces();
  }, [loadNamespaces]);

  useEffect(() => {
    loadApps(selectedNs);
  }, [loadApps, selectedNs]);

  const reload = useCallback(async () => {
    await Promise.all([loadNamespaces(), loadApps(selectedNs)]);
  }, [loadNamespaces, loadApps, selectedNs]);

  // Costs change when Argo CD syncs an application, which can happen while this page is open.
  useEffect(() => {
    const interval = setInterval(() => {
      loadNamespaces(true);
      loadApps(selectedNs, true);
    }, REFRESH_MS);
    return () => clearInterval(interval);
  }, [loadNamespaces, loadApps, selectedNs]);

  return {
    namespaces,
    namespacesLoading,
    namespacesError,
    selectedNs,
    setSelectedNs,
    // Records of the previously selected namespace are never shown under a new selection.
    apps: apps.namespace === selectedNs ? apps.records : [],
    appsLoading,
    appsError,
    reload,
  };
}
