import React, { useEffect, useState } from 'react';
import { CardHeader } from './CardHeader';
import { Chip } from './Chip';
import { DailyChart } from './DailyChart';
import { ChartIcon } from './icons';
import { ErrorNotice, Notice } from './Notice';
import { PeriodChart } from './PeriodChart';
import { StatStrip } from './StatStrip';
import { cardStyle, Tokens, useTokens } from '../theme/tokens';
import { DailyCost } from '../utils/api';
import { summarizeDaily } from '../utils/dailyStats';
import { forecastMonthEnd, MIN_TREND_DAYS, utcDay } from '../utils/forecast';
import { formatDay, formatDayIn, formatEstimate, formatMoney, pluralize, roundColumns, roundParts, sumRounded } from '../utils/format';
import {
  bucketDays,
  completeDays,
  customRangeError,
  DayRange,
  fromFirstCost,
  granularity,
  includesToday,
  MAX_RANGE_DAYS,
  PRESETS,
  presetRange,
  queryRange,
  RangeId,
} from '../utils/ranges';

// RangeView is the range picker's state, and the series for the chosen range.
export interface RangeView {
  id: RangeId;
  range: DayRange;
  choose: (id: Exclude<RangeId, 'custom'>) => void;
  setCustom: (range: DayRange) => void;
  // The chosen range's own series; the 30-day range uses the tab's series instead.
  dailyCosts: DailyCost[] | null;
  loading: boolean;
  error: string | null;
}

interface CostTrendProps {
  loading: boolean;
  error: string | null;
  // dailyCosts is the last 30 days: what the forecast is fitted to, and the 30-day range's data.
  dailyCosts: DailyCost[] | null;
  // namespace is set when the application shares its namespace: the daily series covers all of it.
  namespace?: string;
  // range adds the range picker; without it the tab shows dailyCosts as it is.
  range?: RangeView;
}

const TITLE_ID = 'cost-intel-daily-title';
const CUSTOM_ID = 'cost-intel-custom-range';
const TITLES = { day: 'Daily cost', week: 'Weekly cost', month: 'Monthly cost' };

// noDataReason explains days without cost data. The service records every day OpenCost reports, so
// a missing day predates the pods, or was gone from OpenCost's Prometheus before recording began.
const noDataReason = (namespace?: string) =>
  `This ${namespace ? 'namespace' : 'app'}\u2019s pods had not run yet, or OpenCost no longer had those days to record.`;

