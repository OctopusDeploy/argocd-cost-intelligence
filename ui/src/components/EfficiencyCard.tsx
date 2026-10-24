import React from 'react';
import { CardHeader } from './CardHeader';
import { CheckIcon, ErrorIcon, GaugeIcon, InfoIcon, WarningIcon } from './icons';
import { cardStyle, Tokens, useTokens } from '../theme/tokens';
import { EfficiencyMetrics } from '../utils/api';
import { efficiencyLevel, EfficiencyLevel, hasRequests } from '../utils/costInsights';
import { formatBytes, formatCores, formatMoney, formatPercent, roundParts } from '../utils/format';

const TITLE_ID = 'cost-intel-efficiency-title';

// LEVEL_LABELS and levelIcon present an efficiency level the same way wherever it appears.
export const LEVEL_LABELS: Record<EfficiencyLevel, string> = {
  'over-request': 'Above request',
  healthy: 'Healthy',
  moderate: 'Moderate',
  'over-provisioned': 'Over-provisioned',
};

export function levelIcon(level: EfficiencyLevel, t: Tokens): React.ReactNode {
  switch (level) {
    case 'over-request':
      return <ErrorIcon color={t.danger} size={14} />;
    case 'healthy':
      return <CheckIcon color={t.success} size={14} />;
    case 'moderate':
      return <InfoIcon color={t.textMuted} />;
    case 'over-provisioned':
      return <WarningIcon color={t.warning} />;
  }
}

// EfficiencyCard compares CPU and memory use with what the pods request, and what the gap costs.
// namespace is set when the application shares its namespace, whose pods the figures cover.
export const EfficiencyCard: React.FC<{ efficiency: EfficiencyMetrics; namespace?: string }> = ({
  efficiency: e,
  namespace,
}) => {
  const t = useTokens();
  const showPeak = e.cpuCoresUsedMax > 0 || e.ramBytesUsedMax > 0;
  // Rounded so the CPU and memory idle add up to the idle spend below them.
  const [cpuIdle, ramIdle] = roundParts([e.cpuIdleCost, e.ramIdleCost]);
  const requested = hasRequests(e);

  return (
    <section
      aria-labelledby={TITLE_ID}
      style={{ ...cardStyle(t), display: 'flex', flexDirection: 'column', padding: '20px 24px' }}
    >
      <CardHeader
        id={TITLE_ID}
        icon={<GaugeIcon />}
        title="Resource efficiency"
        detail={requested ? `${formatPercent(e.totalEfficiency)} of requested resources used` : 'The pods set no CPU or memory requests'}
      />
      {namespace && (
        <p style={{ margin: '4px 0 0', color: t.textMuted, fontSize: '12px' }}>Whole {namespace} namespace, not only this app</p>
      )}

      <div
        aria-hidden="true"
        style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '16px', marginTop: '12px', color: t.textMuted, fontSize: '12px' }}
      >
        <LegendKey swatch={{ width: '14px', height: '8px', borderRadius: '4px', backgroundColor: t.accent }} label="Average use" />
        {showPeak && <LegendKey swatch={{ width: '2px', height: '12px', backgroundColor: t.warning }} label="Peak" />}
        {requested && <LegendKey swatch={{ width: '2px', height: '14px', backgroundColor: t.textStrong }} label="Request" />}
        <LegendKey
          swatch={{ width: '14px', height: '8px', borderRadius: '4px', backgroundColor: t.accentWash }}
          label="Up to limit"
        />
      </div>

      <Meter
        label="CPU"
        level={e.cpuCoresRequested > 0 ? efficiencyLevel(e.cpuEfficiency) : null}
        used={e.cpuCoresUsed}
        peak={e.cpuCoresUsedMax}
        request={e.cpuCoresRequested}
        limit={e.cpuCoresLimit}
        format={formatCores}
        idleCost={cpuIdle}
        t={t}
      />
      <Meter
        label="Memory"
        level={e.ramBytesRequested > 0 ? efficiencyLevel(e.ramEfficiency) : null}
        used={e.ramBytesUsed}
        peak={e.ramBytesUsedMax}
        request={e.ramBytesRequested}
        limit={e.ramBytesLimit}
        format={formatBytes}
        idleCost={ramIdle}
        t={t}
      />

      <div style={{ flex: 1 }} />
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          minHeight: '40px',
          marginTop: '24px',
          borderTop: `1px solid ${t.border}`,
          color: t.textStrong,
          fontSize: '13px',
          fontWeight: 500,
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        <span>Idle spend</span>
        <span>{formatMoney(e.cpuIdleCost + e.ramIdleCost)} / mo</span>
      </div>
    </section>
  );
};

