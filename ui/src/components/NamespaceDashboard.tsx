import React, { useCallback, useEffect, useMemo } from 'react';
import { ChargebackView } from './ChargebackView';
import { PricingValue, pricingDetail } from './CostActual';
import { LEVEL_LABELS, levelIcon } from './EfficiencyCard';
import { InfoIcon } from './icons';
import { MeasuredLabel } from './MeasuredLabel';
import { NamespaceAppsTable } from './NamespaceAppsTable';
import { NamespaceList } from './NamespaceList';
import { ClusterIdle, IdleSpendView } from './IdleSpendView';
import { ErrorNotice, Notice } from './Notice';
import { OrphanedPodsNotice } from './OrphanedPodsNotice';
import { RecommendationCard } from './RecommendationCard';
import { framePanelStyle, PluginFrame } from './PluginFrame';
import { ReloadButton } from './ReloadButton';
import { Stat, StatStrip } from './StatStrip';
import { panelId, Tab, TabBar, tabId } from './TabBar';
import { useArgoApplications } from '../hooks/useArgoApplications';
import { useClusterOverhead } from '../hooks/useClusterOverhead';
import { useContainerWidth } from '../hooks/useContainerWidth';
import { useDataQuality } from '../hooks/useDataQuality';
import { UseNamespaceDataResult, useNamespaceData } from '../hooks/useNamespaceData';
import { UrlWrite, useUrlState } from '../hooks/useUrlState';
import { useWasteRanking } from '../hooks/useWasteRanking';
import { Tokens, useTokens } from '../theme/tokens';
import { ClusterOverhead, CostData, NamespaceSummary } from '../utils/api';
import { efficiencyLevel, recommendCpuRequest } from '../utils/costInsights';
import { formatMoney, formatPercent, pluralize } from '../utils/format';
import { PAIRED_ROW_MIN, SIDE_LIST_MIN } from '../utils/layout';
import { ArgoApplication, splitNamespaceByApp } from '../utils/namespaceApps';

type View = 'namespaces' | 'idle' | 'chargeback';

const VIEWS: Array<Tab<View>> = [
  { id: 'namespaces', label: 'Namespaces' },
  { id: 'idle', label: 'Idle spend' },
  { id: 'chargeback', label: 'Chargeback' },
];

const VIEW_IDS = VIEWS.map(view => view.id);

const ID_PREFIX = 'cost-intel-overview';

const PROVIDER_LABELS: Record<string, string> = { AWS: 'EKS', GCP: 'GKE', Azure: 'AKS' };

// NamespaceDashboard is the plugin's system-level page: cluster costs by namespace and application,
// applications ranked by idle spend, and chargeback by Kubernetes label.
export const NamespaceDashboard: React.FC = () => {
  // The open view and the selected namespace live in the URL, so this page can be linked to.
  const [view, setView] = useUrlState<View>('costTab', 'namespaces', VIEW_IDS);
  const [urlNamespace, setUrlNamespace] = useUrlState<string>('costNs', '');
  const namespaceData = useNamespaceData(urlNamespace || null);
  const waste = useWasteRanking(view === 'idle');
  const { reload: reloadNamespaces } = namespaceData;
  const { reload: reloadWaste } = waste;
  const reload = useCallback(async () => {
    await Promise.all([reloadNamespaces(), reloadWaste()]);
  }, [reloadNamespaces, reloadWaste]);

  // An app's namespace opens on the Namespaces tab. Only the tab switch is a step to go Back through.
  const openNamespace = useCallback(
    (namespace: string) => {
      setUrlNamespace(namespace, 'replace');
      setView('namespaces');
    },
    [setUrlNamespace, setView]
  );

  const latestMeasuredAt = namespaceData.namespaces.reduce<string | null>(
    (latest, ns) => (!latest || Date.parse(ns.measuredAt) > Date.parse(latest) ? ns.measuredAt : latest),
    null
  );

  return (
    <PluginFrame
      page
      status={
        <>
          {latestMeasuredAt && <MeasuredLabel measuredAt={latestMeasuredAt} />}
          <ReloadButton onReload={reload} />
        </>
      }
    >
      <TabBar tabs={VIEWS} active={view} onChange={setView} idPrefix={ID_PREFIX} label="Cost Intelligence overview" />

      <div id={panelId(ID_PREFIX, view)} role="tabpanel" aria-labelledby={tabId(ID_PREFIX, view)} style={framePanelStyle}>
        {view === 'namespaces' && (
          <NamespacesView
            data={namespaceData}
            urlNamespace={urlNamespace}
            onSelectNamespace={setUrlNamespace}
            onShowIdleApps={() => setView('idle')}
          />
        )}
        {view === 'idle' && (
          <IdleSpendView
            waste={waste}
            clusterIdle={clusterIdle(namespaceData.namespaces)}
            onSelectNamespace={openNamespace}
          />
        )}
        {view === 'chargeback' && <ChargebackView namespaces={namespaceData.namespaces} />}
      </div>
    </PluginFrame>
  );
};

