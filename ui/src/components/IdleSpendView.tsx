import React, { useState } from 'react';
import { ErrorNotice, Notice } from './Notice';
import { StatStrip } from './StatStrip';
import { saving, WasteRankingCard } from './WasteRankingCard';
import { useUrlState } from '../hooks/useUrlState';
import { UseWasteRankingResult } from '../hooks/useWasteRanking';
import { useTokens, visuallyHidden } from '../theme/tokens';
import { formatMoney, pluralize } from '../utils/format';
import { deploymentNamespaces, notRankedDetail, wasteTable } from '../utils/waste';

const NAMESPACE_SELECT_ID = 'cost-intel-idle-namespace';

interface Props {
  waste: UseWasteRankingResult;
  // clusterIdleCost is the idle cost of every measured namespace, or null when none is measured.
  clusterIdleCost: number | null;
  onSelectNamespace: (namespace: string) => void;
}

// IdleSpendView ranks the cluster's applications by the idle cost of their own pods, so the ones
// worth fixing first are at the top. The figures above the ranking always cover every ranked app;
// the namespace filter narrows only the ranking.
export const IdleSpendView: React.FC<Props> = ({ waste, clusterIdleCost, onSelectNamespace }) => {
  const t = useTokens();
  // The same parameter as Chargeback's namespace, so a namespace chosen on one carries to the other.
  const [namespace, setNamespace] = useUrlState<string>('costScope', '');
  const [announcement, setAnnouncement] = useState('');
  const { ranking, loading, error } = waste;

  if (error) return <ErrorNotice message={error} />;
  if (!ranking) return loading ? <Notice>Loading the ranking…</Notice> : null;

  const { total } = wasteTable(ranking.apps);
  const notRanked = ranking.namespaceOnly + ranking.noPods;
  const shown = namespace ? ranking.apps.filter(app => app.deploymentNamespace === namespace) : ranking.apps;
  // A linked namespace stays selectable after its apps are gone, so the filter never lies about what
  // it shows.
  const options = deploymentNamespaces(ranking.apps);
  if (namespace && !options.includes(namespace)) options.push(namespace);

  const choose = (next: string) => {
    setNamespace(next, 'replace');
    const count = next ? ranking.apps.filter(app => app.deploymentNamespace === next).length : ranking.apps.length;
    setAnnouncement(next ? `${pluralize(count, 'app')} deploying to ${next}` : `All ${pluralize(count, 'app')}`);
  };

  const filter =
    options.length > 1 || namespace ? (
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
        <label htmlFor={NAMESPACE_SELECT_ID} style={{ color: t.textMuted, fontSize: '12px', fontWeight: 500 }}>
          Namespace
        </label>
        <select
          id={NAMESPACE_SELECT_ID}
          value={namespace}
          onChange={event => choose(event.target.value)}
          style={{
            padding: '7px 10px',
            border: `1px solid ${t.border}`,
            borderRadius: '4px',
            backgroundColor: t.surfaceSunken,
            color: t.text,
            fontFamily: 'inherit',
            fontSize: '13px',
            cursor: 'pointer',
          }}
        >
          <option value="">All namespaces</option>
          {options.map(ns => (
            <option key={ns} value={ns}>
              {ns}
            </option>
          ))}
        </select>
      </div>
    ) : undefined;

  return (
    <div>
      <StatStrip
        stats={[
          {
            label: "Apps' idle spend",
            value: formatMoney(total.idleCost),
            detail: `In the pods of ${pluralize(total.count, 'ranked app')}`,
          },
          {
            label: 'Lower CPU requests',
            value: total.suggested > 0 ? saving(total.saving) : '—',
            detail: total.suggested > 0 ? `Suggested for ${pluralize(total.suggested, 'app')}` : 'No app has one to suggest',
          },
          {
            label: 'Not ranked',
            value: String(notRanked),
            detail: notRankedDetail(ranking.namespaceOnly, ranking.noPods) ?? 'Every app measured is ranked',
          },
          {
            label: 'Cluster idle spend',
            value: clusterIdleCost !== null ? formatMoney(clusterIdleCost) : '—',
            detail: clusterIdleCost !== null ? 'Including pods no app owns' : "Not measured yet. Measured at each namespace's next sync",
          },
        ]}
      />
      <WasteRankingCard
        apps={shown}
        namespace={namespace}
        noPods={ranking.noPods}
        action={filter}
        onSelectNamespace={onSelectNamespace}
      />
      <div role="status" style={visuallyHidden}>
        {announcement}
      </div>
    </div>
  );
};
