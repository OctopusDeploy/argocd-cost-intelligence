import React, { useState } from 'react';
import { ChevronIcon } from './icons';
import { cardStyle, Tokens, useTokens } from '../theme/tokens';
import { formatMoney, formatPercent } from '../utils/format';
import { NAME_COLUMN, SPACER_COLUMN } from '../utils/layout';
import { ControllerGroup } from '../utils/podGroups';

// Expand, controller, kind, pods, CPU, memory, total, share, then a spacer. The controller name
// grows to 480px and the spacer takes any width beyond that (see NAME_COLUMN).
const COLUMNS = `48px ${NAME_COLUMN} 112px 56px 104px 104px 104px 132px ${SPACER_COLUMN}`;
const MIN_TABLE_WIDTH = '820px';

const plural = (count: number, noun: string) => `${count} ${noun}${count === 1 ? '' : 's'}`;

interface Props {
  groups: ControllerGroup[];
}

// ControllerCostTable lists controllers, each expandable to its pods. It uses ARIA table roles on
// CSS grid rows so a controller and its pods can share one card, like Argo CD's own table rows.
export const ControllerCostTable: React.FC<Props> = ({ groups }) => {
  const t = useTokens();
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const total = groups.reduce((sum, group) => sum + group.totalCost, 0);
  const podCount = groups.reduce((sum, group) => sum + group.pods.length, 0);

  const toggle = (key: string) =>
    setExpanded(current => {
      const next = new Set(current);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  const row: React.CSSProperties = {
    display: 'grid',
    gridTemplateColumns: COLUMNS,
    alignItems: 'center',
    color: t.text,
    fontSize: '13px',
    fontVariantNumeric: 'tabular-nums',
  };
  const cell: React.CSSProperties = { minWidth: 0, padding: '0 15px' };
  const numeric: React.CSSProperties = { ...cell, textAlign: 'right' };
  const truncated: React.CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };

  return (
    <div>
      <div style={{ marginBottom: '4px', color: t.textMuted, fontSize: '12.8px' }}>
        {plural(groups.length, 'controller')} · {plural(podCount, 'pod')} · monthly cost
      </div>
      <div style={{ overflowX: 'auto' }}>
        <div role="table" aria-label="Monthly cost by controller" style={{ minWidth: MIN_TABLE_WIDTH }}>
          <div role="rowgroup">
            <div
              role="row"
              style={{
                ...row,
                height: '18px',
                margin: '8px 0',
                color: t.textStrong,
                fontSize: '12.8px',
                fontWeight: 600,
                whiteSpace: 'nowrap',
              }}
            >
              <span role="columnheader" aria-label="Pods list" />
              <span role="columnheader" style={cell}>
                Controller
              </span>
              <span role="columnheader" style={cell}>
                Kind
              </span>
              <span role="columnheader" style={numeric}>
                Pods
              </span>
              <span role="columnheader" style={numeric}>
                CPU
              </span>
              <span role="columnheader" style={numeric}>
                Memory
              </span>
              <span role="columnheader" style={numeric}>
                Total
              </span>
              <span role="columnheader" style={numeric}>
                Share
              </span>
            </div>
          </div>

          {groups.map((group, index) => {
            const open = expanded.has(group.key);
            const groupId = `cost-intel-controller-${index}`;
            return (
              <div key={group.key} id={groupId} role="rowgroup" style={{ ...cardStyle(t), margin: '8px 0', overflow: 'hidden' }}>
                <div role="row" style={{ ...row, minHeight: '52px' }}>
                  <span role="cell" style={{ paddingLeft: '12px' }}>
                    <button
                      type="button"
                      aria-expanded={open}
                      aria-controls={groupId}
                      aria-label={`Pods of ${group.name}`}
                      onClick={() => toggle(group.key)}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: '28px',
                        height: '28px',
                        padding: 0,
                        border: 'none',
                        borderRadius: '4px',
                        backgroundColor: t.surfaceSunken,
                        color: t.text,
                        cursor: 'pointer',
                      }}
                    >
                      <ChevronIcon expanded={open} />
                    </button>
                  </span>
                  <span
                    role="rowheader"
                    title={group.name}
                    style={{ ...cell, ...truncated, color: t.textStrong, fontWeight: 500 }}
                  >
                    {group.name}
                  </span>
                  <span role="cell" style={{ ...cell, ...truncated }}>
                    {group.kind}
                  </span>
                  <span role="cell" style={numeric}>
                    {group.pods.length}
                  </span>
                  <span role="cell" style={numeric}>
                    {formatMoney(group.cpuCost)}
                  </span>
                  <span role="cell" style={numeric}>
                    {formatMoney(group.ramCost)}
                  </span>
                  <span role="cell" style={{ ...numeric, color: t.textStrong, fontWeight: 500 }}>
                    {formatMoney(group.totalCost)}
                  </span>
                  <span role="cell" style={cell}>
                    <Share fraction={group.share} t={t} />
                  </span>
                </div>

                {open &&
                  group.pods.map(pod => (
                    <div
                      key={pod.name}
                      role="row"
                      style={{ ...row, minHeight: '40px', backgroundColor: t.surfaceSunken, borderTop: `1px solid ${t.border}` }}
                    >
                      <span role="cell" />
                      <span role="rowheader" title={pod.name} style={{ ...cell, ...truncated, paddingLeft: '31px' }}>
                        {pod.name}
                      </span>
                      <span role="cell" style={{ ...cell, color: t.textMuted }}>
                        Pod
                      </span>
                      <span role="cell" />
                      <span role="cell" style={numeric}>
                        {formatMoney(pod.cpuCost)}
                      </span>
                      <span role="cell" style={numeric}>
                        {formatMoney(pod.ramCost)}
                      </span>
                      <span role="cell" style={numeric}>
                        {formatMoney(pod.totalCost)}
                      </span>
                      <span role="cell" style={cell}>
                        <Share fraction={total > 0 ? pod.totalCost / total : 0} t={t} />
                      </span>
                    </div>
                  ))}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};

// Share shows a fraction of the listed total as a small bar and a percentage.
export const Share: React.FC<{ fraction: number; t: Tokens }> = ({ fraction, t }) => (
  <span style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '8px' }}>
    <span
      aria-hidden="true"
      style={{ width: '56px', height: '6px', overflow: 'hidden', borderRadius: '3px', backgroundColor: t.accentWash }}
    >
      <span style={{ display: 'block', width: `${Math.min(fraction, 1) * 100}%`, height: '100%', backgroundColor: t.accent }} />
    </span>
    <span style={{ minWidth: '40px', textAlign: 'right' }}>{formatPercent(fraction)}</span>
  </span>
);