interface NamespacesViewProps {
  data: UseNamespaceDataResult;
  urlNamespace: string;
  onSelectNamespace: (namespace: string, mode?: UrlWrite) => void;
  onShowIdleApps: () => void;
}

const NamespacesView: React.FC<NamespacesViewProps> = ({ data, urlNamespace, onSelectNamespace, onShowIdleApps }) => {
  const t = useTokens();
  const argo = useArgoApplications();
  const quality = useDataQuality();
  const { overhead, error: overheadError } = useClusterOverhead();
  const { namespaces, namespacesLoading, namespacesError, selectedNs, setSelectedNs } = data;
  // Until measured, and without layout, the list sits beside the namespace as on a normal screen.
  const [layoutRef, width] = useContainerWidth<HTMLDivElement>(Number.POSITIVE_INFINITY);
  const sideList = width >= SIDE_LIST_MIN;

  // The URL decides which namespace is read whenever it names a real one, so Back and Forward move
  // between namespaces. When it names none, or names one that is gone by the time the link is
  // opened, the list falls back to the most expensive namespace and the address bar is corrected to
  // match — by replacing, so the correction is not a step to go Back through.
  useEffect(() => {
    if (!selectedNs) return;
    if (urlNamespace !== '' && namespaces.some(ns => ns.namespace === urlNamespace)) {
      if (urlNamespace !== selectedNs) setSelectedNs(urlNamespace);
      return;
    }
    onSelectNamespace(selectedNs, 'replace');
  }, [selectedNs, urlNamespace, namespaces, onSelectNamespace, setSelectedNs]);

  const select = (namespace: string) => {
    setSelectedNs(namespace);
    onSelectNamespace(namespace);
  };

  if (namespacesLoading && namespaces.length === 0) return <Notice>Loading namespaces…</Notice>;
  if (namespacesError) return <ErrorNotice message={namespacesError} />;
  if (namespaces.length === 0) {
    return <Notice>No cost data yet. Costs appear after Argo CD syncs an application.</Notice>;
  }

  const clusterTotal = namespaces.reduce((sum, ns) => sum + ns.totalCost, 0);
  const appCount = namespaces.reduce((sum, ns) => sum + ns.appCount, 0);
  const selected = namespaces.find(ns => ns.namespace === selectedNs) ?? null;

  return (
    <div ref={layoutRef}>
      <StatStrip
        stats={[
          {
            label: 'Cluster monthly cost',
            value: formatMoney(clusterTotal),
            detail: `${pluralize(namespaces.length, 'namespace')} · ${pluralize(appCount, 'app')}`,
          },
          clusterIdleStat(namespaces, onShowIdleApps, t),
          controlPlaneStat(overhead, overheadError, t),
          { label: 'Pricing', value: <PricingValue quality={quality} t={t} />, detail: pricingDetail(quality) },
        ]}
      />

      {/* Narrow, the list stacks above the namespace rather than squeezing it into a sliver. */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: sideList ? '300px minmax(0, 1fr)' : 'minmax(0, 1fr)',
          gap: '16px',
          alignItems: 'start',
        }}
      >
        <NamespaceList namespaces={namespaces} selected={selectedNs} onSelect={select} clusterTotal={clusterTotal} />
        {selected && (
          <NamespaceDetail
            namespace={selected}
            clusterTotal={clusterTotal}
            records={data.apps}
            loading={data.appsLoading || argo.loading}
            error={data.appsError}
            argoApps={argo.applications}
            argoError={argo.error}
          />
        )}
      </div>
    </div>
  );
};

