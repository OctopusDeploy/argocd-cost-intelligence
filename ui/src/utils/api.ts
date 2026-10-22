// API client for Cost Intelligence service

import type { DayRange } from './ranges';

// Calls go through ArgoCD's extension proxy (/extensions/cost-intel → backend service).
// This works from any browser context without exposing the backend externally.
const DEFAULT_BASE_URL = '/extensions/cost-intel';

export interface EfficiencyMetrics {
  cpuEfficiency: number;
  ramEfficiency: number;
  totalEfficiency: number;
  cpuCoresRequested: number;
  cpuCoresUsed: number;
  ramBytesRequested: number;
  ramBytesUsed: number;
  cpuIdleCost: number;
  ramIdleCost: number;
  cpuCoresLimit: number;
  ramBytesLimit: number;
  cpuCoresUsedMax: number;
  ramBytesUsedMax: number;
}

export interface PodCost {
  name: string;
  namespace: string;
  controller: string;
  controllerKind: string;
  cpuCost: number;
  ramCost: number;
  totalCost: number;
}

export interface CostBreakdown {
  cpuCost: number;
  memoryCost: number;
  storageCost: number;
  gpuCost: number;
  networkCost: number;
  networkCrossZoneCost?: number;
  networkCrossRegionCost?: number;
  networkInternetCost?: number;
  networkNatGatewayCost?: number;
  loadBalancerCost: number;
  totalCost: number;
  carbonCost?: number;
  sharedCost?: number;
  sharedCostBreakdown?: Record<string, number>;
}

export interface CostAnomaly {
  severity: 'warning' | 'alert';
  score: number;
  rollingAvg: number;
  recordCount: number;
  message: string;
}

export interface CloudServiceResource {
  name: string;
  resourceType: string;
  monthlyCost: number;
}

export interface CloudServiceEstimate {
  source: 'infracost' | 'terracost';
  totalMonthlyCost: number;
  currency: string;
  resources: CloudServiceResource[];
  pushedAt: string;
}

export interface CostData {
  applicationName: string;
  // applicationNamespace is the namespace of the Application resource itself;
  // deploymentNamespace is where the application deploys and what OpenCost measured.
  applicationNamespace?: string;
  deploymentNamespace?: string;
  // scope says what the cost fields cover on the application routes: only the pods of the application's
  // controllers ('application'), or its whole deployment namespace ('namespace', for records stored
  // before applications were measured on their own). Absent on the namespace routes, whose records
  // always cover the namespace.
  scope?: 'application' | 'namespace';
  totalMonthlyCost: number;
  measuredAt: string;
  // measuredOn says what took the measurement: a sync ('sync'), or the service when it started and found
  // the application had no record yet ('install'), in which case there is no syncTimestamp. Absent on
  // records stored before it was, which were all taken after a sync.
  measuredOn?: 'sync' | 'install';
  syncTimestamp?: string;
  // measuredHours is how much of its 24-hour window OpenCost had data for; monthly costs are projected
  // from it. Absent on records measured before it was stored.
  opencostOutput?: { allocationTotals: CostBreakdown; measuredHours?: number };
  resources?: ResourceCost[];
  efficiency?: EfficiencyMetrics;
  pods?: PodCost[];
  anomaly?: CostAnomaly;
  cloudEstimate?: CloudServiceEstimate;
}

export interface ResourceCost {
  name: string;
  kind: string;
  namespace: string;
  actualMonthlyCost: number;
  cpuCost: number;
  memoryCost: number;
  cpuUsage: string;
  memoryUsage: string;
}

export interface CostTrend {
  applicationName: string;
  trend: CostData[];
  statistics: {
    average: number;
    maximum: number;
    minimum: number;
    total: number;
  };
  count: number;
  days: number;
}

export interface DailyCost {
  date: string;
  totalCost: number;
  cpuCost: number;
  memoryCost: number;
  networkCost: number;
  storageCost: number;
}

export interface DailyCostData {
  applicationName: string;
  namespace: string;
  days: number;
  dailyCosts: DailyCost[];
}

export interface AppContext {
  namespace: string;
  name: string;
  project: string;
}

