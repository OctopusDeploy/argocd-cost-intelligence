import React from 'react';
import { TrendIcon } from './icons';
import { cardStyle, Tokens, useTokens } from '../theme/tokens';
import { ChangeDriver, ChangeDrivers, NOT_TIED_TO_A_POD } from '../utils/costInsights';
import { formatMoney, formatSignedMoney, pluralize } from '../utils/format';

const TITLE_ID = 'cost-intel-drivers-title';

const STATUS_LABELS: Record<NonNullable<ChangeDriver['status']>, string | null> = {
  added: 'New',
  removed: 'Removed',
  changed: null,
};

// ChangeDriversCard explains the change in monthly cost since the previous sync, controller by
// controller. Its rows add up to the total change shown on the last row.
export const ChangeDriversCard: React.FC<{ drivers: ChangeDrivers }> = ({ drivers }) => {
  const t = useTokens();
  if (drivers.drivers.length === 0) return null;

  const rowHeader: React.CSSProperties = { padding: '8px 12px 8px 0', fontWeight: 400, textAlign: 'left', minWidth: 0 };
  const amount: React.CSSProperties = { padding: '8px 0', color: t.textStrong, textAlign: 'right', verticalAlign: 'top' };
  const detail: React.CSSProperties = { display: 'block', marginTop: '2px', color: t.textMuted, fontSize: '12px' };
  const truncated: React.CSSProperties = { display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };

  return (
    <section aria-labelledby={TITLE_ID} style={{ ...cardStyle(t), padding: '20px 24px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '16px', marginBottom: '8px' }}>
        <h4 id={TITLE_ID} style={{ margin: 0, color: t.textStrong, fontSize: '14px', fontWeight: 500, lineHeight: '20px' }}>
          What changed
        </h4>
        <span style={{ color: t.textMuted, fontSize: '12px' }}>Monthly, since the previous sync</span>
      </div>

      <table
        aria-labelledby={TITLE_ID}
        style={{
          width: '100%',
          tableLayout: 'fixed',
          borderCollapse: 'collapse',
          color: t.text,
          fontSize: '13px',
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        <colgroup>
          <col />
          <col style={{ width: '120px' }} />
        </colgroup>
        <tbody>
          {drivers.drivers.map(driver => (
            <tr key={driver.key} style={{ borderTop: `1px solid ${t.hairline}` }}>
              <th scope="row" style={rowHeader}>
                <span title={driver.name} style={{ ...truncated, color: t.textStrong }}>
                  {driver.name}
                </span>
                <span style={{ ...detail, ...truncated }}>{driverDetail(driver)}</span>
              </th>
              <td style={amount}>
                <SignedAmount amount={driver.delta} t={t} />
              </td>
            </tr>
          ))}
          {drivers.smallerCount > 0 && (
            <tr style={{ borderTop: `1px solid ${t.hairline}` }}>
              <th scope="row" style={{ ...rowHeader, color: t.textMuted }}>
                {pluralize(drivers.smallerCount, 'smaller change')}
              </th>
              <td style={{ ...amount, color: t.text }}>{formatSignedMoney(drivers.smaller)}</td>
            </tr>
          )}
          <tr style={{ borderTop: `1px solid ${t.border}` }}>
            <th scope="row" style={{ ...rowHeader, padding: '10px 12px 10px 0', color: t.textStrong, fontWeight: 500 }}>
              Total change
            </th>
            <td style={{ ...amount, padding: '10px 0', fontWeight: 500 }}>{formatSignedMoney(drivers.total)}</td>
          </tr>
        </tbody>
      </table>

      <p style={{ margin: '12px 0 0', color: t.textMuted, fontSize: '12px', lineHeight: '18px' }}>
        Each measurement covers the 24 hours before it, so a change made at a sync only shows in full a day later.
      </p>
    </section>
  );
};

function driverDetail(driver: ChangeDriver): string {
  if (driver.key === NOT_TIED_TO_A_POD) return 'Unmounted volumes and other costs OpenCost does not assign to a pod';
  const status = driver.status ? STATUS_LABELS[driver.status] : null;
  const parts = [driver.kind, status];
  parts.push(`${formatMoney(driver.before)} → ${formatMoney(driver.after)}`);
  return parts.filter(Boolean).join(' · ');
}

// A falling cost is good news and a rising one is worth a look, as in the "Since last sync" figure.
const SignedAmount: React.FC<{ amount: number; t: Tokens }> = ({ amount, t }) => (
  <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
    <TrendIcon direction={amount < 0 ? 'down' : 'up'} color={amount < 0 ? t.success : t.danger} size={14} />
    {formatSignedMoney(amount)}
  </span>
);
