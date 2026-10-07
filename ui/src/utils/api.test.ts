import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

type Route = (url: string) => { status?: number; body: unknown } | undefined;

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

// stubFetch answers each request from the first route that knows it, and records every request.
function stubFetch(...routes: Route[]) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    for (const route of routes) {
      const answer = route(url);
      if (answer) return json(answer.body, answer.status);
    }
    throw new Error(`unexpected request ${url} ${JSON.stringify(init?.headers)}`);
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

const headersOf = (fetchMock: ReturnType<typeof stubFetch>, fragment: string) =>
  fetchMock.mock.calls.filter(([url]) => String(url).includes(fragment)).map(([, init]) => init?.headers);

// The viewer can see the application shop-web, which the borrowed headers name.
const anyApp: Route = url =>
  url.startsWith('/api/v1/applications?limit=1')
    ? { body: { items: [{ metadata: { namespace: 'argocd', name: 'shop-web' }, spec: { project: 'shop' } }] } }
    : undefined;

const config =
  (clusterScopeApp: { namespace: string; name: string } | null): Route =>
  url =>
    url.endsWith('/api/cost-analysis/config') ? { body: { clusterScopeApp } } : undefined;

// gateApp answers Argo CD's application list for the gate application with the given project, or
// with no items, as Argo CD does for an application the viewer may not get.
const gateApp =
  (project: string | null): Route =>
  url =>
    url.startsWith('/api/v1/applications?name=')
      ? { body: { items: project ? [{ metadata: { name: 'cost-intel' }, spec: { project } }] : [] } }
      : undefined;

const namespaces: Route = url =>
  url.endsWith('/api/cost-analysis/namespaces') ? { body: { namespaces: [] } } : undefined;

// Each test gets a fresh module, because the client caches headers for the session.
const loadApi = async () => {
  vi.resetModules();
  return import('./api');
};

describe('cluster-wide requests', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('name the gate application, not whichever application the viewer happens to see', async () => {
    const fetchMock = stubFetch(anyApp, config({ namespace: 'argocd', name: 'cost-intel' }), gateApp('platform'), namespaces);
    const { fetchNamespaces } = await loadApi();

    await fetchNamespaces();

    expect(headersOf(fetchMock, '/api/cost-analysis/config')).toEqual([
      { 'Argocd-Application-Name': 'argocd:shop-web', 'Argocd-Project-Name': 'shop' },
    ]);
    expect(headersOf(fetchMock, '/api/cost-analysis/namespaces')).toEqual([
      { 'Argocd-Application-Name': 'argocd:cost-intel', 'Argocd-Project-Name': 'platform' },
    ]);
    const lookup = new URL(String(fetchMock.mock.calls.find(([url]) => String(url).includes('?name='))![0]), 'http://x');
    expect(lookup.searchParams.get('name')).toBe('cost-intel');
    expect(lookup.searchParams.get('appNamespace')).toBe('argocd');
  });

  it('resolve the gate application once per session', async () => {
    const fetchMock = stubFetch(anyApp, config({ namespace: 'argocd', name: 'cost-intel' }), gateApp('platform'), namespaces);
    const { fetchNamespaces } = await loadApi();

    await fetchNamespaces();
    await fetchNamespaces();

    expect(headersOf(fetchMock, '/api/cost-analysis/config')).toHaveLength(1);
    expect(headersOf(fetchMock, '/api/cost-analysis/namespaces')).toHaveLength(2);
  });

  it('explain that cluster-wide costs are off when no gate application is configured', async () => {
    const fetchMock = stubFetch(anyApp, config(null));
    const { fetchNamespaces, ClusterAccessError } = await loadApi();

    const error = await fetchNamespaces().catch(err => err);

    expect(error).toBeInstanceOf(ClusterAccessError);
    expect(error.message).toContain('turned off');
    expect(error.message).toContain('costIntelligence.clusterScopeApp');
    expect(error.message).toContain('CLUSTER_SCOPE_APP');
    expect(error.message).toContain('<namespace>:<name>');
    expect(headersOf(fetchMock, '/api/cost-analysis/namespaces')).toHaveLength(0);
  });

  it('explain which application to ask for when the viewer cannot see the gate application', async () => {
    const fetchMock = stubFetch(anyApp, config({ namespace: 'argocd', name: 'cost-intel' }), gateApp(null));
    const { fetchNamespaces, ClusterAccessError } = await loadApi();

    const error = await fetchNamespaces().catch(err => err);

    expect(error).toBeInstanceOf(ClusterAccessError);
    expect(error.message).toContain('argocd/cost-intel');
    expect(headersOf(fetchMock, '/api/cost-analysis/namespaces')).toHaveLength(0);
  });

  it('try again after a failure, so access granted later takes effect without a page reload', async () => {
    let project: string | null = null;
    const fetchMock = stubFetch(
      anyApp,
      config({ namespace: 'argocd', name: 'cost-intel' }),
      url => gateApp(project)(url),
      namespaces
    );
    const { fetchNamespaces } = await loadApi();

    await expect(fetchNamespaces()).rejects.toThrow();
    project = 'platform';
    await fetchNamespaces();

    expect(headersOf(fetchMock, '/api/cost-analysis/config')).toHaveLength(2);
    expect(headersOf(fetchMock, '/api/cost-analysis/namespaces')).toHaveLength(1);
  });
});