export interface NamespaceSummary {
  namespace: string;
  // totalCost and idleCost come from the namespace's most recent measurement, counted once. idleCost
  // is null when that measurement predates it, until an application in the namespace syncs again.
  totalCost: number;
  idleCost: number | null;
  appCount: number;
  measuredAt: string;
}

// NamespaceCostSummary is the average cost of a namespace over a number of days.
export interface NamespaceCostSummary {
  namespace: string;
  days: number;
  averageCost: number;
}

export interface NamespaceAppsResponse {
  apps: CostData[];
}

export interface OrphanedPod {
  name: string;
  namespace: string;
  controller: string;
  controllerKind: string;
  cpuCost: number;
  ramCost: number;
  storageCost: number;
  totalCost: number;
}

// Resources is an amount of CPU (cores) and memory (bytes).
export interface Resources {
  cpuCores: number;
  memoryBytes: number;
}

export interface ContainerRightsizing {
  name: string;
  // requests are per pod; memoryLimitBytes is 0 without a limit.
  requests: Resources;
  memoryLimitBytes: number;
  target: Resources;
  lowerBound: Resources;
  upperBound: Resources;
  // suggested is the VPA's target rounded up to 10m of CPU and 16Mi of memory.
  suggested: Resources;
  // unsettled: the VPA's upper bound is still far above its target, as it is while the VPA has
  // little history, so it says little about what the container needs.
  unsettled: boolean;
  // monthlyChange is what requesting suggested changes the monthly cost by: negative saves.
  monthlyChange: { cpu: number; memory: number };
}

// recommended: a recommendation-only VPA has advice; managed: a VPA sets the requests itself;
// pending: the VPA has no recommendation yet; none: no VPA covers the workload.
export type RightsizingStatus = 'recommended' | 'managed' | 'pending' | 'none';

export interface WorkloadRightsizing {
  kind: string;
  name: string;
  status: RightsizingStatus;
  updateMode?: string;
  lowConfidence: boolean;
  pods: number;
  containers: ContainerRightsizing[];
  monthlyChange: number;
}

export interface Rightsizing {
  // available is false when the cluster has no VPA, or the service may not read it.
  available: boolean;
  workloads: WorkloadRightsizing[];
  monthlySaving: number;
  monthlyIncrease: number;
}

export interface OrphanedPodsResponse {
  orphanedPods: OrphanedPod[];
  totalWastedCost: number;
}

export interface ChargebackEntry {
  labelValue: string;
  cpuCost: number;
  ramCost: number;
  storageCost: number;
  networkCost: number;
  lbCost: number;
  totalCost: number;
  efficiency: number;
}

export interface ClusterOverhead {
  controlPlaneCost: number;
  provider: string;
  cluster: string;
}

export interface DataQuality {
  pricingSource: 'real' | 'estimated' | 'unknown';
  message: string;
}

// ClusterAccessError explains, in words meant for the viewer, why cluster-wide cost data is not
// available to them.
export class ClusterAccessError extends Error {}

export interface ChargebackData {
  labelKey: string;
  namespace: string;
  entries: ChargebackEntry[];
  totalCost: number;
}

// AppWaste is one application's idle spend, from its own pods' measurement.
export interface AppWaste {
  applicationNamespace: string;
  applicationName: string;
  deploymentNamespace: string;
  measuredAt: string;
  monthlyCost: number;
  cpuCost: number;
  idleCost: number;
  efficiency: EfficiencyMetrics;
}

// WasteRanking ranks the applications deploying to this cluster by idle spend, most first.
// namespaceOnly and noPods count the applications it could not rank.
export interface WasteRanking {
  apps: AppWaste[];
  idleCost: number;
  namespaceOnly: number;
  noPods: number;
}

// ApplicationListError says the service couldn't list the Argo CD applications, so it couldn't tell
// which applications deploy to this cluster.
export class ApplicationListError extends Error {}

const APPLICATION_LIST_FAILED = 'failed to list Argo CD applications';

class CostIntelligenceAPI {
  private baseUrl: string;
  // undefined = not fetched yet; null = fetched but no apps found
  private cachedProxyAppRef: string | null | undefined = undefined;
  private clusterHeadersRequest: Promise<HeadersInit> | null = null;