const LegendKey: React.FC<{ swatch: React.CSSProperties; label: string }> = ({ swatch, label }) => (
  <span style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
    <span style={swatch} />
    {label}
  </span>
);

interface MeterProps {
  label: string;
  // level is null when the pods request none of the resource: there is nothing to judge use against.
  level: EfficiencyLevel | null;
  used: number;
  peak: number;
  request: number;
  limit: number;
  format: (value: number) => string;
  idleCost: number;
  t: Tokens;
}

// Meter draws average use on a track that runs to the limit (or the largest value when no limit
// is set), with markers for the request, when one is set, and, when collected, the peak.
const Meter: React.FC<MeterProps> = ({ label, level, used, peak, request, limit, format, idleCost, t }) => {
  const scale = Math.max(limit, request, used, peak) || 1;
  const position = (value: number) => `${Math.min(value / scale, 1) * 100}%`;
  const hasPeak = peak > 0;
  const hasLimit = limit > 0;
  const hasRequest = request > 0;
  const figures = [
    `Avg ${format(used)}`,
    hasPeak ? `Peak ${format(peak)}` : null,
    hasRequest ? `Request ${format(request)}` : 'No request',
    hasLimit ? `Limit ${format(limit)}` : null,
  ].filter(Boolean);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginTop: '24px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span style={{ color: t.textStrong, fontSize: '13px', fontWeight: 500 }}>{label}</span>
        <span style={{ display: 'flex', alignItems: 'center', gap: '6px', color: t.text, fontSize: '12.8px' }}>
          {level ? levelIcon(level, t) : <InfoIcon color={t.textMuted} />}
          {level ? LEVEL_LABELS[level] : 'No request set'}
        </span>
      </div>
      <div
        role="img"
        aria-label={`${label}: ${figures.join(', ')}`}
        style={{ position: 'relative', height: '8px', borderRadius: '4px', backgroundColor: t.accentWash }}
      >
        <div style={{ width: position(used), height: '8px', borderRadius: '4px', backgroundColor: t.accent }} />
        {hasPeak && (
          <div
            style={{ position: 'absolute', top: '-2px', left: position(peak), width: '2px', height: '12px', marginLeft: '-1px', backgroundColor: t.warning }}
          />
        )}
        {hasRequest && (
          <div
            style={{ position: 'absolute', top: '-4px', left: position(request), width: '2px', height: '16px', marginLeft: '-1px', backgroundColor: t.textStrong }}
          />
        )}
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '12px', fontSize: '12px', fontVariantNumeric: 'tabular-nums' }}>
        <span style={{ color: t.textMuted }}>{figures.join(' · ')}</span>
        <span style={{ flexShrink: 0, color: t.text }}>{formatMoney(idleCost)} / mo idle</span>
      </div>
      {hasPeak && hasRequest && peak > request && (
        <span style={{ display: 'flex', alignItems: 'center', gap: '6px', color: t.text, fontSize: '12px' }}>
          <WarningIcon color={t.warning} />
          Peak use is above the request, so pods may be throttled.
        </span>
      )}
    </div>
  );
};