const NOT_MEASURED_IDLE: Stat = { label: 'Idle spend', value: '—', detail: 'Not measured yet' };
const CLUSTER_IDLE_NOT_MEASURED = "Not measured yet. Measured at each namespace's next sync";

// measuredIdle sums the idle cost of the namespaces it was measured for, and their cost alone, so
// the two figures agree while records from before it was measured remain. It is null when no
// namespace is measured.
function measuredIdle(namespaces: NamespaceSummary[]): { idle: number; cost: number; measured: number } | null {
  const measured = namespaces.filter(ns => ns.idleCost !== null);
  if (measured.length === 0) return null;
  return {
    idle: measured.reduce((sum, ns) => sum + (ns.idleCost ?? 0), 0),
    cost: measured.reduce((sum, ns) => sum + ns.totalCost, 0),
    measured: measured.length,
  };
}

// clusterIdle is what the Idle spend tab says of the cluster's idle spend.
function clusterIdle(namespaces: NamespaceSummary[]): ClusterIdle | null {
  const idle = measuredIdle(namespaces);
  return idle && { idle: idle.idle, measured: idle.measured, namespaces: namespaces.length };
}

// clusterIdleStat reports the measured idle cost as a share of the cluster, with a way to the apps
// it is in. The way stays while no namespace is measured, as the apps' own idle cost may already be.
function clusterIdleStat(namespaces: NamespaceSummary[], onShowApps: () => void, t: Tokens): Stat {
  const idle = measuredIdle(namespaces);
  const share = idle && formatPercent(idle.cost > 0 ? idle.idle / idle.cost : 0);
  return {
    label: 'Idle spend',
    value: idle ? formatMoney(idle.idle) : NOT_MEASURED_IDLE.value,
    detail: (
      <>
        {!idle
          ? CLUSTER_IDLE_NOT_MEASURED
          : idle.measured === namespaces.length
            ? `${share} of the cluster cost`
            : `${share} of the cost of the ${idle.measured} of ${namespaces.length} namespaces measured`}
        {' · '}
        <button type="button" onClick={onShowApps} style={linkButton(t)}>
          See which apps
        </button>
      </>
    ),
  };
}

const linkButton = (t: Tokens): React.CSSProperties => ({
  padding: 0,
  border: 'none',
  backgroundColor: 'transparent',
  color: t.accentText,
  fontFamily: 'inherit',
  fontSize: 'inherit',
  fontWeight: 500,
  cursor: 'pointer',
});

function namespaceIdleStat(namespace: NamespaceSummary): Stat {
  if (namespace.idleCost === null) return { ...NOT_MEASURED_IDLE, detail: 'Not measured yet. Measured at the next sync' };
  return {
    label: 'Idle spend',
    value: formatMoney(namespace.idleCost),
    detail: `${formatPercent(namespace.totalCost > 0 ? namespace.idleCost / namespace.totalCost : 0)} of the namespace cost`,
  };
}

// controlPlaneStat reports the managed control-plane fee, and says why when it cannot.
function controlPlaneStat(overhead: ClusterOverhead | null, error: string | null, t: Tokens): Stat {
  const label = 'Control plane fee';
  if (error) {
    return {
      label,
      value: <IconValue icon={<InfoIcon color={t.textMuted} size={18} />} text="Not available" />,
      detail: "OpenCost didn't return cluster assets",
    };
  }
  if (!overhead) return { label, value: '—', detail: 'Checking the cluster…' };
  if (overhead.controlPlaneCost <= 0) return { label, value: 'None', detail: 'Self-managed control plane' };
  const provider = PROVIDER_LABELS[overhead.provider] ?? overhead.provider;
  return {
    label,
    value: formatMoney(overhead.controlPlaneCost),
    detail: [provider, overhead.cluster, 'not in namespace costs'].filter(Boolean).join(' · '),
  };
}

