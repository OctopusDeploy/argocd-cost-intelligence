import React from 'react';
import { AnomalyNotice } from './AnomalyNotice';
import { ChangeDriversCard } from './ChangeDriversCard';
import { CostBreakdownCard } from './CostBreakdownCard';
import { EfficiencyCard } from './EfficiencyCard';
import { CheckIcon, InfoIcon, TrendIcon, WarningIcon } from './icons';
import { ErrorNotice, Notice } from './Notice';
import { RecommendationCard } from './RecommendationCard';
import { RightsizingCard } from './RightsizingCard';
import { StatStrip } from './StatStrip';
import { useContainerWidth } from '../hooks/useContainerWidth';
import { useDataQuality } from '../hooks/useDataQuality';
import { cardStyle, Tokens, useTokens } from '../theme/tokens';
import { CostData, CostTrend, DataQuality, Rightsizing } from '../utils/api';
import {
  changeDrivers,
  ChangeDrivers,
  costChange,
  CostChange,
  NOT_TIED_TO_A_POD,
  asApplication,
  measuredAtInstall,
  previousMeasurement,
  recommendCpuRequest,
  SharedNamespace,
  sharedNamespace,
} from '../utils/costInsights';
import { formatMoney, formatPercent, formatSignedPercent, pluralize } from '../utils/format';
import { THREE_CARD_COLUMNS_MIN } from '../utils/layout';
import { hasAdvice } from '../utils/rightsizing';

interface CostActualProps {
  data: CostData | null;
  trend: CostTrend | null;
  loading: boolean;
  error: string | null;
  onShowResources?: () => void;
  // managedControllers names the controllers Argo CD lists for this application.
  managedControllers?: Set<string>;
  // rightsizing is the Vertical Pod Autoscaler's advice, which replaces the CPU suggestion when it has any.
  rightsizing?: Rightsizing | null;
}

export const CostActual: React.FC<CostActualProps> = ({
  data,
  trend,
  loading,
  error,
  onShowResources,
  managedControllers,
  rightsizing = null,
}) => {
  const t = useTokens();
  const quality = useDataQuality();
  // Measured before the early returns: hooks must run on every render. Until the view is measured it
  // uses the two-column layout.
  const [layoutRef, width] = useContainerWidth<HTMLDivElement>(0);

  if (loading && !data) return <Notice>Loading cost data…</Notice>;
  if (error) return <ErrorNotice message={error} />;
  if (!data) {
    return (
      <Notice>
        No cost data yet. Cost Intelligence measures this application once OpenCost has a few minutes of data for
        its namespace, and again after each sync. If nothing shows up, sync the application.
      </Notice>
    );
  }

  // The backend measures the application's own pods. Records stored before it did measure the whole
  // deployment namespace: when the application shares it, the cost figures count only the pods its
  // controllers own, and efficiency, idle spend, the breakdown and the recommendation, which exist only
  // for the namespace as a whole, say so.
  const measuredAlone = data.scope === 'application';
  const shared = measuredAlone ? null : sharedNamespace(data, managedControllers);
  const latest = asApplication(data, managedControllers) ?? data;
  // A measurement taken when the service started has no sync before it to compare with.
  const atInstall = measuredAtInstall(data);
  const earlier = atInstall ? null : previousMeasurement(data, trend?.trend ?? []);
  // An application's own cost is only compared with an earlier cost of its own, never its namespace's.
  const previous = earlier && (asApplication(earlier, managedControllers) ?? (measuredAlone ? null : earlier));
  const kubernetes = latest.totalMonthlyCost;
  const cloud = data.cloudEstimate?.totalMonthlyCost ?? 0;
  const change = costChange(latest, previous);
  const drivers = previous ? changeDrivers(latest, previous) : null;
  const efficiency = data.efficiency;
  const idle = efficiency ? efficiency.cpuIdleCost + efficiency.ramIdleCost : null;
  const recommendation = recommendCpuRequest(
    efficiency,
    data.opencostOutput?.allocationTotals?.cpuCost ?? 0,
    data.opencostOutput?.measuredHours
  );
  const controllers = new Set((data.pods ?? []).map(pod => pod.controller || pod.name));
  const controllerName = !shared && controllers.size === 1 ? [...controllers][0] : null;
  // In a shared namespace the advice is about every pod in it, which the Resources tab does not list.
  const showResources = shared ? undefined : onShowResources;
  // On a wide screen the cards sit in three columns and the recommendation becomes the last of them,
  // instead of a banner stretched across the page. Otherwise the cards take two columns, or one when
  // narrow, and the recommendation stays a banner above them.
  const threeColumns = width >= THREE_CARD_COLUMNS_MIN;
  // The VPA's per-container advice, when it has any, replaces the CPU suggestion: one recommendation,
  // never two that disagree.
  const advice = hasAdvice(rightsizing)
    ? (layout: 'banner' | 'column') => (
        <RightsizingCard
          rightsizing={rightsizing!}
          onShowResources={showResources}
          layout={layout}
          flush={layout === 'column'}
        />
      )
    : recommendation
      ? (layout: 'banner' | 'column') => (
          <RecommendationCard
            recommendation={recommendation}
            controllerName={controllerName}
            namespaceName={shared?.namespace}
            onShowResources={showResources}
            layout={layout}
            flush={layout === 'column'}
          />
        )
      : null;

  return (
    <div ref={layoutRef}>
      {data.anomaly && <AnomalyNotice anomaly={data.anomaly} />}
      {shared && <SharedNamespaceNote shared={shared} />}

      <StatStrip
        stats={[
          {
            label: 'Monthly cost',
            value: formatMoney(kubernetes + cloud),
            detail:
              cloud > 0
                ? `Kubernetes ${formatMoney(kubernetes)} + cloud ${formatMoney(cloud)}`
                : shared
                  ? "This app's workloads"
                  : 'Kubernetes workloads',
          },
          {
            label: 'Since last sync',
            value: change ? <ChangeValue change={change} t={t} /> : '—',
            detail: change ? changeDetail(change, drivers) : atInstall ? 'Measured at install' : 'No earlier measurement',
          },
          {
            label: 'Idle spend',
            value: idle === null ? '—' : formatMoney(idle),
            detail: idleDetail(idle, data.totalMonthlyCost, shared),
          },
          { label: 'Pricing', value: <PricingValue quality={quality} t={t} />, detail: pricingDetail(quality) },
        ]}
      />

      {advice && !threeColumns && advice('banner')}

      {/* min() keeps a single column inside a container narrower than 420px instead of overflowing it. */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: threeColumns ? 'repeat(3, minmax(0, 1fr))' : 'repeat(auto-fit, minmax(min(420px, 100%), 1fr))',
          alignItems: 'start',
          gap: '16px',
        }}
      >
        <CostBreakdownCard data={data} namespace={shared?.namespace} />
        {efficiency ? (
          <EfficiencyCard efficiency={efficiency} namespace={shared?.namespace} />
        ) : (
          <Notice>Efficiency metrics aren&apos;t available for this measurement.</Notice>
        )}
        {advice && threeColumns && advice('column')}
        {drivers && <ChangeDriversCard drivers={drivers} />}
      </div>

      <p style={{ margin: '16px 0 0', color: t.textMuted, fontSize: '12px' }}>
        {atInstall
          ? 'These costs were measured with OpenCost when the service started, before the application synced. They are measured again after each sync.'
          : 'Costs are measured after each sync with OpenCost.'}
      </p>
    </div>
  );
};

