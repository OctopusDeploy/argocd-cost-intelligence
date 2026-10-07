import React, { useState } from 'react';
import { ChevronIcon, LeafIcon, WarningIcon } from './icons';
import { Notice } from './Notice';
import { cardStyle, useTokens } from '../theme/tokens';
import { CloudServiceEstimate, CostData } from '../utils/api';
import { costSlices } from '../utils/costInsights';
import {
  formatCarbon,
  formatKmDriven,
  formatMoney,
  formatPercent,
  formatRelativeTime,
  isStale,
  pluralize,
} from '../utils/format';

const TITLE_ID = 'cost-intel-breakdown-title';

const SOURCE_LABELS: Record<CloudServiceEstimate['source'], string> = {
  infracost: 'Infracost',
  terracost: 'Terracost',
};

// CostBreakdownCard splits the monthly cost into OpenCost's allocation and the cloud services
// estimate, as one stacked bar with a labelled row per part. namespace is set when the application
// shares its namespace, since OpenCost's allocation then covers other workloads too.
export const CostBreakdownCard: React.FC<{ data: CostData; namespace?: string }> = ({ data, namespace }) => {
  const t = useTokens();
  const [cloudOpen, setCloudOpen] = useState(false);
  const [sharedOpen, setSharedOpen] = useState(false);

  const slices = costSlices(data);
  const total = slices.reduce((sum, slice) => sum + slice.value, 0);
  if (total <= 0) return <Notice>No cost breakdown for this measurement.</Notice>;

  const totals = data.opencostOutput?.allocationTotals;
  const cloud = data.cloudEstimate;
  const cloudResources = cloud?.resources ?? [];
  const carbonKg = totals?.carbonCost ?? 0;
  const shared = totals?.sharedCost ?? 0;
  const sharedParts = Object.entries(totals?.sharedCostBreakdown ?? {}).sort(([, a], [, b]) => b - a);

  const rowHeader: React.CSSProperties = { padding: '8px 0', fontWeight: 400, textAlign: 'left' };
  const amount: React.CSSProperties = { width: '110px', padding: '8px 0', color: t.textStrong, textAlign: 'right' };
  const share: React.CSSProperties = { width: '64px', padding: '8px 0', color: t.textMuted, textAlign: 'right' };
  const subRowHeader: React.CSSProperties = { ...rowHeader, padding: '4px 0 4px 20px', color: t.textMuted, fontSize: '12px' };
  const subAmount: React.CSSProperties = { ...amount, padding: '4px 0', color: t.text, fontSize: '12px' };
  const subShare: React.CSSProperties = { ...share, padding: '4px 0', fontSize: '12px' };
  const toggleStyle: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '4px',
    padding: 0,
    border: 'none',
    backgroundColor: 'transparent',
    color: t.accentText,
    fontFamily: 'inherit',
    fontSize: '12.8px',
    cursor: 'pointer',
  };

  return (
    <section aria-labelledby={TITLE_ID} style={{ ...cardStyle(t), padding: '20px 24px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '16px' }}>
        <h4 id={TITLE_ID} style={{ margin: 0, color: t.textStrong, fontSize: '14px', fontWeight: 500, lineHeight: '20px' }}>
          Cost breakdown
        </h4>
        <span style={{ color: t.textMuted, fontSize: '12px' }}>{cloud ? 'Monthly, Kubernetes and cloud' : 'Monthly'}</span>
      </div>
      {namespace && (
        <p style={{ margin: '4px 0 0', color: t.textMuted, fontSize: '12px' }}>Whole {namespace} namespace, not only this app</p>
      )}

      <div aria-hidden="true" style={{ display: 'flex', gap: '2px', height: '12px', margin: '16px 0' }}>
        {slices.map((slice, index) => (
          <span
            key={slice.id}
            style={{
              flex: `${slice.value} 1 0`,
              minWidth: '3px',
              backgroundColor: t.series[slice.slot],
              borderRadius:
                slices.length === 1 ? '4px' : index === 0 ? '4px 0 0 4px' : index === slices.length - 1 ? '0 4px 4px 0' : 0,
            }}
          />
        ))}
      </div>

      <table
        aria-labelledby={TITLE_ID}
        style={{ width: '100%', borderCollapse: 'collapse', color: t.text, fontSize: '13px', fontVariantNumeric: 'tabular-nums' }}
      >
        <tbody>
          {slices.map(slice => (
            <React.Fragment key={slice.id}>
              <tr style={{ borderTop: `1px solid ${t.hairline}` }}>
                <th scope="row" style={rowHeader}>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <span
                      aria-hidden="true"
                      style={{ flexShrink: 0, width: '10px', height: '10px', borderRadius: '2px', backgroundColor: t.series[slice.slot] }}
                    />
                    {slice.label}
                    {slice.id === 'cloud' && cloudResources.length > 0 && (
                      <button type="button" aria-expanded={cloudOpen} onClick={() => setCloudOpen(open => !open)} style={toggleStyle}>
                        {pluralize(cloudResources.length, 'resource')}
                        <ChevronIcon expanded={cloudOpen} size={12} />
                      </button>
                    )}
                  </span>
                  {slice.id === 'cloud' && cloud && (
                    <span
                      style={{ display: 'flex', alignItems: 'center', gap: '6px', marginTop: '2px', paddingLeft: '20px', color: t.textMuted, fontSize: '12px' }}
                    >
                      {isStale(cloud.pushedAt) && <WarningIcon color={t.warning} size={12} />}
                      {SOURCE_LABELS[cloud.source] ?? cloud.source} estimate from {formatRelativeTime(cloud.pushedAt)}
                    </span>
                  )}
                </th>
                <td style={amount}>{formatMoney(slice.value)}</td>
                <td style={share}>{formatPercent(slice.value / total)}</td>
              </tr>
              {slice.children?.map(part => (
                <tr key={part.label}>
                  <th scope="row" style={subRowHeader}>
                    {part.label}
                  </th>
                  <td style={subAmount}>{formatMoney(part.value)}</td>
                  <td style={subShare}>{formatPercent(part.value / total)}</td>
                </tr>
              ))}
              {slice.id === 'cloud' &&
                cloudOpen &&
                cloudResources.map(resource => (
                  <tr key={resource.name}>
                    <th scope="row" style={subRowHeader}>
                      {resource.name}{' '}
                      <span style={{ fontFamily: 'monospace', fontSize: '11px' }}>{resource.resourceType}</span>
                    </th>
                    <td style={subAmount}>{formatMoney(resource.monthlyCost, cloud?.currency)}</td>
                    <td style={subShare}>{formatPercent(resource.monthlyCost / total)}</td>
                  </tr>
                ))}
            </React.Fragment>
          ))}
          <tr style={{ borderTop: `1px solid ${t.border}` }}>
            <th scope="row" style={{ ...rowHeader, padding: '10px 0', color: t.textStrong, fontWeight: 500 }}>
              Total
            </th>
            <td style={{ ...amount, padding: '10px 0', fontWeight: 500 }}>{formatMoney(total)}</td>
            <td style={{ ...share, padding: '10px 0' }}>100%</td>
          </tr>
        </tbody>
      </table>

      {shared > 0 && (
        <div style={{ marginTop: '8px', paddingTop: '8px', borderTop: `1px solid ${t.hairline}` }}>
          <button
            type="button"
            aria-expanded={sharedOpen}
            onClick={() => setSharedOpen(open => !open)}
            style={{ ...toggleStyle, display: 'flex', width: '100%', gap: '8px', color: t.text, fontSize: '13px' }}
          >
            <ChevronIcon expanded={sharedOpen} size={12} />
            <span style={{ flex: 1, textAlign: 'left' }}>Shared costs</span>
            <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatMoney(shared)}</span>
          </button>
          {sharedOpen && (
            <div style={{ padding: '6px 0 0 20px', color: t.textMuted, fontSize: '12px' }}>
              {sharedParts.map(([name, cost]) => (
                <div key={name} style={{ display: 'flex', justifyContent: 'space-between', padding: '3px 0' }}>
                  <span>{name}</span>
                  <span style={{ color: t.text, fontVariantNumeric: 'tabular-nums' }}>{formatMoney(cost)}</span>
                </div>
              ))}
              <p style={{ margin: '6px 0 0', lineHeight: '18px' }}>
                Attributed proportionally from cluster-wide services, such as monitoring, DNS and ingress, running in other
                namespaces.
              </p>
            </div>
          )}
        </div>
      )}

      {carbonKg > 0 && (
        <p style={{ display: 'flex', alignItems: 'center', gap: '6px', margin: '12px 0 0', color: t.textMuted, fontSize: '12px' }}>
          <LeafIcon />
          Carbon footprint: {formatCarbon(carbonKg)} per month, about {formatKmDriven(carbonKg)} by car
        </p>
      )}
    </section>
  );
};
