import React, { useState } from 'react';
import { useContainerWidth } from '../hooks/useContainerWidth';
import { Tokens, useTokens, visuallyHidden } from '../theme/tokens';
import { niceTicks } from '../utils/chartScale';
import { formatDay, formatMoney } from '../utils/format';
import { chartHeight } from '../utils/layout';
import { Granularity, PeriodBucket } from '../utils/ranges';

interface Props {
  buckets: PeriodBucket[];
  unit: Exclude<Granularity, 'day'>;
}

const PAD = { top: 12, right: 16, bottom: 28, left: 52 };
const TOOLTIP_WIDTH = 220;
const TOOLTIP_HEIGHT = 66;
const FALLBACK_WIDTH = 600;
// Bars fill this share of their slot; the rest is the gap between them.
const BAR_SHARE = 0.7;
// The room each axis label needs, so labels never overlap.
const LABEL_WIDTH = { month: 44, week: 72 };

const monthName = new Intl.DateTimeFormat('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' });
const monthShort = new Intl.DateTimeFormat('en-US', { month: 'short', timeZone: 'UTC' });
const asDate = (day: string) => new Date(`${day}T00:00:00Z`);

// periodName is what a bar stands for, in words.
function periodName(bucket: PeriodBucket, unit: Props['unit']): string {
  return unit === 'month' ? monthName.format(asDate(bucket.start)) : `Week of ${formatDay(bucket.start)}`;
}