const IconValue: React.FC<{ icon: React.ReactNode; text: string }> = ({ icon, text }) => (
  <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
    {icon}
    {text}
  </span>
);

interface NamespaceDetailProps {
  namespace: NamespaceSummary;
  clusterTotal: number;
  records: CostData[];
  loading: boolean;
  error: string | null;
  argoApps: ArgoApplication[];
  argoError: string | null;
}

const NamespaceDetail: React.FC<NamespaceDetailProps> = ({
  namespace,
  clusterTotal,
  records,
  loading,
  error,
  argoApps,
  argoError,
}) => {
  const t = useTokens();
  // Wide enough, the namespace's figures and its recommendation share a row; both are short.
  const [layoutRef, width] = useContainerWidth<HTMLDivElement>(0);
  const breakdown = useMemo(
    () => splitNamespaceByApp(namespace.namespace, records, argoApps),
    [namespace.namespace, records, argoApps]
  );
  const efficiency = breakdown.latest?.efficiency;
  const level = efficiency ? efficiencyLevel(efficiency.totalEfficiency) : null;
  const recommendation = recommendCpuRequest(efficiency, breakdown.latest?.opencostOutput?.allocationTotals?.cpuCost ?? 0);
  const controllerCount = breakdown.apps.reduce((sum, app) => sum + app.controllers.length, 0);

  let applications: React.ReactNode;
  if (loading && records.length === 0) applications = <Notice>Loading applications…</Notice>;
  else if (error) applications = <ErrorNotice message={error} />;
  else if (breakdown.apps.length === 0) applications = <Notice>No cost records for this namespace yet.</Notice>;
  else applications = <NamespaceAppsTable apps={breakdown.apps} totalCost={breakdown.totalCost} />;

  const paired = recommendation !== null && width >= PAIRED_ROW_MIN;
  const stats = (
    <StatStrip
      flush={paired}
      stats={[
        {
          label: 'Monthly cost',
          value: formatMoney(namespace.totalCost),
          detail: `${formatPercent(clusterTotal > 0 ? namespace.totalCost / clusterTotal : 0)} of the cluster`,
        },
        namespaceIdleStat(namespace),
        {
          label: 'Efficiency',
          value:
            efficiency && level ? (
              <IconValue icon={levelIcon(level, t)} text={formatPercent(efficiency.totalEfficiency)} />
            ) : (
              '—'
            ),
          detail: level ? `${LEVEL_LABELS[level]} use of requests` : 'Not measured',
        },
      ]}
    />
  );

  return (
    <div ref={layoutRef} style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '12px', marginBottom: '16px' }}>
        <h3 style={{ margin: 0, color: t.textStrong, fontSize: '18px', fontWeight: 500, lineHeight: '24px' }}>
          {namespace.namespace}
        </h3>
        <span style={{ color: t.textMuted, fontSize: '12.8px' }}>
          {pluralize(namespace.appCount, 'app')}
          {breakdown.latest ? ` · ${pluralize(controllerCount, 'controller')}` : ''}
        </span>
      </div>

      {paired && recommendation ? (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'minmax(0, 3fr) minmax(0, 2fr)',
            alignItems: 'stretch',
            gap: '16px',
            marginBottom: '16px',
          }}
        >
          {stats}
          <RecommendationCard recommendation={recommendation} namespaceName={namespace.namespace} flush />
        </div>
      ) : (
        <>
          {stats}
          {recommendation && <RecommendationCard recommendation={recommendation} namespaceName={namespace.namespace} />}
        </>
      )}

      {argoError && (
        <div style={{ marginBottom: '16px' }}>
          <Notice>Couldn&apos;t list Argo CD applications ({argoError}), so pods can&apos;t be matched to apps.</Notice>
        </div>
      )}

      {applications}

      <OrphanedPodsNotice namespace={namespace.namespace} />
    </div>
  );
};
