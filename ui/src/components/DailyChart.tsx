import React, { useState } from 'react';
import { useContainerWidth } from '../hooks/useContainerWidth';
import { Tokens, useTokens, visuallyHidden } from '../theme/tokens';
import { DailyCost } from '../utils/api';
import { niceTicks, tickDecimals } from '../utils/chartScale';
import { MonthForecast } from '../utils/forecast';
import { formatDay, formatEstimate, formatMoney, formatMonth } from '../utils/format';
import { addDays, fromFirstCost, spanDays } from '../utils/ranges';
import { chartHeight, dayLabelCount } from '../utils/layout';

interface Props {
  dailyCosts: DailyCost[];
  // today is the UTC day OpenCost is still measuring: drawn on its own, not joined to the line.
  today: string;
  // from and to are the days the x-axis spans, both included; by default, the series' own days, and
  // with a forecast, on to the month's last day. Days without data are left empty.
  from?: string;
  to?: string;
  // forecast, when set, draws a dashed trend line through the days it covers.
  forecast?: MonthForecast | null;
}

// A Slot is one day on the x-axis: a measured day, a forecast day, both (the last complete day,
// where the trend line starts, and today, which is still being measured), or neither.
interface Slot {
  date: string;
  day?: DailyCost;
  trend?: number;
}

const PAD = { top: 12, right: 16, bottom: 28, left: 52 };
// The tooltip sizes to its content; these estimates only decide which side of the cursor it goes.
const TOOLTIP_WIDTH = 260;
const TOOLTIP_HEIGHT = 66;
// Used where the container cannot be measured (no layout, as in tests).
const FALLBACK_WIDTH = 600;
const DASH = '6 5';

// toSlots gives every day from through to its place on the axis, so days are spaced by date and a
// day without data leaves a gap rather than pulling its neighbours together.
function toSlots(days: DailyCost[], from: string, to: string, forecast?: MonthForecast | null): Slot[] {
  const byDate = new Map(days.map(day => [day.date, day]));
  const trend = new Map((forecast?.trend ?? []).map(({ date, cost }) => [date, cost]));
  const slots: Slot[] = [];
  for (let date = from; date <= to; date = addDays(date, 1)) {
    slots.push({ date, day: byDate.get(date), trend: trend.get(date) });
  }
  return slots;
}

