import React from 'react';
import { WarningIcon } from './icons';
import { useTokens } from '../theme/tokens';
import { formatHours, formatRelativeTime, isPartialMeasurement, isStale } from '../utils/format';

interface Props {
  measuredAt: string;
  // measuredHours is how much data OpenCost had for the measurement; less than a day is flagged.
  measuredHours?: number;
  title?: string;
}

// MeasuredLabel says how long ago data was measured. It flags it as stale after STALE_AFTER_DAYS, and as
// partial when OpenCost had less than a day of data, since monthly figures are then projected from a few
// hours and change once a full day is measured.
export const MeasuredLabel: React.FC<Props> = ({ measuredAt, measuredHours, title }) => {
  const t = useTokens();
  const measured = formatRelativeTime(measuredAt);
  if (!measured) return null;

  const flags: string[] = [];
  if (isStale(measuredAt)) flags.push('Stale');
  if (measuredHours !== undefined && isPartialMeasurement(measuredHours)) {
    flags.push(`Projected from ${formatHours(measuredHours)} of data`);
  }

  return (
    <span
      title={title ?? new Date(measuredAt).toLocaleString()}
      style={{ display: 'flex', alignItems: 'center', gap: '6px', color: t.textMuted, fontSize: '12.8px' }}
    >
      {flags.length > 0 && <WarningIcon color={t.warning} />}
      {flags.map(flag => (
        <React.Fragment key={flag}>
          <span style={{ color: t.text }}>{flag}</span>
          <span aria-hidden="true">·</span>
        </React.Fragment>
      ))}
      <span>Measured {measured}</span>
    </span>
  );
};