function idleDetail(idle: number | null, namespaceCost: number, shared: SharedNamespace | null): string {
  if (idle === null) return 'Efficiency not measured';
  const share = formatPercent(namespaceCost > 0 ? idle / namespaceCost : 0);
  return shared ? `${share} of the ${shared.namespace} namespace` : `${share} of Kubernetes cost`;
}

// SharedNamespaceNote explains which figures are the application's and which are its namespace's.
const SharedNamespaceNote: React.FC<{ shared: SharedNamespace }> = ({ shared }) => {
  const t = useTokens();
  return (
    <div
      style={{
        ...cardStyle(t),
        display: 'flex',
        alignItems: 'flex-start',
        gap: '12px',
        marginBottom: '16px',
        padding: '14px 24px',
        color: t.text,
        fontSize: '13px',
        lineHeight: '20px',
      }}
    >
      <span style={{ display: 'flex', marginTop: '3px' }}>
        <InfoIcon color={t.textMuted} />
      </span>
      <span>
        <span style={{ color: t.textStrong, fontWeight: 500 }}>
          This app shares the {shared.namespace} namespace with {pluralize(shared.otherWorkloads, 'other workload')}
        </span>{' '}
        ({formatMoney(shared.cost)}/mo in total). Monthly cost and Since last sync count this app&apos;s pods only;
        idle spend, efficiency, the breakdown and the recommendation cover the whole namespace.
      </span>
    </div>
  );
};

// changeDetail says where the cost moved from and, when the largest change went the same way as the
// total, what moved it most.
function changeDetail(change: CostChange, drivers: ChangeDrivers | null): string {
  if (change.fraction === 0) return `Kubernetes, unchanged at ${formatMoney(change.previous)}`;
  const moved = `Kubernetes, ${change.fraction < 0 ? 'down' : 'up'} from ${formatMoney(change.previous)}`;
  const top = drivers?.drivers[0];
  if (!top || Math.sign(top.delta) !== Math.sign(change.fraction)) return moved;
  return `${moved}, mostly ${top.key === NOT_TIED_TO_A_POD ? 'costs not tied to a pod' : top.name}`;
}

// A falling cost is good news and a rising one is worth a look, so the icon colour follows that.
const ChangeValue: React.FC<{ change: CostChange; t: Tokens }> = ({ change, t }) => (
  <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
    {change.fraction !== 0 && (
      <TrendIcon direction={change.fraction < 0 ? 'down' : 'up'} color={change.fraction < 0 ? t.success : t.danger} />
    )}
    {formatSignedPercent(change.fraction)}
  </span>
);

const PRICING_LABELS: Record<DataQuality['pricingSource'], string> = {
  real: 'Real',
  estimated: 'Estimated',
  unknown: 'Unknown',
};

// pricingDetail and PricingValue describe OpenCost's pricing source in a summary strip.
export function pricingDetail(quality: DataQuality | null): string {
  if (!quality) return 'Checking the pricing source…';
  if (quality.message) return quality.message;
  return quality.pricingSource === 'real' ? "Your cloud provider's prices" : 'Public list prices';
}

export const PricingValue: React.FC<{ quality: DataQuality | null; t: Tokens }> = ({ quality, t }) => {
  if (!quality) return <>—</>;
  const icon =
    quality.pricingSource === 'real' ? (
      <CheckIcon color={t.success} size={18} />
    ) : quality.pricingSource === 'estimated' ? (
      <WarningIcon color={t.warning} size={18} />
    ) : (
      <InfoIcon color={t.textMuted} size={18} />
    );
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
      {icon}
      {PRICING_LABELS[quality.pricingSource] ?? PRICING_LABELS.unknown}
    </span>
  );
};