export const CostTrendComponent: React.FC<CostTrendProps> = ({ loading, error, dailyCosts, namespace, range }) => {
  const t = useTokens();
  const [view, setView] = useState<'chart' | 'table'>('chart');

  if (loading && !dailyCosts) return <Notice>Loading daily costs…</Notice>;
  if (error) return <ErrorNotice message={error} />;

  const recent = dailyCosts ?? [];
  if (recent.length === 0) return <Notice>No daily cost data yet.</Notice>;
  const now = new Date();
  const today = utcDay(now);
  // The tab's own series is the last 30 days, asked for from this day.
  const recentFrom = presetRange('30d', today).from;
  const forecast = forecastMonthEnd(recent, recentFrom, now);

  // The range on screen, and its series: the tab's own for the last 30 days, else fetched for it.
  const ownSeries = !range || range.id === '30d';
  const shown: DayRange = range?.range ?? { from: recent[0].date, to: recent[recent.length - 1].date };
  const series = ownSeries ? recent : range.dailyCosts;
  const seriesLoading = !ownSeries && range.loading && !range.dailyCosts;
  const seriesError = ownSeries ? null : range.error;
  const inRange = (series ?? []).filter(day => day.date >= shown.from && day.date <= shown.to);
  const data = fromFirstCost(inRange);
  const reachesToday = !range || includesToday(shown, today);
  const showForecast = reachesToday ? forecast : null;
  const unit = range ? granularity(shown) : 'day';
  // Today is still being measured, and so was the first day measured, so either would read as the
  // cheapest day: the per-day figures leave them out, while the total, which is money already
  // spent, keeps them.
  const complete = completeDays(inRange, today, range?.range.from ?? recentFrom);
  const summary = summarizeDaily(complete.length > 0 ? complete : data);
  // The sum of the days as the table lists them, so the table adds up to it.
  const total = sumRounded(data.map(day => day.totalCost));
  // Rounded as the forecast is shown, so what is spent and what is projected add up to it.
  const [spent, projected] = showForecast
    ? roundParts([showForecast.spent, showForecast.projected], showForecast.total, Math.abs(showForecast.total) >= 100 ? 0 : 2)
    : [0, 0];
  // Without a forecast, say how many more days of data it needs, so its absence is not a mystery.
  const daysToForecast = forecast || !reachesToday ? 0 : MIN_TREND_DAYS - completeDays(recent, today, recentFrom).length;
  const axisTo = showForecast && showForecast.monthEnd > shown.to ? showForecast.monthEnd : shown.to;
  // The days there can be data for; a span crossing a year names the year with each day.
  const shownDays = queryRange(shown, today);
  const dayName = (day: string) => formatDayIn(day, shownDays);

  return (
    <div>
      {summary && !seriesLoading && !seriesError && (
        <StatStrip
          stats={[
            {
              label: 'Average per day',
              value: formatMoney(summary.average),
              detail: `Over ${pluralize(complete.length || data.length, complete.length ? 'complete day' : 'day')}`,
            },
            { label: 'Highest day', value: formatMoney(summary.highest.totalCost), detail: dayName(summary.highest.date) },
            { label: 'Lowest day', value: formatMoney(summary.lowest.totalCost), detail: dayName(summary.lowest.date) },
            {
              label: 'Total',
              value: formatMoney(total),
              detail: `${dayName(data[0].date)} – ${dayName(data[data.length - 1].date)}`,
            },
            ...(showForecast
              ? [
                  {
                    label: 'Month-end forecast',
                    value: formatEstimate(showForecast.total),
                    detail: `${formatEstimate(spent, showForecast.total)} so far + ${formatEstimate(projected, showForecast.total)} projected`,
                  },
                ]
              : []),
          ]}
        />
      )}

      <section aria-labelledby={TITLE_ID} style={{ ...cardStyle(t), padding: '20px 24px 24px' }}>
        <div style={{ marginBottom: '16px' }}>
          <CardHeader
            id={TITLE_ID}
            icon={<ChartIcon />}
            title={TITLES[unit]}
            detail={
              namespace
                ? `Whole ${namespace} namespace, not only this app, queried from OpenCost for each day`
                : 'Queried from OpenCost for each day, independent of sync frequency'
            }
            action={
              <button
                type="button"
                onClick={() => setView(current => (current === 'chart' ? 'table' : 'chart'))}
                style={{
                  height: '32px',
                  padding: '0 14px',
                  border: `1px solid ${t.accent}`,
                  borderRadius: '4px',
                  backgroundColor: 'transparent',
                  color: t.accentText,
                  fontFamily: 'inherit',
                  fontSize: '13px',
                  fontWeight: 500,
                  cursor: 'pointer',
                }}
              >
                {view === 'chart' ? 'View as table' : 'View as chart'}
              </button>
            }
          />
        </div>

        {range && <RangePicker range={range} today={today} t={t} />}

        {seriesLoading ? (
          <Notice>Loading daily costs…</Notice>
        ) : seriesError ? (
          <ErrorNotice message={seriesError} />
        ) : data.length === 0 ? (
          <Notice>
            No cost data from {dayName(shownDays.from)} to {dayName(shownDays.to)}. {noDataReason(namespace)}
          </Notice>
        ) : (
          <>
            {data[0].date > shown.from && (
              <p style={{ margin: '0 0 12px', fontSize: '12px', color: t.textMuted }}>
                No cost data before {dayName(data[0].date)}. {noDataReason(namespace)}
              </p>
            )}
            {view === 'table' ? (
              <DailyCostTable days={data} t={t} />
            ) : unit === 'day' ? (
              <DailyChart
                dailyCosts={inRange}
                today={today}
                from={range ? shown.from : undefined}
                to={range ? (showForecast ? axisTo : queryRange(shown, today).to) : undefined}
                forecast={showForecast}
              />
            ) : (
              <PeriodChart buckets={bucketDays(inRange, shown, unit, today, data[0].date)} unit={unit} />
            )}
          </>
        )}
        {daysToForecast > 0 && (
          <p style={{ margin: '12px 0 0', fontSize: '12px', color: t.textMuted }}>
            A month-end forecast appears after {MIN_TREND_DAYS} complete days of cost data,{' '}
            {pluralize(daysToForecast, 'more day')} to go.
          </p>
        )}
      </section>
    </div>
  );
};

interface RangePickerProps {
  range: RangeView;
  today: string;
  t: Tokens;
}

