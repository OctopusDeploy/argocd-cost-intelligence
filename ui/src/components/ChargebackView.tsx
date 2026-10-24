import React, { useCallback, useEffect, useRef, useState } from 'react';
import { CardHeader } from './CardHeader';
import { Chip } from './Chip';
import { Share } from './ControllerCostTable';
import { LEVEL_LABELS, levelIcon } from './EfficiencyCard';
import { InfoIcon, TagIcon } from './icons';
import { ErrorNotice, Notice } from './Notice';
import { StatStrip } from './StatStrip';
import { useChargeback } from '../hooks/useChargeback';
import { useLabelKeys } from '../hooks/useLabelKeys';
import { useUrlState } from '../hooks/useUrlState';
import { cardStyle, Tokens, useTokens, visuallyHidden } from '../theme/tokens';
import { ChargebackEntry, NamespaceSummary } from '../utils/api';
import { efficiencyLevel } from '../utils/costInsights';
import { formatMoney, formatPercent, pluralize, roundParts, sumRounded } from '../utils/format';
import { FALLBACK_LABEL_KEYS, sameLabelKey, suggestLabelKeys } from '../utils/labelKeys';
import { NAME_COLUMN, SPACER_COLUMN } from '../utils/layout';

const DEFAULT_LABEL = 'team';

// Typing a label key queries as you go; this is how long the field waits for you to stop.
const DEBOUNCE_MS = 400;

// Label value, CPU, memory, storage, network, GPU when any value has GPU cost, total, share,
// efficiency, then a spacer. The value grows to 480px and the spacer takes any width beyond that
// (see NAME_COLUMN). The minimum width is the value's minimum plus the fixed columns.
const columns = (gpu: boolean) =>
  `${NAME_COLUMN} 96px 96px 96px 96px ${gpu ? '96px ' : ''}112px 124px 152px ${SPACER_COLUMN}`;
const minTableWidth = (gpu: boolean) => (gpu ? '1028px' : '932px');

const LABEL_INPUT_ID = 'cost-intel-chargeback-label';
const LABEL_LIST_ID = 'cost-intel-chargeback-label-keys';
const NAMESPACE_SELECT_ID = 'cost-intel-chargeback-namespace';
const TITLE_ID = 'cost-intel-chargeback-title';

interface Props {
  namespaces: NamespaceSummary[];
}

