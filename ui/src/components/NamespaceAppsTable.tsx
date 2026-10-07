import React, { useState } from 'react';
import { Share } from './ControllerCostTable';
import { ChevronIcon, WarningIcon } from './icons';
import { cardStyle, useTokens } from '../theme/tokens';
import { formatMoney, formatRelativeTime, isStale, pluralize } from '../utils/format';
import { NAME_COLUMN, SPACER_COLUMN } from '../utils/layout';
import { AppCost } from '../utils/namespaceApps';

// Expand, application, CPU, memory, total, share, measured, then a spacer. The application name
// grows to 480px and the spacer takes any width beyond that (see NAME_COLUMN).
const COLUMNS = `48px ${NAME_COLUMN} 104px 104px 116px 132px 140px ${SPACER_COLUMN}`;
const MIN_TABLE_WIDTH = '844px';
const TITLE_ID = 'cost-intel-namespace-apps-title';

// appLink opens an application's Cost Intelligence view in Argo CD.
const appLink = (app: AppCost) =>
  app.visible && app.name && app.namespace
    ? `/applications/${encodeURIComponent(app.namespace)}/${encodeURIComponent(app.name)}?view=Cost+Intelligence`
    : null;

interface Props {
  apps: AppCost[];
  totalCost: number;
}

// NamespaceAppsTable lists a namespace's applications, each expandable to the controllers it manages.
// Every pod appears under exactly one row.
export const NamespaceAppsTable: React.FC<Props> = ({ apps, totalCost }) => {
  const t = useTokens();
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());

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
  const money = (app: AppCost, value: number) => (app.visible ? formatMoney(value) : '—');

  return (
    <section aria-labelledby={TITLE_ID}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '16px' }}>
        <h4 id={TITLE_ID} style={{ margin: 0, color: t.textStrong, fontSize: '14px', fontWeight: 500, lineHeight: '20px' }}>
          Applications
        </h4>
        <span style={{ color: t.textMuted, fontSize: '12px' }}>
          Each pod counts once, under the app that manages it · totals include storage and network
        </span>
      </div>
      <div style={{ overflowX: 'auto' }}>
        <div role="table" aria-labelledby={TITLE_ID} style={{ minWidth: MIN_TABLE_WIDTH }}>
          <div role="rowgroup">
            <div
              role="row"
              style={{
                ...row,
                height: '18px',
                margin: '12px 0 8px',
                color: t.textMuted,
                fontSize: '12px',
                fontWeight: 500,
                textTransform: 'uppercase',
                whiteSpace: 'nowrap',
              }}
            >
              <span role="columnheader" aria-label="Controllers" />
              <span role="columnheader" style={cell}>
                App
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
              <span role="columnheader" style={numeric}>
                Measured
              </span>
            </div>
          </div>

          {apps.map((app, index) => {
            // An application is its namespace and name: with apps-in-any-namespace, team-a/web and
            // team-b/web can both deploy here.
            const key = `${app.namespace ?? ''}/${app.name ?? ''}`;
            const open = expanded.has(key);
            const groupId = `cost-intel-namespace-app-${index}`;
            const href = appLink(app);
            const label = app.name ?? 'Not managed by an Argo CD app';
            const detail = app.visible
              ? `${pluralize(app.controllers.length, 'controller')} · ${pluralize(app.podCount, 'pod')}`
              : 'Not listed by Argo CD';
            return (
              <div key={key} id={groupId} role="rowgroup" style={{ ...cardStyle(t), margin: '8px 0', overflow: 'hidden' }}>
                <div role="row" style={{ ...row, minHeight: '56px' }}>
                  <span role="cell" style={{ paddingLeft: '12px' }}>
                    {app.controllers.length > 0 && (
                      <button
                        type="button"
                        aria-expanded={open}
                        aria-controls={groupId}
                        aria-label={`Controllers of ${label}`}
                        onClick={() => toggle(key)}
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
                    )}
                  </span>
                  <span role="rowheader" style={{ ...cell, display: 'flex', flexDirection: 'column', gap: '2px' }}>
                    {href ? (
                      <a href={href} title={label} style={{ ...truncated, color: t.accentText, fontWeight: 500, textDecoration: 'none' }}>
                        {label}
                      </a>
                    ) : (
                      <span title={label} style={{ ...truncated, color: t.textStrong, fontWeight: 500 }}>
                        {label}
                      </span>
                    )}
                    <span style={{ color: t.textMuted, fontSize: '12px' }}>{detail}</span>
                  </span>
                  <span role="cell" style={numeric}>
                    {money(app, app.cpuCost)}
                  </span>
                  <span role="cell" style={numeric}>
                    {money(app, app.ramCost)}
                  </span>
                  <span role="cell" style={{ ...numeric, color: t.textStrong, fontWeight: 500 }}>
                    {money(app, app.totalCost)}
                  </span>
                  <span role="cell" style={cell}>
                    {app.visible && <Share fraction={app.share} t={t} />}
                  </span>
                  <span role="cell" style={numeric}>
                    {app.measuredAt && (
                      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', color: t.textMuted, fontSize: '12.8px' }}>
                        {isStale(app.measuredAt) && <WarningIcon color={t.warning} size={12} />}
                        {formatRelativeTime(app.measuredAt)}
                      </span>
                    )}
                  </span>
                </div>

                {open &&
                  app.controllers.map(controller => (
                    <div
                      key={controller.key}
                      role="row"
                      style={{ ...row, minHeight: '44px', backgroundColor: t.surfaceSunken, borderTop: `1px solid ${t.border}` }}
                    >
                      <span role="cell" />
                      <span role="rowheader" style={{ ...cell, ...truncated, paddingLeft: '31px' }}>
                        {controller.name}{' '}
                        <span style={{ color: t.textMuted, fontSize: '12px' }}>
                          {controller.kind} · {pluralize(controller.pods.length, 'pod')}
                        </span>
                      </span>
                      <span role="cell" style={numeric}>
                        {formatMoney(controller.cpuCost)}
                      </span>
                      <span role="cell" style={numeric}>
                        {formatMoney(controller.ramCost)}
                      </span>
                      <span role="cell" style={numeric}>
                        {formatMoney(controller.totalCost)}
                      </span>
                      <span role="cell" style={cell}>
                        <Share fraction={totalCost > 0 ? controller.totalCost / totalCost : 0} t={t} />
                      </span>
                      <span role="cell" />
                    </div>
                  ))}
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
};
