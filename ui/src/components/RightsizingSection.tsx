import React, { useEffect, useState } from 'react';
import { CardHeader } from './CardHeader';
import { SlidersIcon, WarningIcon } from './icons';
import { cardStyle, Tokens, useTokens, visuallyHidden } from '../theme/tokens';
import { ContainerRightsizing, Rightsizing, WorkloadRightsizing } from '../utils/api';
import { pluralize } from '../utils/format';
import {
  containerChange,
  formatCpu,
  formatMemory,
  formatMonthlyChange,
  hasVpa,
  memoryLimitTooLow,
  minimumMemoryLimit,
  RangePosition,
  rangePosition,
  requestsYaml,
} from '../utils/rightsizing';

const STATUS_LABELS: Record<WorkloadRightsizing['status'], string> = {
  recommended: 'VPA recommends',
  managed: 'Managed by VPA',
  pending: 'VPA has no recommendation yet',
  none: 'No VPA',
};

// RightsizingSection shows, per workload, what the Vertical Pod Autoscaler recommends for each
// container: where today's requests sit against the VPA's range, and the requests to paste into the
// manifest. It renders nothing when none of the application's workloads has a VPA.
export const RightsizingSection: React.FC<{ rightsizing: Rightsizing | null }> = ({ rightsizing }) => {
  const t = useTokens();
  if (!rightsizing || !hasVpa(rightsizing)) return null;

  return (
    <section aria-labelledby="cost-intel-rightsizing" style={{ ...cardStyle(t), marginTop: '16px' }}>
      <div style={{ padding: '18px 20px 6px' }}>
        <CardHeader
          as="h3"
          id="cost-intel-rightsizing"
          icon={<SlidersIcon />}
          title="Rightsizing"
          detail={
            <>
              Requests the Vertical Pod Autoscaler recommends, per container. On each bar, the shaded band is the
              VPA&apos;s range, the tick its target and the line today&apos;s request.
            </>
          }
        />
      </div>
      {rightsizing.workloads.map(workload => (
        <Workload key={`${workload.kind}/${workload.name}`} workload={workload} t={t} />
      ))}
    </section>
  );
};