export const DailyChart: React.FC<Props> = ({ dailyCosts, today, from, to, forecast }) => {
  const t = useTokens();
  // The chart is drawn at its real size rather than by scaling a fixed viewBox, which would inflate
  // the axis text on wide screens. Its height follows the width, so it keeps its shape.
  const [containerRef, width] = useContainerWidth<HTMLDivElement>(FALLBACK_WIDTH);
  const height = chartHeight(width);
  const [active, setActive] = useState<number | null>(null);

  const axisFrom = from ?? dailyCosts[0]?.date ?? today;
  const axisTo = to ?? (forecast ? forecast.monthEnd : (dailyCosts[dailyCosts.length - 1]?.date ?? today));
  // The $0 days before OpenCost had data are not measurements: the chart shows them as missing.
  const data = fromFirstCost(dailyCosts.filter(day => day.date >= axisFrom && day.date <= axisTo));
  if (data.length === 0) return null;

  const slots = toSlots(data, axisFrom, axisTo, forecast);
  const n = slots.length;
  const indexOf = (date: string) => spanDays({ from: axisFrom, to: date }) - 1;
  // Today's cost is still accumulating, so joining it to the line would draw a drop that is not there.
  const isPartial = (slot: Slot) => Boolean(slot.day && slot.date >= today);
  const measured = data.filter(day => day.date < today);
  const latest = data[data.length - 1];
  const latestIndex = indexOf(latest.date);
  const firstData = indexOf(data[0].date);

  const costs = data.map(day => day.totalCost);
  const ticks = niceTicks(Math.max(...costs, ...slots.map(slot => slot.trend ?? 0)));
  const yMax = ticks[ticks.length - 1];
  const decimals = tickDecimals(ticks);
  const plotWidth = Math.max(width - PAD.left - PAD.right, 1);
  const plotHeight = height - PAD.top - PAD.bottom;
  const baseline = PAD.top + plotHeight;
  const xOf = (i: number) => PAD.left + (n === 1 ? plotWidth / 2 : (i / (n - 1)) * plotWidth);
  const yOf = (cost: number) => baseline - (cost / yMax) * plotHeight;
  const clamp = (i: number) => Math.min(n - 1, Math.max(0, i));
  const halfDay = n === 1 ? 0 : plotWidth / (n - 1) / 2;

  const line = measured.map((day, k) => `${k === 0 ? 'M' : 'L'}${xOf(indexOf(day.date))},${yOf(day.totalCost)}`).join(' ');
  const area = measured.length
    ? `${line} L${xOf(indexOf(measured[measured.length - 1].date))},${baseline} L${xOf(indexOf(measured[0].date))},${baseline} Z`
    : '';
  const trendPath = slots
    .map((slot, i) => ({ slot, i }))
    .filter(({ slot }) => slot.trend !== undefined)
    .map(({ slot, i }, k) => `${k === 0 ? 'M' : 'L'}${xOf(i)},${yOf(slot.trend!)}`)
    .join(' ');

  // Days before the data starts are shaded, so a short history reads as missing, not as $0.
  const noDataRight = firstData > 0 ? Math.max(PAD.left, xOf(firstData) - halfDay) : PAD.left;
  // The forecast zone starts where the trend line does, on the last complete day; the month marker
  // sits on the month's first day.
  const firstForecast = forecast ? slots.findIndex(slot => slot.date >= today) : -1;
  const zoneLeft = xOf(Math.max(0, firstForecast - 1));
  const monthStartIndex = forecast ? slots.findIndex(slot => slot.date >= forecast.monthStart) : -1;
  const endY = forecast ? yOf(slots[n - 1].trend ?? 0) : 0;

  // Evenly spaced day labels, as many as the width allows, always ending on the last day without
  // crowding it.
  const labelEvery = Math.max(1, Math.ceil(n / dayLabelCount(plotWidth)));
  const labelled = slots
    .map((_, i) => i)
    .filter(i => i === n - 1 || (i % labelEvery === 0 && n - 1 - i >= labelEvery / 2));

  const onPointerMove = (event: React.PointerEvent<SVGSVGElement>) => {
    const x = event.clientX - event.currentTarget.getBoundingClientRect().left - PAD.left;
    setActive(clamp(Math.round(n === 1 ? 0 : (x / plotWidth) * (n - 1))));
  };

  // Arrow keys step through the days; the first press starts at the latest measured one.
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

  const activeSlot = active === null ? null : slots[active];
  const activeX = active === null ? 0 : xOf(active);
  const activeY = activeSlot ? yOf(activeSlot.day?.totalCost ?? activeSlot.trend ?? 0) : 0;
  const latestPartial = isPartial(slots[latestIndex]);
  const forecastLabel = forecast ? `Est. month-end ${formatEstimate(forecast.total)}` : '';

  return (
    // The chart is read by keyboard: it takes focus and the arrow keys step through its values.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      ref={containerRef}
      role="group"
      aria-label={
        `Daily cost chart, ${formatMoney(Math.min(...costs))} to ${formatMoney(Math.max(...costs))} per day.` +
        (forecast ? ` Estimated month-end: ${formatEstimate(forecast.total)}; the days after the latest are forecast.` : '') +
        ' Use the left and right arrow keys to read each day.'
      }
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      onKeyDown={onKeyDown}
      onBlur={() => setActive(null)}
      style={{
        position: 'relative',
        borderRadius: '2px',
        // The plugin stylesheet draws the ring on :focus-visible; the chart just wants more room.
        outlineOffset: '4px',
      }}
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
        {firstData > 0 && (
          <g>
            <rect x={PAD.left} y={PAD.top} width={noDataRight - PAD.left} height={plotHeight} fill={t.surfaceSunken} />
            {noDataRight - PAD.left >= 100 && (
              <text
                x={(PAD.left + noDataRight) / 2}
                y={PAD.top + plotHeight / 2}
                textAnchor="middle"
                fontSize={11}
                fill={t.textMuted}
              >
                No cost data
              </text>
            )}
          </g>
        )}
        {firstForecast >= 0 && (
          <g>
            <rect x={zoneLeft} y={PAD.top} width={xOf(n - 1) - zoneLeft} height={plotHeight} fill={t.surfaceSunken} />
            {xOf(n - 1) - zoneLeft >= 64 && (
              <text x={zoneLeft + 8} y={PAD.top + 14} fontSize={11} fill={t.textMuted}>
                Forecast
              </text>
            )}
          </g>
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
              {tick === 0 ? '$0' : formatMoney(tick, 'USD', decimals)}
            </text>
          </g>
        ))}
        {forecast && monthStartIndex > 0 && (
          <g>
            <line
              x1={xOf(monthStartIndex)}
              x2={xOf(monthStartIndex)}
              y1={PAD.top}
              y2={baseline}
              stroke={t.border}
              strokeDasharray="2 3"
            />
            <text x={xOf(monthStartIndex) + 4} y={baseline - 6} fontSize={11} fill={t.textMuted}>
              {formatMonth(forecast.monthStart)}
            </text>
          </g>
        )}
        {area && <path d={area} fill={t.accent} fillOpacity={0.1} />}
        {line && (
          <path d={line} fill="none" stroke={t.accent} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
        )}
        {trendPath && <path d={trendPath} fill="none" stroke={t.accent} strokeWidth={2} strokeDasharray={DASH} />}
        {activeSlot && <line x1={activeX} x2={activeX} y1={PAD.top} y2={baseline} stroke={t.textMuted} strokeWidth={1} />}
        <circle
          cx={xOf(latestIndex)}
          cy={yOf(latest.totalCost)}
          r={4}
          fill={latestPartial ? t.surface : t.accent}
          stroke={latestPartial ? t.accent : t.surface}
          strokeWidth={2}
        />
        {forecast && (
          <g>
            <circle cx={xOf(n - 1)} cy={endY} r={4} fill={t.accent} stroke={t.surface} strokeWidth={2} />
            <text
              x={xOf(n - 1) - 6}
              // Above the end point, unless that would run off the top of the plot.
              y={endY - 12 < PAD.top + 12 ? endY + 20 : endY - 12}
              textAnchor="end"
              fontSize={12}
              fontWeight={500}
              fill={t.textStrong}
            >
              {forecastLabel}
            </text>
          </g>
        )}
        {activeSlot && (activeSlot.day || activeSlot.trend !== undefined) && (
          <circle
            cx={activeX}
            cy={activeY}
            r={5}
            fill={activeSlot.day ? t.accent : t.surface}
            stroke={activeSlot.day ? t.surface : t.accent}
            strokeWidth={2}
          />
        )}
        {labelled.map(i => (
          <text
            key={i}
            x={xOf(i)}
            y={height - 6}
            textAnchor={i === n - 1 && n > 1 ? 'end' : 'middle'}
            fontSize={11}
            fill={t.textMuted}
          >
            {formatDay(slots[i].date)}
          </text>
        ))}
      </svg>

      {activeSlot && (
        <ChartTooltip
          slot={activeSlot}
          partial={isPartial(activeSlot)}
          left={Math.max(0, activeX + 12 + TOOLTIP_WIDTH > width ? activeX - 12 - TOOLTIP_WIDTH : activeX + 12)}
          top={activeY < height / 2 ? activeY + 12 : activeY - 12 - TOOLTIP_HEIGHT}
          t={t}
        />
      )}
      {(forecast || latestPartial) && <ChartLegend trend={Boolean(forecast)} partial={latestPartial} t={t} />}
      <div role="status" style={visuallyHidden}>
        {activeSlot ? describeSlot(activeSlot, isPartial(activeSlot)) : ''}
      </div>
    </div>
  );
};

