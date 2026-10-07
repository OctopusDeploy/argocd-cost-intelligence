import { CostData, PodCost } from './api';
import { ControllerGroup, groupPodsByController } from './podGroups';

// ArgoApplication is an Argo CD application and the resources it manages, as listed by
// Argo CD's /api/v1/applications.
export interface ArgoApplication {
  name: string;
  namespace: string;
  resources: Array<{ kind: string; name: string; namespace?: string }>;
}

export interface AppCost {
  // name is null for the group of pods that no visible application manages.
  name: string | null;
  // namespace is the Application resource's own namespace, used to link to it.
  namespace: string | null;
  // visible is false for an application that has cost records but that Argo CD did not list,
  // for example because the viewer may not see it. Its pods count as unmanaged.
  visible: boolean;
  controllers: ControllerGroup[];
  podCount: number;
  cpuCost: number;
  ramCost: number;
  totalCost: number;
  // share is the fraction (0–1) of the namespace's cost.
  share: number;
  measuredAt: string | null;
}

export interface NamespaceBreakdown {
  apps: AppCost[];
  // totalCost sums the namespace's pods, each counted once.
  totalCost: number;
  // latest is the most recent measurement; its pods, efficiency and totals describe the namespace.
  latest: CostData | null;
}

// Kinds OpenCost reports as a pod's controller, plus bare pods an application manages directly.
const POD_OWNER_KINDS = new Set(['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'CronJob', 'ReplicaSet', 'Pod']);

// splitNamespaceByApp counts each pod in a namespace once and assigns it to the Argo CD application
// that manages its controller. Every cost record measures the whole namespace, so pods come from the
// most recent record only. Pods whose controller no listed application manages form a final group
// with a null name. A pod claimed by several applications goes to the first by name.
export function splitNamespaceByApp(
  namespace: string,
  records: CostData[],
  argoApps: ArgoApplication[]
): NamespaceBreakdown {
  if (records.length === 0) return { apps: [], totalCost: 0, latest: null };

  const latest = records.reduce((newest, record) =>
    Date.parse(record.measuredAt) > Date.parse(newest.measuredAt) ? record : newest
  );

  const owners = [...records]
    .sort((a, b) => a.applicationName.localeCompare(b.applicationName))
    .map(record => {
      const argoApp = argoApps.find(
        app =>
          app.name === record.applicationName &&
          (!record.applicationNamespace || app.namespace === record.applicationNamespace)
      );
      const controllers = new Set(
        (argoApp?.resources ?? [])
          .filter(resource => POD_OWNER_KINDS.has(resource.kind) && (resource.namespace ?? namespace) === namespace)
          .map(resource => resource.name)
      );
      return { record, argoApp, controllers, pods: [] as PodCost[] };
    });

  const unmanaged: PodCost[] = [];
  for (const pod of latest.pods ?? []) {
    const controller = pod.controller || pod.name;
    const owner = owners.find(candidate => candidate.controllers.has(controller));
    (owner ? owner.pods : unmanaged).push(pod);
  }

  const toAppCost = (
    name: string | null,
    appNamespace: string | null,
    visible: boolean,
    pods: PodCost[],
    measuredAt: string | null
  ): AppCost => {
    const controllers = groupPodsByController(pods);
    return {
      name,
      namespace: appNamespace,
      visible,
      controllers,
      podCount: controllers.reduce((sum, group) => sum + group.pods.length, 0),
      cpuCost: controllers.reduce((sum, group) => sum + group.cpuCost, 0),
      ramCost: controllers.reduce((sum, group) => sum + group.ramCost, 0),
      totalCost: controllers.reduce((sum, group) => sum + group.totalCost, 0),
      share: 0,
      measuredAt,
    };
  };

  const apps = owners
    .map(owner =>
      toAppCost(
        owner.record.applicationName,
        owner.argoApp?.namespace ?? (owner.record.applicationNamespace || null),
        Boolean(owner.argoApp),
        owner.pods,
        owner.record.measuredAt
      )
    )
    .sort((a, b) => b.totalCost - a.totalCost);

  const unmanagedCost = toAppCost(null, null, true, unmanaged, null);
  if (unmanagedCost.podCount > 0) apps.push(unmanagedCost);

  const totalCost = apps.reduce((sum, app) => sum + app.totalCost, 0);
  for (const app of apps) {
    app.share = totalCost > 0 ? app.totalCost / totalCost : 0;
  }

  return { apps, totalCost, latest };
}
