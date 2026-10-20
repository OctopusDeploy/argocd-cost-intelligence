import React from 'react';
import { CostActual } from './CostActual';
import { CostTrendComponent } from './CostTrend';
import { MeasuredLabel } from './MeasuredLabel';
import { framePanelStyle, PluginFrame } from './PluginFrame';
import { ReloadButton } from './ReloadButton';
import { ResourcesTab } from './ResourcesTab';
import { panelId, Tab, TabBar, tabId } from './TabBar';
import { useCostData } from '../hooks/useCostData';
import { useDailyRange } from '../hooks/useDailyRange';
import { useRightsizing } from '../hooks/useRightsizing';
import { useTrendRange } from '../hooks/useTrendRange';
import { useUrlState } from '../hooks/useUrlState';
import { useTokens } from '../theme/tokens';
import { AppContext, CostData, DailyCost } from '../utils/api';
import { sharedNamespace } from '../utils/costInsights';
import { utcDay } from '../utils/forecast';
import { formatDay, formatHours, isPartialMeasurement } from '../utils/format';
import { queryRange } from '../utils/ranges';

export interface CostPanelProps {
  appName: string;
  appCtx?: AppContext;
  managedControllers?: Set<string>;
}

type TabId = 'actual' | 'resources' | 'trends';

const TABS: Array<Tab<TabId>> = [
  { id: 'actual', label: 'Actual costs' },
  { id: 'resources', label: 'Resources' },
  { id: 'trends', label: 'Trends' },
];

const TAB_IDS = TABS.map(tab => tab.id);

const ID_PREFIX = 'cost-intel';

export const CostPanel: React.FC<CostPanelProps> = ({ appName, appCtx, managedControllers }) => {
  // The open tab lives in the URL, so a link to this view opens on the same tab.
  const [activeTab, setActiveTab] = useUrlState<TabId>('costTab', 'actual', TAB_IDS);
  const { loading, error, data, trend, dailyCosts, refetch } = useCostData(appName, appCtx);
  // The Trends range lives in the URL. Its series is fetched only while Trends is open, and not for
  // the last 30 days, which useCostData already has.
  const today = utcDay();
  const trendRange = useTrendRange(today);
  const fetchRange = activeTab === 'trends' && trendRange.id !== '30d' ? queryRange(trendRange.range, today) : null;
  const rangeData = useDailyRange(appName, appCtx, fetchRange, today);
  const { rightsizing, refetch: refetchRightsizing } = useRightsizing(appName, appCtx);
  const showResources = () => {
    // The button that asked for this disappears with the view, so keep focus in the tab bar.
    setActiveTab('resources');
    document.getElementById(tabId(ID_PREFIX, 'resources'))?.focus();
  };

  return (
    <PluginFrame
      status={
        <>
          <Freshness tab={activeTab} data={data} dailyCosts={dailyCosts} />
          <ReloadButton onReload={() => Promise.all([refetch(), rangeData.refetch(), refetchRightsizing()])} />
        </>
      }
    >
      <TabBar tabs={TABS} active={activeTab} onChange={setActiveTab} idPrefix={ID_PREFIX} label="Cost Intelligence views" />

      <div
        id={panelId(ID_PREFIX, activeTab)}
        role="tabpanel"
        aria-labelledby={tabId(ID_PREFIX, activeTab)}
        style={framePanelStyle}
      >
        {activeTab === 'actual' && (
          <CostActual
            data={data}
            trend={trend}
            loading={loading}
            error={error}
            onShowResources={showResources}
            managedControllers={managedControllers}
            rightsizing={rightsizing}
          />
        )}
        {activeTab === 'resources' && (
          <ResourcesTab
            data={data}
            loading={loading}
            error={error}
            managedControllers={managedControllers}
            appName={appName}
            appCtx={appCtx}
            rightsizing={rightsizing}
          />
        )}
        {activeTab === 'trends' && (
          <CostTrendComponent
            loading={loading}
            error={error}
            dailyCosts={dailyCosts}
            namespace={data && data.scope !== 'application' ? sharedNamespace(data, managedControllers)?.namespace : undefined}
            range={{ ...trendRange, ...rangeData }}
          />
        )}
      </div>
    </PluginFrame>
  );
};

interface FreshnessProps {
  tab: TabId;
  data: CostData | null;
  dailyCosts: DailyCost[] | null;
}

// Freshness says how current the active view is: the latest sync measurement for Actual costs
// and Resources, the latest day of the series for Trends.
const Freshness: React.FC<FreshnessProps> = ({ tab, data, dailyCosts }) => {
  const t = useTokens();

  if (tab === 'trends') {
    const lastDay = dailyCosts?.[dailyCosts.length - 1];
    return lastDay ? (
      <span style={{ color: t.textMuted, fontSize: '12.8px' }}>Daily data through {formatDay(lastDay.date)}</span>
    ) : null;
  }

  if (!data) return null;
  const measuredHours = data.opencostOutput?.measuredHours;
  const partial =
    measuredHours !== undefined && isPartialMeasurement(measuredHours)
    ? ` OpenCost had ${formatHours(measuredHours)} of data for this ${data.scope === 'application' ? 'application' : 'namespace'}, so monthly figures are projected from those hours and will settle once it has a full day.`
    : '';
  return (
    <MeasuredLabel
      measuredAt={data.measuredAt}
      measuredHours={measuredHours}
      atInstall={data.measuredOn === 'install'}
      title={`Measured with OpenCost ${data.measuredOn === 'install' ? 'at install, before the application synced' : 'after the latest sync'}, ${new Date(data.measuredAt).toLocaleString()}.${partial}`}
    />
  );
};