// ChargebackView groups cluster cost by the values of one Kubernetes label. Editing any control
// runs the query, so there is nothing to submit. The query lives in the URL, so a grouping can be
// linked to; refining it replaces the entry rather than stacking up Back steps.
export const ChargebackView: React.FC<Props> = ({ namespaces }) => {
  const t = useTokens();
  const [query, setQuery] = useUrlState<string>('costLabel', DEFAULT_LABEL);
  const [namespace, setNamespace] = useUrlState<string>('costScope', '');
  const [labelKey, setLabelKey] = useState(query);
  const { entries, totalCost, loading, error } = useChargeback(query || null, namespace || null);

  // The chips offer label keys pods in scope actually carry. Until the first answer there are none,
  // and if the backend cannot list keys the view falls back to commonly used ones.
  const labelKeys = useLabelKeys(namespace || null);
  const suggestions = labelKeys.keys
    ? suggestLabelKeys(labelKeys.keys)
    : labelKeys.failed
      ? FALLBACK_LABEL_KEYS
      : [];

  // Everything below sets the query through applyQuery, so a query that arrives any other way came
  // from Back or Forward, and the field has to catch up with it.
  const applied = useRef(query);
  const applyQuery = useCallback(
    (next: string) => {
      applied.current = next;
      setQuery(next, 'replace');
    },
    [setQuery]
  );
  useEffect(() => {
    if (query === applied.current) return;
    applied.current = query;
    setLabelKey(query);
  }, [query]);

  // Typing settles before it queries; picking a suggestion or a namespace queries at once.
  useEffect(() => {
    const trimmed = labelKey.trim();
    if (trimmed === query) return;
    const timer = setTimeout(() => applyQuery(trimmed), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [labelKey, query, applyQuery]);

  const chooseLabel = (key: string) => {
    setLabelKey(key);
    applyQuery(key);
  };

  return (
    <div>
      <div
        style={{
          ...cardStyle(t),
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'flex-end',
          gap: '16px 24px',
          marginBottom: '16px',
          padding: '16px 20px',
        }}
      >
        <Field label="Label key" htmlFor={LABEL_INPUT_ID} t={t}>
          <input
            id={LABEL_INPUT_ID}
            value={labelKey}
            list={LABEL_LIST_ID}
            placeholder="e.g. team"
            onChange={event => setLabelKey(event.target.value)}
            onKeyDown={event => {
              if (event.key === 'Enter') applyQuery(labelKey.trim());
            }}
            style={{
              width: '180px',
              padding: '7px 10px',
              border: `1px solid ${t.border}`,
              borderRadius: '4px',
              backgroundColor: t.surfaceSunken,
              color: t.text,
              fontFamily: 'inherit',
              fontSize: '13px',
            }}
          />
          <datalist id={LABEL_LIST_ID}>
            {(labelKeys.keys ?? []).map(key => (
              <option key={key} value={key} />
            ))}
          </datalist>
        </Field>

        {suggestions.length > 0 && (
          <div role="group" aria-label="Suggested label keys" style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
            {suggestions.map(key => {
              return (
                <Chip key={key} pressed={sameLabelKey(labelKey, key)} onClick={() => chooseLabel(key)}>
                  {key}
                </Chip>
              );
            })}
          </div>
        )}

        <Field label="Namespace" htmlFor={NAMESPACE_SELECT_ID} t={t}>
          <select
            id={NAMESPACE_SELECT_ID}
            value={namespace}
            onChange={event => setNamespace(event.target.value, 'replace')}
            style={{
              padding: '7px 10px',
              border: `1px solid ${t.border}`,
              borderRadius: '4px',
              backgroundColor: t.surfaceSunken,
              color: t.text,
              fontFamily: 'inherit',
              fontSize: '13px',
              cursor: 'pointer',
            }}
          >
            <option value="">All namespaces</option>
            {namespaces.map(ns => (
              <option key={ns.namespace} value={ns.namespace}>
                {ns.namespace}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <Results
        entries={entries}
        totalCost={totalCost}
        loading={loading}
        error={error}
        labelKey={query}
        namespace={namespace}
      />
    </div>
  );
};

const Field: React.FC<{ label: string; htmlFor: string; t: Tokens; children: React.ReactNode }> = ({
  label,
  htmlFor,
  t,
  children,
}) => (
  <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
    <label
      htmlFor={htmlFor}
      style={{ color: t.textMuted, fontSize: '12px', fontWeight: 500 }}
    >
      {label}
    </label>
    {children}
  </div>
);

interface ResultsProps {
  entries: ChargebackEntry[];
  totalCost: number;
  loading: boolean;
  error: string | null;
  labelKey: string;
  namespace: string;
}

const Results: React.FC<ResultsProps> = ({ entries, totalCost, loading, error, labelKey, namespace }) => {
  const t = useTokens();
  const scope = namespace ? ` in ${namespace}` : ' across the cluster';

  if (!labelKey) return <Notice>Enter a label key to group costs by its values.</Notice>;
  if (error) return <ErrorNotice message={error} />;
  if (loading && entries.length === 0) return <Notice>Grouping costs by {labelKey}…</Notice>;
  if (entries.length === 0) {
    return (
      <Notice>
        No pods carry the label <strong style={{ color: t.text }}>{labelKey}</strong>
        {scope}. Only labelled pods are counted.
      </Notice>
    );
  }

  const largest = entries[0];
  // The attributed cost is the sum of the rows as shown, so the rows add up to it.
  const attributed = sumRounded(entries.map(entry => entry.totalCost));

  return (
    <div>
      <div role="status" style={visuallyHidden}>
        {`${pluralize(entries.length, 'value')} of ${labelKey}, ${formatMoney(attributed)} attributed.`}
      </div>

      <StatStrip
        stats={[
          {
            label: 'Attributed cost',
            value: formatMoney(attributed),
            detail: `Grouped by ${labelKey}${scope}`,
          },
          {
            label: 'Label values',
            value: String(entries.length),
            detail: 'Pods without the label are excluded',
          },
          {
            label: 'Largest',
            value: <Truncated text={largest.labelValue} />,
            detail: `${formatMoney(largest.totalCost)} · ${formatPercent(totalCost > 0 ? largest.totalCost / totalCost : 0)}`,
          },
        ]}
      />

      <ChargebackTable entries={entries} totalCost={totalCost} labelKey={labelKey} loading={loading} />
    </div>
  );
};

const Truncated: React.FC<{ text: string }> = ({ text }) => (
  <span title={text} style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
    {text}
  </span>
);

interface TableProps {
  entries: ChargebackEntry[];
  totalCost: number;
  labelKey: string;
  loading: boolean;
}

const ChargebackTable: React.FC<TableProps> = ({ entries, totalCost, labelKey, loading }) => {
  const t = useTokens();
  // GPU gets a column only when a value has GPU cost, as on the cost breakdown, so the columns
  // beside each total still add up to it.
  const gpu = entries.some(entry => (entry.gpuCost ?? 0) > 0);

  const row: React.CSSProperties = {
    display: 'grid',
    gridTemplateColumns: columns(gpu),
    alignItems: 'center',
    color: t.text,
    fontSize: '13px',
    fontVariantNumeric: 'tabular-nums',
  };
  const cell: React.CSSProperties = { minWidth: 0, padding: '0 15px' };
  const numeric: React.CSSProperties = { ...cell, textAlign: 'right' };

  return (
    <section aria-labelledby={TITLE_ID}>
      <CardHeader
        id={TITLE_ID}
        icon={<TagIcon />}
        title={`Costs by ${labelKey}`}
        detail={loading ? 'Updating…' : 'Monthly · network includes load balancers'}
      />

      <div style={{ overflowX: 'auto' }}>
        <div role="table" aria-labelledby={TITLE_ID} style={{ minWidth: minTableWidth(gpu) }}>
          <div role="rowgroup">
            <div
              role="row"
              style={{
                ...row,
                height: '18px',
                margin: '12px 0 8px',
                color: t.textStrong,
                fontSize: '12.8px',
                fontWeight: 600,
                whiteSpace: 'nowrap',
              }}
            >
              <span role="columnheader" style={cell}>
                Value
              </span>
              <span role="columnheader" style={numeric}>
                CPU
              </span>
              <span role="columnheader" style={numeric}>
                Memory
              </span>
              <span role="columnheader" style={numeric}>
                Storage
              </span>
              <span role="columnheader" style={numeric}>
                Network
              </span>
              {gpu && (
                <span role="columnheader" style={numeric}>
                  GPU
                </span>
              )}
              <span role="columnheader" style={numeric}>
                Total
              </span>
              <span role="columnheader" style={numeric}>
                Share
              </span>
              <span role="columnheader" style={numeric}>
                Efficiency
              </span>
            </div>
          </div>

          <div role="rowgroup">
            {entries.map(entry => {
              // Rounded so the cells beside the total add up to it.
              const [cpuCost, ramCost, storageCost, networkCost, gpuCost] = roundParts(
                [entry.cpuCost, entry.ramCost, entry.storageCost, entry.networkCost + entry.lbCost, entry.gpuCost ?? 0],
                entry.totalCost,
              );
              return (
                <div
                  key={entry.labelValue}
                  role="row"
                  style={{ ...row, ...cardStyle(t), margin: '8px 0', minHeight: '52px' }}
                >
                  <span role="rowheader" style={{ ...cell, color: t.textStrong, fontWeight: 500 }}>
                    <Truncated text={entry.labelValue} />
                  </span>
                  <span role="cell" style={numeric}>
                    {formatMoney(cpuCost)}
                  </span>
                  <span role="cell" style={numeric}>
                    {formatMoney(ramCost)}
                  </span>
                  <span role="cell" style={numeric}>
                    {formatMoney(storageCost)}
                  </span>
                  <span role="cell" style={numeric}>
                    {formatMoney(networkCost)}
                  </span>
                  {gpu && (
                    <span role="cell" style={numeric}>
                      {formatMoney(gpuCost)}
                    </span>
                  )}
                  <span role="cell" style={{ ...numeric, color: t.textStrong, fontWeight: 500 }}>
                    {formatMoney(entry.totalCost)}
                  </span>
                  <span role="cell" style={cell}>
                    <Share fraction={totalCost > 0 ? entry.totalCost / totalCost : 0} t={t} />
                  </span>
                  <span role="cell" style={numeric}>
                    <Efficiency ratio={entry.efficiency} noRequests={entry.noRequests} t={t} />
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </section>
  );
};

// Efficiency reads the same way here as it does on the namespace and application views.
const Efficiency: React.FC<{ ratio: number; noRequests?: boolean; t: Tokens }> = ({ ratio, noRequests, t }) => {
  if (noRequests) {
    return (
      <span
        aria-label="No CPU or memory requests set"
        style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '8px', color: t.textMuted }}
      >
        <span aria-hidden="true" style={{ display: 'flex' }}>
          <InfoIcon color={t.textMuted} />
        </span>
        <span aria-hidden="true">No request set</span>
      </span>
    );
  }
  if (!ratio || !Number.isFinite(ratio)) {
    return (
      <span aria-label="Efficiency not measured" style={{ color: t.textMuted }}>
        —
      </span>
    );
  }
  const level = efficiencyLevel(ratio);
  return (
    <span
      aria-label={`${formatPercent(ratio)}, ${LEVEL_LABELS[level]}`}
      style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '8px' }}
    >
      <span aria-hidden="true" style={{ display: 'flex' }}>
        {levelIcon(level, t)}
      </span>
      <span aria-hidden="true">{formatPercent(ratio)}</span>
    </span>
  );
};
