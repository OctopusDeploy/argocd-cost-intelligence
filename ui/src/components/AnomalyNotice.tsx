import React from 'react';
import { ErrorIcon, WarningIcon } from './icons';
import { cardStyle, useTokens } from '../theme/tokens';
import { CostAnomaly } from '../utils/api';
import { formatMoney } from '../utils/format';

// AnomalyNotice reports a cost that departs from the rolling average of recent syncs.
export const AnomalyNotice: React.FC<{ anomaly: CostAnomaly }> = ({ anomaly }) => {
  const t = useTokens();
  const alert = anomaly.severity === 'alert';

  return (
    <div
      role="status"
      style={{
        ...cardStyle(t),
        display: 'flex',
        alignItems: 'flex-start',
        gap: '12px',
        marginBottom: '16px',
        padding: '14px 24px',
        color: t.text,
        fontSize: '13px',
      }}
    >
      <span style={{ display: 'flex', marginTop: '2px' }}>
        {alert ? <ErrorIcon color={t.danger} /> : <WarningIcon color={t.warning} size={16} />}
      </span>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
        <span style={{ color: t.textStrong, fontWeight: 500 }}>{alert ? 'Cost alert' : 'Cost warning'}</span>
        <span>{anomaly.message}</span>
        <span style={{ color: t.textMuted, fontSize: '12px' }}>
          Rolling average over the last {anomaly.recordCount} syncs: {formatMoney(anomaly.rollingAvg)}/mo
        </span>
      </div>
    </div>
  );
};
