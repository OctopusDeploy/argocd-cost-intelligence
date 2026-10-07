import React, { useRef } from 'react';
import { useTokens } from '../theme/tokens';

export interface Tab<T extends string> {
  id: T;
  label: string;
}

interface TabBarProps<T extends string> {
  tabs: Array<Tab<T>>;
  active: T;
  onChange: (id: T) => void;
  // idPrefix keeps tab and panel ids unique on the page; see tabId and panelId.
  idPrefix: string;
  label: string;
  // children render at the end of the bar, such as a freshness note and a reload button.
  children?: React.ReactNode;
}

export const tabId = (idPrefix: string, id: string) => `${idPrefix}-tab-${id}`;
export const panelId = (idPrefix: string, id: string) => `${idPrefix}-panel-${id}`;

// TabBar is the plugin's Argo CD-styled tab strip. It follows the WAI-ARIA tabs pattern: arrow keys,
// Home and End move between tabs, and focus follows the selection.
export function TabBar<T extends string>({ tabs, active, onChange, idPrefix, label, children }: TabBarProps<T>) {
  const t = useTokens();
  const refs = useRef<Array<HTMLButtonElement | null>>([]);

  const onKeyDown = (event: React.KeyboardEvent, index: number) => {
    const last = tabs.length - 1;
    let next: number;
    switch (event.key) {
      case 'ArrowRight':
        next = index === last ? 0 : index + 1;
        break;
      case 'ArrowLeft':
        next = index === 0 ? last : index - 1;
        break;
      case 'Home':
        next = 0;
        break;
      case 'End':
        next = last;
        break;
      default:
        return;
    }
    event.preventDefault();
    onChange(tabs[next].id);
    refs.current[next]?.focus();
  };

  return (
    <div
      style={{
        display: 'flex',
        // When the tabs and the status beside them don't fit on one line, wrap-reverse moves the
        // status onto a line above, so the tabs stay on the bottom border they underline.
        flexWrap: 'wrap-reverse',
        alignItems: 'flex-end',
        justifyContent: 'space-between',
        columnGap: '16px',
        marginBottom: '20px',
        borderBottom: `1px solid ${t.border}`,
      }}
    >
      <div role="tablist" aria-label={label} style={{ display: 'flex', gap: '24px' }}>
        {tabs.map((tab, index) => {
          const selected = tab.id === active;
          return (
            <button
              key={tab.id}
              ref={element => {
                refs.current[index] = element;
              }}
              id={tabId(idPrefix, tab.id)}
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls={panelId(idPrefix, tab.id)}
              tabIndex={selected ? 0 : -1}
              onClick={() => onChange(tab.id)}
              onKeyDown={event => onKeyDown(event, index)}
              style={{
                height: '44px',
                padding: '0 2px',
                marginBottom: '-1px',
                border: 'none',
                borderBottom: `2px solid ${selected ? t.accent : 'transparent'}`,
                backgroundColor: 'transparent',
                color: selected ? t.textStrong : t.textMuted,
                fontFamily: 'inherit',
                fontSize: '14px',
                fontWeight: 500,
                cursor: 'pointer',
              }}
            >
              {tab.label}
            </button>
          );
        })}
      </div>
      {children && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', minHeight: '44px', marginLeft: 'auto' }}>
          {children}
        </div>
      )}
    </div>
  );
}
