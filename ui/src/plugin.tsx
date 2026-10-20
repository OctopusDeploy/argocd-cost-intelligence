import React, { useMemo } from 'react';
import { CostPanel } from './components/CostPanel';
import { RemoteClusterPanel } from './components/RemoteClusterPanel';
import { AppContext } from './utils/api';
import { Destination, otherCluster } from './utils/destination';

// Argo CD Plugin Interface
export const plugin = {
  name: 'argocd-cost-intelligence',
  version: '1.0.0',
  description: 'Display cost metrics for Argo CD applications',
};

// This component renders in the Application details view
// It receives props from Argo CD's plugin system
// Kinds that directly own pods — used to filter status.resources to controllers.
const POD_OWNER_KINDS = new Set(['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'CronJob', 'ReplicaSet']);

export interface ApplicationDetailsTabProps {
  application: {
    metadata: {
      name: string;
      namespace: string;
    };
    spec?: {
      project?: string;
      destination?: Destination;
    };
    status?: {
      resources?: Array<{ kind: string; name: string; namespace?: string }>;
    };
  };
}

export const ArgoCDCostIntelligenceTab: React.FC<ApplicationDetailsTabProps> = ({
  application,
}) => {
  const appName = application?.metadata?.name;
  const appNs = application?.metadata?.namespace;
  const appProject = application?.spec?.project;

  // Stable reference — prevents useEffect in useCostData from re-firing on every
  // ArgoCD parent re-render (application status polling, etc.)
  const appCtx = useMemo<AppContext>(
    () => ({
      name: appName ?? '',
      namespace: appNs ?? 'argocd',
      project: appProject ?? 'default',
    }),
    [appName, appNs, appProject]
  );

  // Derive the set of controller names this ArgoCD app manages so the Resources
  // tab can filter OpenCost pod data down to just this app's resources.
  const managedControllers = useMemo<Set<string>>(() => {
    const resources = application?.status?.resources ?? [];
    return new Set(
      resources.filter(r => POD_OWNER_KINDS.has(r.kind)).map(r => r.name)
    );
  }, [application?.status?.resources]);

  if (!appName) {
    return <div>Unable to determine application name</div>;
  }

  // OpenCost measures only the cluster the service runs in, so an application deploying elsewhere has
  // no cost to show, and its views would fetch nothing useful.
  const cluster = otherCluster(application.spec?.destination);
  if (cluster) {
    return <RemoteClusterPanel cluster={cluster} />;
  }

  return <CostPanel appName={appName} appCtx={appCtx} managedControllers={managedControllers} />;
};

// Export default for Argo CD to detect
export default {
  component: ArgoCDCostIntelligenceTab,
  name: 'Cost Intelligence',
  key: 'cost-intelligence',
};
