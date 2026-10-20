import React, { useState } from 'react';
import { ChevronIcon, WarningIcon } from './icons';
import { useOrphanedPods } from '../hooks/useOrphanedPods';
import { cardStyle, useTokens } from '../theme/tokens';
import { AppContext } from '../utils/api';
import { formatMoney } from '../utils/format';

const LIST_ID = 'cost-intel-orphaned-pods';

// OrphanedPodsNotice reports namespace pods with no CPU or memory use, collapsed by default.
// It renders nothing while loading or when there are none. On an application's page, pass appName and
// appCtx: the namespace is then the one the application deploys to, fetched as that application.
export const OrphanedPodsNotice: React.FC<{ namespace: string; appName?: string; appCtx?: AppContext }> = ({
  namespace,
  appName,
  appCtx,
}) => {
  const t = useTokens();
  const { pods, totalWasted, loading } = useOrphanedPods(namespace, appName, appCtx);
  const [open, setOpen] = useState(false);

  if (loading || pods.length === 0) return null;

  const headStyle: React.CSSProperties = {
    padding: '8px 12px 8px 0',
    borderBottom: `1px solid ${t.border}`,
    color: t.textStrong,
    fontSize: '12.8px',
    fontWeight: 600,
    textAlign: 'left',
  };
  const cellStyle: React.CSSProperties = {
    padding: '8px 12px 8px 0',
    borderBottom: `1px solid ${t.hairline}`,
    textAlign: 'left',
  };

  return (
    <section aria-label="Orphaned pods" style={{ ...cardStyle(t), marginTop: '16px', overflow: 'hidden' }}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={LIST_ID}
        onClick={() => setOpen(current => !current)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
          width: '100%',
          padding: '14px 24px',
          border: 'none',
          backgroundColor: 'transparent',
          color: t.text,
          fontFamily: 'inherit',
          fontSize: '13px',
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        <WarningIcon color={t.warning} />
        <span style={{ flex: 1 }}>
          <span style={{ color: t.textStrong, fontWeight: 500 }}>
            {pods.length} orphaned {pods.length === 1 ? 'pod' : 'pods'}
          </span>{' '}
          in {namespace}, {formatMoney(totalWasted)}/mo with no CPU or memory use
        </span>
        <ChevronIcon expanded={open} />
      </button>

      {open && (
        <div id={LIST_ID} style={{ padding: '4px 24px 16px', borderTop: `1px solid ${t.border}` }}>
          <div style={{ overflowX: 'auto' }}>
            <table
              style={{ width: '100%', borderCollapse: 'collapse', color: t.text, fontSize: '13px', fontVariantNumeric: 'tabular-nums' }}
            >
              <thead>
                <tr>
                  <th scope="col" style={headStyle}>
                    Pod
                  </th>
                  <th scope="col" style={headStyle}>
                    Controller
                  </th>
                  <th scope="col" style={headStyle}>
                    Kind
                  </th>
                  <th scope="col" style={{ ...headStyle, textAlign: 'right' }}>
                    Storage / mo
                  </th>
                  <th scope="col" style={{ ...headStyle, padding: '8px 0', textAlign: 'right' }}>
                    Total / mo
                  </th>
                </tr>
              </thead>
              <tbody>
                {pods.map(pod => (
                  <tr key={pod.name}>
                    <th scope="row" style={{ ...cellStyle, fontWeight: 400, wordBreak: 'break-all' }}>
                      {pod.name}
                    </th>
                    <td style={cellStyle}>{pod.controller || '—'}</td>
                    <td style={cellStyle}>{pod.controllerKind || '—'}</td>
                    <td style={{ ...cellStyle, textAlign: 'right' }}>
                      {pod.storageCost > 0 ? formatMoney(pod.storageCost) : '—'}
                    </td>
                    <td style={{ ...cellStyle, padding: '8px 0', color: t.textStrong, textAlign: 'right' }}>
                      {formatMoney(pod.totalCost)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p style={{ margin: '12px 0 0', color: t.textMuted, fontSize: '12px', lineHeight: '18px' }}>
            These pods report no CPU or memory use. They may be evicted, stuck in Pending, or finished Jobs still
            holding volumes or load balancers.
          </p>
        </div>
      )}
    </section>
  );
};
