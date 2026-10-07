import React from 'react';
import { cardStyle, useTokens } from '../theme/tokens';
import { NamespaceSummary } from '../utils/api';
import { formatMoney, formatPercent, pluralize } from '../utils/format';

interface Props {
  namespaces: NamespaceSummary[];
  selected: string | null;
  onSelect: (namespace: string) => void;
  clusterTotal: number;
}

// NamespaceList lets the viewer pick a namespace, showing each one's cost and share of the cluster.
export const NamespaceList: React.FC<Props> = ({ namespaces, selected, onSelect, clusterTotal }) => {
  const t = useTokens();

  return (
    <nav aria-label="Namespaces" style={{ ...cardStyle(t), padding: '8px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', padding: '8px 12px 10px' }}>
        <span style={{ color: t.textMuted, fontSize: '12px', fontWeight: 500, textTransform: 'uppercase' }}>Namespaces</span>
        <span style={{ color: t.textMuted, fontSize: '12px' }}>By monthly cost</span>
      </div>
      <ul style={{ display: 'flex', flexDirection: 'column', gap: '2px', margin: 0, padding: 0, listStyle: 'none' }}>
        {namespaces.map(ns => {
          const isSelected = ns.namespace === selected;
          const share = clusterTotal > 0 ? ns.totalCost / clusterTotal : 0;
          return (
            <li key={ns.namespace}>
              <button
                type="button"
                aria-current={isSelected ? 'true' : undefined}
                onClick={() => onSelect(ns.namespace)}
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '8px',
                  width: '100%',
                  padding: '10px 12px',
                  border: 'none',
                  borderRadius: '4px',
                  backgroundColor: isSelected ? t.accentWash : 'transparent',
                  color: isSelected ? t.textStrong : t.text,
                  fontFamily: 'inherit',
                  textAlign: 'left',
                  cursor: 'pointer',
                }}
              >
                <span
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    gap: '12px',
                    width: '100%',
                    fontSize: '13px',
                    fontVariantNumeric: 'tabular-nums',
                  }}
                >
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontWeight: 500 }}>
                    {ns.namespace}
                  </span>
                  <span>{formatMoney(ns.totalCost)}</span>
                </span>
                <span style={{ display: 'flex', alignItems: 'center', gap: '10px', width: '100%' }}>
                  <span
                    aria-hidden="true"
                    style={{ flex: 1, height: '4px', overflow: 'hidden', borderRadius: '2px', backgroundColor: t.accentWash }}
                  >
                    <span style={{ display: 'block', width: `${Math.min(share, 1) * 100}%`, height: '100%', backgroundColor: t.accent }} />
                  </span>
                  <span style={{ minWidth: '84px', color: t.textMuted, fontSize: '12px', textAlign: 'right' }}>
                    {formatPercent(share)} · {pluralize(ns.appCount, 'app')}
                  </span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
};