const Workload: React.FC<{ workload: WorkloadRightsizing; t: Tokens }> = ({ workload, t }) => {
  const advised = workload.status === 'recommended';
  return (
    <div style={{ padding: '16px 20px', borderTop: `1px solid ${t.hairline}` }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
        <h4 style={{ margin: 0, color: t.textStrong, fontSize: '14px', fontWeight: 500 }}>
          {workload.name}{' '}
          <span style={{ color: t.textMuted, fontWeight: 400 }}>
            · {workload.kind}
            {advised && workload.pods > 0 ? ` · ${pluralize(workload.pods, 'pod')}` : ''}
          </span>
        </h4>
        <span style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
          {advised && workload.lowConfidence && (
            <Tag t={t} color={t.warning}>
              <WarningIcon color={t.warning} size={12} /> Low confidence
            </Tag>
          )}
          {advised ? (
            workload.containers.length > 0 && <Tag t={t}>{formatMonthlyChange(workload.monthlyChange)}</Tag>
          ) : (
            <Tag t={t}>
              {STATUS_LABELS[workload.status]}
              {workload.status === 'managed' && workload.updateMode ? ` (${workload.updateMode})` : ''}
            </Tag>
          )}
        </span>
      </div>
      {advised && workload.containers.length === 0 && (
        <p style={{ margin: '8px 0 0', color: t.textMuted, fontSize: '12px' }}>
          None of the containers the VPA recommends for ran in the last 24 hours.
        </p>
      )}
      {workload.containers.map(container => (
        <Container key={container.name} container={container} t={t} />
      ))}
    </div>
  );
};

const Container: React.FC<{ container: ContainerRightsizing; t: Tokens }> = ({ container, t }) => (
  <div
    style={{
      display: 'grid',
      gridTemplateColumns: 'repeat(auto-fit, minmax(min(280px, 100%), 1fr))',
      gap: '8px 24px',
      marginTop: '12px',
      alignItems: 'start',
    }}
  >
    <div style={{ minWidth: 0 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '12px', marginBottom: '4px' }}>
        <span style={{ color: t.textStrong, fontSize: '13px' }}>{container.name}</span>
        <span style={{ color: t.text, fontSize: '12px', fontVariantNumeric: 'tabular-nums' }}>
          {formatMonthlyChange(containerChange(container))}
        </span>
      </div>
      <RangeBar
        label="CPU"
        format={formatCpu}
        request={container.requests.cpuCores}
        suggested={container.suggested.cpuCores}
        lower={container.lowerBound.cpuCores}
        upper={container.upperBound.cpuCores}
        target={container.target.cpuCores}
        unsettled={container.unsettled}
        t={t}
      />
      <RangeBar
        label="Memory"
        format={formatMemory}
        request={container.requests.memoryBytes}
        suggested={container.suggested.memoryBytes}
        lower={container.lowerBound.memoryBytes}
        upper={container.upperBound.memoryBytes}
        target={container.target.memoryBytes}
        unsettled={container.unsettled}
        t={t}
      />
      {memoryLimitTooLow(container) && (
        <p style={{ display: 'flex', gap: '6px', alignItems: 'flex-start', margin: '4px 0 0', color: t.text, fontSize: '12px' }}>
          <span style={{ display: 'flex', marginTop: '2px' }}>
            <WarningIcon color={t.warning} size={12} />
          </span>
          <span>
            The memory limit of {formatMemory(container.memoryLimitBytes)} is below the most the VPA expects it to need,{' '}
            {formatMemory(container.upperBound.memoryBytes)}. Raise it to at least{' '}
            {formatMemory(minimumMemoryLimit(container))} so the container is not killed for running out of memory.
          </span>
        </p>
      )}
    </div>
    <Snippet container={container} t={t} />
  </div>
);

const POSITION_WORDS: Record<RangePosition, string> = {
  above: 'above the range',
  within: 'within the range',
  below: 'below the range',
};

interface RangeBarProps {
  label: string;
  format: (value: number) => string;
  request: number;
  suggested: number;
  target: number;
  lower: number;
  upper: number;
  unsettled: boolean;
  t: Tokens;
}

// RangeBar places today's request against the VPA's range and target. The words carry everything the
// bar shows, so the bar itself is hidden from screen readers. An unsettled upper bound is neither named
// nor drawn to scale: the range runs off the end of the bar.
const RangeBar: React.FC<RangeBarProps> = ({ label, format, request, suggested, target, lower, upper, unsettled, t }) => {
  const position = rangePosition(request, lower, upper);
  const max = Math.max(request, unsettled ? target : upper, suggested) * 1.15 || 1;
  const x = (value: number) => Math.min(100, (value / max) * 100);
  const requestColor = position === 'above' ? t.success : position === 'below' ? t.danger : t.textMuted;
  return (
    <div style={{ margin: '2px 0 8px' }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: '8px',
          flexWrap: 'wrap',
          color: t.textMuted,
          fontSize: '12px',
        }}
      >
        <span>
          {label}: requests {format(request)}, {POSITION_WORDS[position]}
        </span>
        <span>
          VPA <b style={{ color: t.textStrong, fontWeight: 500 }}>{format(suggested)}</b>{' '}
          {unsettled ? `(from ${format(lower)}; upper bound not settled yet)` : `(range ${format(lower)}–${format(upper)})`}
        </span>
      </div>
      <svg width="100%" height="14" viewBox="0 0 100 14" preserveAspectRatio="none" aria-hidden="true" focusable="false">
        <rect x="0" y="5" width="100" height="4" rx="2" fill={t.surfaceSunken} />
        <rect x={x(lower)} y="3" width={Math.max(0.5, x(upper) - x(lower))} height="8" rx="2" fill={t.accent} fillOpacity={0.25} />
        <rect x={x(target) - 0.4} y="1" width="0.8" height="12" fill={t.accent} />
        <rect x={x(request) - 0.5} y="0" width="1" height="14" fill={requestColor} />
      </svg>
    </div>
  );
};

// Snippet is the requests to paste into the pod template, with a button that copies them.
const Snippet: React.FC<{ container: ContainerRightsizing; t: Tokens }> = ({ container, t }) => {
  const [copied, setCopied] = useState(false);
  const yaml = requestsYaml([container]);
  const canCopy = typeof navigator !== 'undefined' && Boolean(navigator.clipboard);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(yaml);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div style={{ minWidth: 0 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
        <span style={{ color: t.textMuted, fontSize: '12px' }}>Suggested requests</span>
        {canCopy && (
          <button
            type="button"
            onClick={copy}
            aria-label={`Copy the suggested requests for ${container.name}`}
            style={{
              border: `1px solid ${t.border}`,
              background: t.surface,
              color: t.text,
              borderRadius: '4px',
              padding: '2px 10px',
              fontFamily: 'inherit',
              fontSize: '12px',
              cursor: 'pointer',
            }}
          >
            {copied ? 'Copied' : 'Copy'}
          </button>
        )}
      </div>
      <pre
        style={{
          margin: 0,
          padding: '8px 12px',
          background: t.surfaceSunken,
          border: `1px solid ${t.hairline}`,
          borderRadius: '4px',
          color: t.text,
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
          fontSize: '12px',
          lineHeight: 1.5,
          overflowX: 'auto',
        }}
      >
        {yaml}
      </pre>
      <span role="status" style={visuallyHidden}>
        {copied ? `Copied the suggested requests for ${container.name}` : ''}
      </span>
    </div>
  );
};

const Tag: React.FC<{ t: Tokens; color?: string; children: React.ReactNode }> = ({ t, color, children }) => (
  <span
    style={{
      display: 'inline-flex',
      alignItems: 'center',
      gap: '4px',
      padding: '1px 8px',
      border: `1px solid ${color ?? t.border}`,
      borderRadius: '10px',
      color: t.text,
      fontSize: '11px',
      whiteSpace: 'nowrap',
    }}
  >
    {children}
  </span>
);