describe('chargeback label keys', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const labels: Route = url =>
    url.includes('/api/cost-analysis/chargeback/labels') ? { body: { namespace: '', labelKeys: ['app', 'team'] } } : undefined;

  it('ask as the gate application, for the namespace in scope', async () => {
    const fetchMock = stubFetch(anyApp, config({ namespace: 'argocd', name: 'cost-intel' }), gateApp('platform'), labels);
    const { fetchChargebackLabels } = await loadApi();

    expect(await fetchChargebackLabels('payments')).toEqual(['app', 'team']);

    expect(headersOf(fetchMock, '/chargeback/labels')).toEqual([
      { 'Argocd-Application-Name': 'argocd:cost-intel', 'Argocd-Project-Name': 'platform' },
    ]);
    const request = new URL(String(fetchMock.mock.calls.find(([url]) => String(url).includes('/chargeback/labels'))![0]), 'http://x');
    expect(request.searchParams.get('namespace')).toBe('payments');
  });

  it('fail when the backend cannot list them', async () => {
    stubFetch(anyApp, config({ namespace: 'argocd', name: 'cost-intel' }), gateApp('platform'), url =>
      url.includes('/chargeback/labels') ? { status: 500, body: { error: 'failed to retrieve label keys' } } : undefined
    );
    const { fetchChargebackLabels } = await loadApi();

    await expect(fetchChargebackLabels()).rejects.toThrow();
  });
});

describe('application requests', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('ask for orphaned pods as the application, not by namespace', async () => {
    const fetchMock = stubFetch(url =>
      url.endsWith('/api/cost-analysis/shop-web/orphaned-pods') ? { body: { orphanedPods: [], totalWastedCost: 0 } } : undefined
    );
    const { fetchAppOrphanedPods } = await loadApi();

    await fetchAppOrphanedPods('shop-web', { namespace: 'argocd', name: 'shop-web', project: 'shop' });

    expect(headersOf(fetchMock, '/shop-web/orphaned-pods')).toEqual([
      { 'Argocd-Application-Name': 'argocd:shop-web', 'Argocd-Project-Name': 'shop' },
    ]);
  });
});

describe('getDailyCosts', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('asks for the last N days, or for a range of days', async () => {
    const fetchMock = stubFetch(url => (url.includes('/daily?') ? { body: { dailyCosts: [] } } : undefined));
    const { default: api } = await loadApi();

    await api.getDailyCosts('redis', 30);
    await api.getDailyCosts('redis', { from: '2026-09-01', to: '2026-09-30' });

    expect(fetchMock.mock.calls.map(([url]) => String(url))).toEqual([
      '/extensions/cost-intel/api/cost-analysis/redis/daily?days=30',
      '/extensions/cost-intel/api/cost-analysis/redis/daily?from=2026-09-01&to=2026-09-30',
    ]);
  });
});
