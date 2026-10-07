import React from 'react';
import { useContainerWidth } from '../hooks/useContainerWidth';
import { cardStyle, useTokens } from '../theme/tokens';
import { statColumns } from '../utils/layout';

export interface Stat {
  label: string;
  value: React.ReactNode;
  detail?: React.ReactNode;
}

interface Props {
  stats: Stat[];
  // flush drops the space below the strip, for when it sits in a row that provides its own.
  flush?: boolean;
}

// StatStrip is one card of side-by-side figures, styled after Argo CD's status panel. It folds to
// fit its own width — every stat on one row, then two columns, then one — so it works both across
// a page and inside a narrow column. Dividers sit between stats on the same row only.
export const StatStrip: React.FC<Props> = ({ stats, flush = false }) => {
  const t = useTokens();
  // Until the strip is measured, and wherever there is no layout, assume room for one row.
  const [ref, width] = useContainerWidth<HTMLDListElement>(Number.POSITIVE_INFINITY);
  const columns = statColumns(width, stats.length);

  return (
    <dl
      ref={ref}
      style={{
        ...cardStyle(t),
        display: 'grid',
        gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
        rowGap: '24px',
        margin: flush ? 0 : '0 0 16px',
        padding: '20px 0',
      }}
    >
      {stats.map((stat, i) => (
        <div
          key={stat.label}
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: '6px',
            minWidth: 0,
            padding: '0 24px',
            borderLeft: i % columns === 0 ? 'none' : `1px solid ${t.border}`,
          }}
        >
          <dt
            style={{
              fontSize: '12px',
              fontWeight: 500,
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
              color: t.textMuted,
            }}
          >
            {stat.label}
          </dt>
          <dd style={{ margin: 0, fontSize: '22.4px', fontWeight: 500, lineHeight: '28px', color: t.textStrong }}>
            {stat.value}
          </dd>
          {stat.detail && <dd style={{ margin: 0, fontSize: '12px', color: t.textMuted }}>{stat.detail}</dd>}
        </div>
      ))}
    </dl>
  );
};
