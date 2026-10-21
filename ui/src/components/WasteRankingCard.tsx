import React from 'react';
import { CardHeader } from './CardHeader';
import { Share } from './ControllerCostTable';
import { GaugeIcon, WarningIcon } from './icons';
import { cardStyle, Tokens, useTokens } from '../theme/tokens';
import { AppWaste } from '../utils/api';
import { formatMoney, formatRelativeTime, isStale, pluralize } from '../utils/format';
import { NAME_COLUMN, SPACER_COLUMN } from '../utils/layout';
import { WasteTotal, wasteTable } from '../utils/waste';

// Rank, application, idle, share of its cost, CPU saving, measured, then a spacer (see NAME_COLUMN).
const COLUMNS = `48px ${NAME_COLUMN} 112px 132px 148px 120px ${SPACER_COLUMN}`;
const MIN_TABLE_WIDTH = '720px';
const TITLE_ID = 'cost-intel-waste-title';

const appLink = (app: AppWaste) =>
  `/applications/${encodeURIComponent(app.applicationNamespace)}/${encodeURIComponent(app.applicationName)}?view=Cost+Intelligence`;

// The saving is rounded to whole dollars, as the application's tab rounds it.
export const saving = (amount: number) => `~${formatMoney(amount, 'USD', 0)}`;

interface Props {
  // apps are the ranked applications to show, most idle first.
  apps: AppWaste[];
  // namespace is the deployment namespace the apps are filtered to, or '' for all of them.
  namespace: string;
  // noPods is how many applications were left out for having no pods.
  noPods: number;
  // action sits at the card's top right, such as the namespace filter.
  action?: React.ReactNode;
  onSelectNamespace: (namespace: string) => void;
}

// WasteRankingCard ranks applications by the idle cost of their own pods, with the saving of the
// lower CPU request each one's tab suggests.
export const WasteRankingCard: React.FC<Props> = ({ apps, namespace, noPods, action, onSelectNamespace }) => {
  const t = useTokens();
  const table = wasteTable(apps);

  let body: React.ReactNode;
  if (apps.length === 0) {
    body = (
      <p style={messageStyle(t)}>
        {namespace
          ? `No ranked app deploys to ${namespace}.`
          : noPods > 0
            ? 'No app measured on its own has pods running.'
            : "No app has been measured on its own yet. Apps are ranked after their next sync, once the notifications template sends each app's resources."}
      </p>
    );
  } else if (table.total.idleCost <= 0) {
    body = (
      <p style={messageStyle(t)}>
        No idle spend in the {pluralize(apps.length, 'app')} measured on their own: their pods use what they request.
      </p>
    );
  } else {
    body = <WasteTableView table={table} onSelectNamespace={onSelectNamespace} t={t} />;
  }

  return (
    <section aria-labelledby={TITLE_ID} style={{ ...cardStyle(t), padding: '20px 24px' }}>
      <CardHeader
        id={TITLE_ID}
        icon={<GaugeIcon />}
        title="Apps by idle spend"
        detail="Requested CPU and memory that each app's own pods didn't use, per month"
        action={action}
      />
      {body}
    </section>
  );
};

interface TableProps {
  table: ReturnType<typeof wasteTable>;
  onSelectNamespace: (namespace: string) => void;
  t: Tokens;
}