  constructor(baseUrl: string = DEFAULT_BASE_URL) {
    this.baseUrl = baseUrl;
  }

  private headers(ctx?: AppContext): HeadersInit {
    if (!ctx) return {};
    return {
      'Argocd-Application-Name': `${ctx.namespace}:${ctx.name}`,
      'Argocd-Project-Name': ctx.project,
    };
  }

  // The ArgoCD extension proxy validates both Argocd-Application-Name and
  // Argocd-Project-Name on every request, even for system-level extensions that
  // have no app context. We resolve any valid app from ArgoCD's own
  // session-cookie-authenticated API and cache the result for the session.
  // The specific app doesn't matter — RBAC only checks that the user can see it — so these
  // headers only reach endpoints that reveal nothing about a particular application.
  private async anyAppHeaders(): Promise<HeadersInit> {
    if (this.cachedProxyAppRef === undefined) {
      try {
        // Argo CD 3.4 returns no spec at all for a nested field such as items.spec.project.
        const resp = await fetch(
          '/api/v1/applications?limit=1&fields=items.metadata.name,items.metadata.namespace,items.spec'
        );
        if (resp.ok) {
          const data = await resp.json();
          const app = data.items?.[0];
          this.cachedProxyAppRef = app
            ? `${app.metadata.namespace}:${app.metadata.name}:${app.spec?.project ?? 'default'}`
            : null;
        } else {
          this.cachedProxyAppRef = null;
        }
      } catch {
        this.cachedProxyAppRef = null;
      }
    }
    if (!this.cachedProxyAppRef) return {};
    const [ns, name, project] = this.cachedProxyAppRef.split(':');
    return {
      'Argocd-Application-Name': `${ns}:${name}`,
      'Argocd-Project-Name': project,
    };
  }

  // Cluster-wide endpoints only answer requests naming the Application the backend is configured
  // with, and Argo CD's proxy only forwards those for viewers allowed to get it. The backend says
  // which Application that is; Argo CD's own API gives its project, and returns nothing when the
  // viewer can't see it. A success is kept for the session; a failure is retried on the next call.
  private clusterHeaders(): Promise<HeadersInit> {
    if (!this.clusterHeadersRequest) {
      this.clusterHeadersRequest = this.resolveClusterHeaders().catch(error => {
        this.clusterHeadersRequest = null;
        throw error;
      });
    }
    return this.clusterHeadersRequest;
  }

  private async resolveClusterHeaders(): Promise<HeadersInit> {
    const response = await fetch(`${this.baseUrl}/api/cost-analysis/config`, { headers: await this.anyAppHeaders() });
    if (!response.ok) await this.throwWithBody(response);
    const { clusterScopeApp } = (await response.json()) as {
      clusterScopeApp: { namespace: string; name: string } | null;
    };
    if (!clusterScopeApp) {
      throw new ClusterAccessError(
        'cluster-wide costs are turned off, either on purpose or because no Argo CD application was found to decide ' +
          'who sees them; the service log says which. An administrator can choose one by setting the Helm value ' +
          'costIntelligence.clusterScopeApp (environment variable CLUSTER_SCOPE_APP) to an Argo CD application ' +
          'as <namespace>:<name>, for example argocd:my-app. People allowed to view that application will see these pages.'
      );
    }

    const { namespace, name } = clusterScopeApp;
    const params = new URLSearchParams({
      name,
      appNamespace: namespace,
      // Not items.spec.project: Argo CD 3.4 drops the whole spec for a nested field.
      fields: 'items.metadata.name,items.spec',
    });
    const apps = await fetch(`/api/v1/applications?${params}`);
    const project = apps.ok ? (await apps.json()).items?.[0]?.spec?.project : undefined;
    if (!project) {
      throw new ClusterAccessError(
        `cluster-wide costs are shown to viewers of the Argo CD application ${namespace}/${name}. Ask an administrator for access to it.`
      );
    }
    return {
      'Argocd-Application-Name': `${namespace}:${name}`,
      'Argocd-Project-Name': project,
    };
  }