// PeriodChart draws one bar per week or month, for ranges where daily points would only be noise.
// Bars sit in even slots whatever the period's length; a period the range, today or the start of the
// data cuts short is drawn lighter, so a short bar is not read as a cheap one.
export const PeriodChart: React.FC<Props> = ({ buckets, unit }) => {
  const t = useTokens();
  const [containerRef, width] = useContainerWidth<HTMLDivElement>(FALLBACK_WIDTH);
  const height = chartHeight(width);
  const [active, setActive] = useState<number | null>(null);

  const n = buckets.length;
  if (n === 0) return null;

  const ticks = niceTicks(Math.max(...buckets.map(bucket => bucket.cost)));
  const yMax = ticks[ticks.length - 1];
  const tickDecimals = Number.isInteger(ticks[1]) ? 0 : 2;
  const plotWidth = Math.max(width - PAD.left - PAD.right, 1);
  const plotHeight = height - PAD.top - PAD.bottom;
  const baseline = PAD.top + plotHeight;
  const slot = plotWidth / n;
  const barWidth = Math.max(1, slot * BAR_SHARE);
  const centerOf = (i: number) => PAD.left + (i + 0.5) * slot;
  const yOf = (cost: number) => baseline - (cost / yMax) * plotHeight;
  const clamp = (i: number) => Math.min(n - 1, Math.max(0, i));
  const latestIndex = Math.max(0, buckets.map(bucket => bucket.measuredDays > 0).lastIndexOf(true));
  // Month names are short, so they fit closer together than dates.
  const labelEvery = Math.max(1, Math.ceil(n / Math.max(2, Math.floor(plotWidth / (unit === 'month' ? LABEL_WIDTH.month : LABEL_WIDTH.week)))));

  const onPointerMove = (event: React.PointerEvent<SVGSVGElement>) => {
    const x = event.clientX - event.currentTarget.getBoundingClientRect().left - PAD.left;
    setActive(clamp(Math.floor(x / slot)));
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    switch (event.key) {
      case 'ArrowLeft':
        setActive(active === null ? latestIndex : clamp(active - 1));
        break;
      case 'ArrowRight':
        setActive(active === null ? latestIndex : clamp(active + 1));
        break;
      case 'Home':
        setActive(0);
        break;
      case 'End':
        setActive(n - 1);
        break;
      case 'Escape':
        setActive(null);
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  const activeBucket = active === null ? null : buckets[active];
  const activeX = active === null ? 0 : centerOf(active);
  const activeY = activeBucket ? yOf(activeBucket.cost) : 0;
  const per = unit === 'month' ? 'month' : 'week';
  const anyIncomplete = buckets.some(bucket => !bucket.complete && bucket.measuredDays > 0);

  return (
    // The chart is read by keyboard: it takes focus and the arrow keys step through its values.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      ref={containerRef}
      role="group"
      aria-label={`Cost per ${per}, one bar each. Use the left and right arrow keys to read each ${per}.`}
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      onKeyDown={onKeyDown}
      onBlur={() => setActive(null)}
      style={{ position: 'relative', borderRadius: '2px', outlineOffset: '4px' }}
    >
      <svg
        width={width}
        height={height}
        aria-hidden="true"
        focusable="false"
        onPointerMove={onPointerMove}
        onPointerLeave={() => setActive(null)}
        style={{ display: 'block', overflow: 'visible' }}
      >
        {activeBucket && (
          <rect x={centerOf(active!) - slot / 2} y={PAD.top} width={slot} height={plotHeight} fill={t.hover} />
        )}
        {ticks.map(tick => (
          <g key={tick}>
            <line
              x1={PAD.left}
              x2={width - PAD.right}
              y1={yOf(tick)}
              y2={yOf(tick)}
              stroke={tick === 0 ? t.border : t.hairline}
              strokeWidth={1}
            />
            <text x={PAD.left - 8} y={yOf(tick) + 4} textAnchor="end" fontSize={11} fill={t.textMuted}>
              {tick === 0 ? '$0' : formatMoney(tick, 'USD', tickDecimals)}
            </text>
          </g>
        ))}
        {buckets.map((bucket, i) =>
          bucket.measuredDays > 0 ? (
            <rect
              key={bucket.start}
              x={centerOf(i) - barWidth / 2}
              y={yOf(bucket.cost)}
              width={barWidth}
              height={baseline - yOf(bucket.cost)}
              rx={1}
              fill={t.accent}
              fillOpacity={bucket.complete ? 0.85 : 0.4}
            />
          ) : null
        )}
        {buckets.map((bucket, i) =>
          i % labelEvery === 0 ? (
            <text key={bucket.start} x={centerOf(i)} y={height - 6} textAnchor="middle" fontSize={11} fill={t.textMuted}>
              {unit === 'month' ? monthShort.format(asDate(bucket.start)) : formatDay(bucket.start)}
            </text>
          ) : null
        )}
      </svg>

      {activeBucket && (
        <PeriodTooltip
          bucket={activeBucket}
          unit={unit}
          left={Math.max(0, activeX + slot / 2 + TOOLTIP_WIDTH > width ? activeX - slot / 2 - TOOLTIP_WIDTH : activeX + slot / 2)}
          top={Math.max(0, Math.min(activeY, baseline) - TOOLTIP_HEIGHT - 8)}
          t={t}
        />
      )}
      {anyIncomplete && (
        <div aria-hidden="true" style={{ display: 'flex', gap: '6px', alignItems: 'center', marginTop: '8px', fontSize: '12px', color: t.textMuted }}>
          <svg width={12} height={10} aria-hidden="true" focusable="false">
            <rect width={12} height={10} fill={t.accent} fillOpacity={0.4} />
          </svg>
          {unit === 'month' ? 'Month' : 'Week'} not complete
        </div>
      )}
      <div role="status" style={visuallyHidden}>
        {activeBucket ? describeBucket(activeBucket, unit) : ''}
      </div>
    </div>
  );
};

// describeBucket is what the status region announces for a bar.
function describeBucket(bucket: PeriodBucket, unit: Props['unit']): string {
  const name = periodName(bucket, unit);
  if (bucket.measuredDays === 0) return `${name}: no cost data`;
  return `${name}: ${formatMoney(bucket.cost)}${bucket.complete ? '' : `, not complete (${measuredText(bucket)})`}`;
}

const measuredText = (bucket: PeriodBucket) => {
  const days = Math.round((asDate(bucket.end).getTime() - asDate(bucket.start).getTime()) / 86_400_000) + 1;
  return `${bucket.measuredDays} of ${days} days measured`;
};

interface PeriodTooltipProps {
  bucket: PeriodBucket;
  unit: Props['unit'];
  left: number;
  top: number;
  t: Tokens;
}

const PeriodTooltip: React.FC<PeriodTooltipProps> = ({ bucket, unit, left, top, t }) => (
  <div
    aria-hidden="true"
    style={{
      position: 'absolute',
      left,
      top,
      display: 'flex',
      flexDirection: 'column',
      gap: '2px',
      whiteSpace: 'nowrap',
      boxSizing: 'border-box',
      padding: '8px 12px',
      backgroundColor: t.surfaceSunken,
      border: `1px solid ${t.border}`,
      borderRadius: '4px',
      boxShadow: '1px 2px 3px rgba(0, 0, 0, 0.1)',
      pointerEvents: 'none',
    }}
  >
    <span style={{ fontSize: '11px', color: t.textMuted }}>
      {unit === 'month' ? periodName(bucket, unit) : `${formatDay(bucket.start)} – ${formatDay(bucket.end)}`}
    </span>
    <span style={{ fontSize: '13px', fontWeight: 500, color: t.textStrong }}>
      {bucket.measuredDays === 0 ? 'No cost data' : formatMoney(bucket.cost)}
    </span>
    {bucket.measuredDays > 0 && !bucket.complete && (
      <span style={{ fontSize: '11px', color: t.text }}>Not complete · {measuredText(bucket)}</span>
    )}
  </div>
);