const WasteTableView: React.FC<TableProps> = ({ table, onSelectNamespace, t }) => {
  const row: React.CSSProperties = {
    display: 'grid',
    gridTemplateColumns: COLUMNS,
    alignItems: 'center',
    minHeight: '52px',
    borderTop: `1px solid ${t.border}`,
    color: t.text,
    fontSize: '13px',
    fontVariantNumeric: 'tabular-nums',
  };
  const cell: React.CSSProperties = { minWidth: 0, padding: '0 15px' };
  const numeric: React.CSSProperties = { ...cell, textAlign: 'right' };
  const muted: React.CSSProperties = { color: t.textMuted };
  const truncated: React.CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };

  const totalRow = (total: WasteTotal) => (
    <div role="row" style={{ ...row, minHeight: '44px', color: t.textStrong, fontWeight: 600 }}>
      <span role="cell" />
      <span role="rowheader" style={cell}>
        Total, {pluralize(total.count, 'app')}
      </span>
      <span role="cell" style={numeric}>
        {formatMoney(total.idleCost)}
      </span>
      <span role="cell" />
      <span role="cell" style={numeric}>
        {total.saving > 0 ? saving(total.saving) : '—'}
      </span>
      <span role="cell" />
    </div>
  );

  return (
    <div style={{ overflowX: 'auto', marginTop: '12px' }}>
      <div role="table" aria-labelledby={TITLE_ID} style={{ minWidth: MIN_TABLE_WIDTH }}>
        <div role="rowgroup">
          <div
            role="row"
            style={{
              ...row,
              minHeight: '32px',
              borderTop: 'none',
              color: t.textStrong,
              fontSize: '12.8px',
              fontWeight: 600,
              whiteSpace: 'nowrap',
            }}
          >
            <span role="columnheader" aria-label="Rank" style={numeric}>
              #
            </span>
            <span role="columnheader" style={cell}>
              App
            </span>
            <span role="columnheader" style={numeric}>
              Idle /mo
            </span>
            <span role="columnheader" style={numeric}>
              Of its cost
            </span>
            <span role="columnheader" style={numeric}>
              Lower CPU request
            </span>
            <span role="columnheader" style={numeric}>
              Measured
            </span>
          </div>
        </div>

        <div role="rowgroup">
          {table.rows.map((app, index) => (
            // An application is its namespace and name: team-a/web and team-b/web are two rows.
            <div key={`${app.applicationNamespace}/${app.applicationName}`} role="row" style={row}>
              <span role="cell" style={{ ...numeric, ...muted }}>
                {index + 1}
              </span>
              <span role="rowheader" style={{ ...cell, display: 'flex', flexDirection: 'column', gap: '2px' }}>
                <a
                  href={appLink(app)}
                  title={app.applicationName}
                  style={{ ...truncated, color: t.accentText, fontWeight: 500, textDecoration: 'none' }}
                >
                  {app.applicationName}
                </a>
                <span style={{ ...truncated, ...muted, fontSize: '12px' }}>
                  {app.applicationNamespace} · deploys to{' '}
                  <button
                    type="button"
                    aria-label={`Show namespace ${app.deploymentNamespace}`}
                    title={`Show namespace ${app.deploymentNamespace}`}
                    onClick={() => onSelectNamespace(app.deploymentNamespace)}
                    style={namespaceChip(t)}
                  >
                    {app.deploymentNamespace}
                  </button>
                </span>
              </span>
              <span role="cell" style={{ ...numeric, color: t.textStrong, fontWeight: 500 }}>
                {formatMoney(app.idleCost)}
              </span>
              <span role="cell" style={cell}>
                <Share fraction={app.share} t={t} />
              </span>
              <span role="cell" style={numeric}>
                {app.saving !== null ? `save ${saving(app.saving)}` : <span style={muted}>—</span>}
              </span>
              <span role="cell" style={numeric}>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', ...muted, fontSize: '12.8px' }}>
                  {isStale(app.measuredAt) && <WarningIcon color={t.warning} size={12} />}
                  {formatRelativeTime(app.measuredAt)}
                </span>
              </span>
            </div>
          ))}
          {totalRow(table.total)}
        </div>
      </div>
    </div>
  );
};

const namespaceChip = (t: Tokens): React.CSSProperties => ({
  padding: '1px 8px',
  border: `1px solid ${t.border}`,
  borderRadius: '999px',
  backgroundColor: 'transparent',
  color: t.text,
  fontFamily: 'inherit',
  fontSize: '12px',
  cursor: 'pointer',
});

const messageStyle = (t: Tokens): React.CSSProperties => ({ margin: '16px 0 0', fontSize: '13px', color: t.textMuted });