// RangePicker offers the preset ranges as chips, applied at once, and a custom range whose dates
// apply as soon as they make a valid range. Custom starts from the range on screen.
const RangePicker: React.FC<RangePickerProps> = ({ range, today, t }) => {
  const custom = range.id === 'custom';
  return (
    <div style={{ marginBottom: '16px' }}>
      <div role="group" aria-label="Date range" style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
        {PRESETS.map(preset => (
          <Chip key={preset.id} pressed={range.id === preset.id} onClick={() => range.choose(preset.id)}>
            {preset.label}
          </Chip>
        ))}
        <Chip
          pressed={custom}
          aria-expanded={custom}
          aria-controls={CUSTOM_ID}
          onClick={() => {
            if (!custom) range.setCustom(queryRange(range.range, today));
          }}
        >
          Custom…
        </Chip>
      </div>
      {custom && <CustomRange range={range} today={today} t={t} />}
    </div>
  );
};

const CustomRange: React.FC<RangePickerProps> = ({ range, today, t }) => {
  const { from, to } = range.range;
  const [draft, setDraft] = useState<DayRange>({ from, to });
  // Back and Forward change the range from outside; the fields follow.
  useEffect(() => setDraft({ from, to }), [from, to]);
  const problem = customRangeError(draft, today);

  const edit = (next: DayRange) => {
    setDraft(next);
    if (customRangeError(next, today) === null) range.setCustom(next);
  };
  const field: React.CSSProperties = {
    padding: '5px 8px',
    border: `1px solid ${t.border}`,
    borderRadius: '4px',
    backgroundColor: t.surfaceSunken,
    color: t.text,
    fontFamily: 'inherit',
    fontSize: '13px',
  };
  const label: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '12.8px', color: t.textMuted };

  return (
    <div id={CUSTOM_ID} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '8px 16px', marginTop: '12px' }}>
      <label htmlFor={`${CUSTOM_ID}-from`} style={label}>
        From
        <input
          id={`${CUSTOM_ID}-from`}
          type="date"
          value={draft.from}
          max={today}
          onChange={event => edit({ ...draft, from: event.target.value })}
          style={field}
        />
      </label>
      <label htmlFor={`${CUSTOM_ID}-to`} style={label}>
        To
        <input
          id={`${CUSTOM_ID}-to`}
          type="date"
          value={draft.to}
          onChange={event => edit({ ...draft, to: event.target.value })}
          style={field}
        />
      </label>
      <span role="status" style={{ fontSize: '12px', color: problem ? t.danger : t.textMuted }}>
        {problem ?? `Up to ${MAX_RANGE_DAYS} days.`}
      </span>
    </div>
  );
};

const COST_COLUMNS: Array<[string, (day: DailyCost) => number]> = [
  ['CPU', day => day.cpuCost],
  ['Memory', day => day.memoryCost],
  ['Network', day => day.networkCost],
  ['Storage', day => day.storageCost],
];

// DailyCostTable is the accessible alternative to the chart, newest day first.
const DailyCostTable: React.FC<{ days: DailyCost[]; t: Tokens }> = ({ days, t }) => {
  const headStyle: React.CSSProperties = {
    position: 'sticky',
    top: 0,
    padding: '8px 12px',
    backgroundColor: t.surface,
    borderBottom: `1px solid ${t.border}`,
    color: t.textStrong,
    fontSize: '12.8px',
    fontWeight: 600,
    textAlign: 'right',
  };
  const cellStyle: React.CSSProperties = {
    padding: '8px 12px',
    borderBottom: `1px solid ${t.hairline}`,
    textAlign: 'right',
  };

  return (
    <div style={{ maxHeight: '320px', overflow: 'auto' }}>
      <table
        aria-labelledby={TITLE_ID}
        style={{ width: '100%', borderCollapse: 'collapse', color: t.text, fontSize: '13px', fontVariantNumeric: 'tabular-nums' }}
      >
        <thead>
          <tr>
            <th scope="col" style={{ ...headStyle, textAlign: 'left' }}>
              Day
            </th>
            <th scope="col" style={headStyle}>
              Total
            </th>
            {COST_COLUMNS.map(([label]) => (
              <th key={label} scope="col" style={headStyle}>
                {label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {[...days].reverse().map(day => {
            // Rounded so the columns never add up to more than the total, which also holds what has no
            // column, such as load balancers and GPUs.
            const columns = roundColumns(COST_COLUMNS.map(([, cost]) => cost(day)), day.totalCost);
            return (
              <tr key={day.date}>
                <th scope="row" style={{ ...cellStyle, fontWeight: 400, textAlign: 'left' }}>
                  {formatDay(day.date, true)}
                </th>
                <td style={{ ...cellStyle, color: t.textStrong }}>{formatMoney(day.totalCost)}</td>
                {COST_COLUMNS.map(([label], index) => (
                  <td key={label} style={cellStyle}>
                    {formatMoney(columns[index])}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};