// describeSlot is what the status region announces for a day.
function describeSlot(slot: Slot, partial: boolean): string {
  const day = formatDay(slot.date, true);
  if (!slot.day && slot.trend === undefined) return `${day}: no cost data`;
  if (!slot.day) return `${day}, forecast: ${formatMoney(slot.trend ?? 0)} per day`;
  if (partial) {
    const trend = slot.trend === undefined ? '' : `, trend ${formatMoney(slot.trend)} per day`;
    return `${day}, still being measured: ${formatMoney(slot.day.totalCost)} so far${trend}`;
  }
  return `${day}: ${formatMoney(slot.day.totalCost)} per day`;
}

interface ChartTooltipProps {
  slot: Slot;
  partial: boolean;
  left: number;
  top: number;
  t: Tokens;
}

// ChartTooltip is visual only; the status region above announces the same day to screen readers.
const ChartTooltip: React.FC<ChartTooltipProps> = ({ slot, partial, left, top, t }) => {
  const { day } = slot;
  return (
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
      <span style={{ fontSize: '11px', color: t.textMuted }}>{formatDay(slot.date, true)}</span>
      <span style={{ fontSize: '13px', fontWeight: 500, color: t.textStrong }}>
        {day
          ? `${formatMoney(day.totalCost)} ${partial ? 'so far' : '/ day'}`
          : slot.trend === undefined
            ? 'No cost data'
            : `${formatMoney(slot.trend)} / day`}
      </span>
      <span style={{ fontSize: '11px', color: t.text, fontVariantNumeric: 'tabular-nums' }}>
        {!day && slot.trend === undefined
          ? 'OpenCost reported nothing for this day'
          : !day
          ? 'Forecast from the trend'
          : partial
            ? `Still being measured${slot.trend === undefined ? '' : ` · trend ${formatMoney(slot.trend)} / day`}`
            : `CPU ${formatMoney(day.cpuCost)} · Memory ${formatMoney(day.memoryCost)} · Other ${formatMoney(day.networkCost + day.storageCost)}`}
      </span>
    </div>
  );
};

// ChartLegend names the marks beyond the plain daily line; it repeats nothing the status region says.
const ChartLegend: React.FC<{ trend: boolean; partial: boolean; t: Tokens }> = ({ trend, partial, t }) => {
  const swatch = (dashed: boolean) => (
    <svg width={22} height={8} aria-hidden="true" focusable="false">
      <line x1={0} x2={22} y1={4} y2={4} stroke={t.accent} strokeWidth={2} strokeDasharray={dashed ? DASH : undefined} />
    </svg>
  );
  const item: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: '6px' };
  return (
    <div
      aria-hidden="true"
      style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 16px', marginTop: '8px', fontSize: '12px', color: t.textMuted }}
    >
      <span style={item}>{swatch(false)} Daily cost</span>
      {trend && <span style={item}>{swatch(true)} Trend to month-end</span>}
      {partial && (
        <span style={item}>
          <svg width={12} height={12} aria-hidden="true" focusable="false">
            <circle cx={6} cy={6} r={4} fill={t.surface} stroke={t.accent} strokeWidth={2} />
          </svg>
          {trend ? 'Today, still being measured (not in the trend)' : 'Today, still being measured'}
        </span>
      )}
    </div>
  );
};