  private async throwWithBody(response: Response): Promise<never> {
    let detail = response.statusText;
    try {
      const body = await response.text();
      if (body) detail = body;
    } catch { /* ignore */ }
    throw new Error(`${response.status}: ${detail}`);
  }

  async getLatestCost(appName: string, ctx?: AppContext): Promise<CostData | null> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/${encodeURIComponent(appName)}/actual`,
        {headers: this.headers(ctx)}
      );

      if (!response.ok) {
        if (response.status === 404) return null;
        await this.throwWithBody(response);
      }

      return await response.json();
    } catch (error) {
      console.error('Failed to fetch latest cost:', error);
      throw error;
    }
  }

  async getCostTrend(appName: string, days: number = 30, ctx?: AppContext): Promise<CostTrend | null> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/${encodeURIComponent(appName)}/trend?days=${days}`,
        {headers: this.headers(ctx)}
      );

      if (!response.ok) {
        if (response.status === 404) return null;
        await this.throwWithBody(response);
      }

      return await response.json();
    } catch (error) {
      console.error('Failed to fetch cost trend:', error);
      throw error;
    }
  }

  async getNamespaceSummary(namespace: string, days: number = 30): Promise<NamespaceCostSummary | null> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/namespace/${encodeURIComponent(namespace)}/summary?days=${days}`,
        { headers: await this.clusterHeaders() }
      );

      if (!response.ok) {
        if (response.status === 404) {
          return null;
        }
        throw new Error(`API error: ${response.statusText}`);
      }

      return await response.json();
    } catch (error) {
      console.error('Failed to fetch namespace summary:', error);
      throw error;
    }
  }

  // getDailyCosts fetches the last `window` days, or the UTC days of a DayRange.
  async getDailyCosts(
    appName: string,
    window: number | DayRange = 30,
    ctx?: AppContext
  ): Promise<DailyCostData | null> {
    try {
      const params = new URLSearchParams(
        typeof window === 'number' ? { days: String(window) } : { from: window.from, to: window.to }
      );
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/${encodeURIComponent(appName)}/daily?${params}`,
        { headers: this.headers(ctx) }
      );

      if (!response.ok) {
        if (response.status === 404) return null;
        await this.throwWithBody(response);
      }

      return await response.json();
    } catch (error) {
      console.error('Failed to fetch daily costs:', error);
      throw error;
    }
  }

  async listNamespaces(): Promise<NamespaceSummary[]> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/namespaces`,
        { headers: await this.clusterHeaders() }
      );
      if (!response.ok) {
        await this.throwWithBody(response);
      }
      const body = await response.json();
      return body.namespaces ?? [];
    } catch (error) {
      console.error('Failed to fetch namespaces:', error);
      throw error;
    }
  }

  async getOrphanedPods(namespace: string): Promise<OrphanedPodsResponse> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/namespace/${encodeURIComponent(namespace)}/orphaned-pods`,
        { headers: await this.clusterHeaders() }
      );
      if (!response.ok) {
        await this.throwWithBody(response);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to fetch orphaned pods:', error);
      throw error;
    }
  }

  // getRightsizing returns what the Vertical Pod Autoscaler recommends for the application's workloads.
  async getRightsizing(appName: string, ctx?: AppContext): Promise<Rightsizing> {
    const response = await fetch(`${this.baseUrl}/api/cost-analysis/${encodeURIComponent(appName)}/rightsizing`, {
      headers: this.headers(ctx),
    });
    if (!response.ok) await this.throwWithBody(response);
    return response.json();
  }

  // getAppOrphanedPods returns the orphaned pods in the namespace the application deploys to.
  async getAppOrphanedPods(appName: string, ctx?: AppContext): Promise<OrphanedPodsResponse> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/${encodeURIComponent(appName)}/orphaned-pods`,
        { headers: this.headers(ctx) }
      );
      if (!response.ok) {
        await this.throwWithBody(response);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to fetch orphaned pods:', error);
      throw error;
    }
  }

  async getChargeback(labelKey: string, namespace?: string): Promise<ChargebackData> {
    try {
      const params = new URLSearchParams({ label: labelKey });
      if (namespace) params.set('namespace', namespace);
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/chargeback?${params}`,
        { headers: await this.clusterHeaders() }
      );
      if (!response.ok) {
        await this.throwWithBody(response);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to fetch chargeback data:', error);
      throw error;
    }
  }

  // getChargebackLabels lists the label keys pods carry, in a namespace or across the cluster.
  async getChargebackLabels(namespace?: string): Promise<string[]> {
    try {
      const params = new URLSearchParams();
      if (namespace) params.set('namespace', namespace);
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/chargeback/labels?${params}`,
        { headers: await this.clusterHeaders() }
      );
      if (!response.ok) {
        await this.throwWithBody(response);
      }
      const body = await response.json();
      return body.labelKeys ?? [];
    } catch (error) {
      console.error('Failed to fetch chargeback label keys:', error);
      throw error;
    }
  }

  async getClusterOverhead(): Promise<ClusterOverhead> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/cluster/overhead`,
        { headers: await this.clusterHeaders() }
      );
      if (!response.ok) {
        await this.throwWithBody(response);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to fetch cluster overhead:', error);
      throw error;
    }
  }

  async getWasteRanking(): Promise<WasteRanking> {
    try {
      const response = await fetch(`${this.baseUrl}/api/cost-analysis/waste`, { headers: await this.clusterHeaders() });
      if (!response.ok) {
        // The service answers 503 with this error when it can't list the applications; a proxy in
        // front of it could answer 503 for its own reasons.
        const body = await response.text().catch(() => '');
        if (response.status === 503 && body.includes(APPLICATION_LIST_FAILED)) {
          throw new ApplicationListError(
            "Couldn't list the Argo CD applications, so apps on other clusters can't be left out."
          );
        }
        throw new Error(`${response.status}: ${body || response.statusText}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to fetch the waste ranking:', error);
      throw error;
    }
  }

  async getNamespaceApps(namespace: string): Promise<CostData[]> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/namespace/${encodeURIComponent(namespace)}/apps`,
        { headers: await this.clusterHeaders() }
      );
      if (!response.ok) {
        if (response.status === 404) return [];
        await this.throwWithBody(response);
      }
      const body = await response.json();
      return body.apps ?? [];
    } catch (error) {
      console.error('Failed to fetch namespace apps:', error);
      throw error;
    }
  }

  async getDataQuality(): Promise<DataQuality> {
    try {
      const response = await fetch(
        `${this.baseUrl}/api/cost-analysis/data-quality`,
        { headers: await this.anyAppHeaders() }
      );
      if (!response.ok) {
        return { pricingSource: 'unknown', message: '' };
      }
      return await response.json();
    } catch {
      return { pricingSource: 'unknown', message: '' };
    }
  }

  async isHealthy(): Promise<boolean> {
    try {
      const response = await fetch(`${this.baseUrl}/health`);
      return response.ok;
    } catch {
      return false;
    }
  }
}

export default new CostIntelligenceAPI();

const _api = new CostIntelligenceAPI();

export async function fetchNamespaces(): Promise<NamespaceSummary[]> {
  return _api.listNamespaces();
}

export async function fetchNamespaceApps(namespace: string): Promise<CostData[]> {
  return _api.getNamespaceApps(namespace);
}

export async function fetchOrphanedPods(namespace: string): Promise<OrphanedPodsResponse> {
  return _api.getOrphanedPods(namespace);
}

export async function fetchAppOrphanedPods(appName: string, ctx?: AppContext): Promise<OrphanedPodsResponse> {
  return _api.getAppOrphanedPods(appName, ctx);
}

export async function fetchChargeback(labelKey: string, namespace?: string): Promise<ChargebackData> {
  return _api.getChargeback(labelKey, namespace);
}

export async function fetchChargebackLabels(namespace?: string): Promise<string[]> {
  return _api.getChargebackLabels(namespace);
}

export async function fetchClusterOverhead(): Promise<ClusterOverhead> {
  return _api.getClusterOverhead();
}

export async function fetchDataQuality(): Promise<DataQuality> {
  return _api.getDataQuality();
}

export async function fetchWasteRanking(): Promise<WasteRanking> {
  return _api.getWasteRanking();
}
