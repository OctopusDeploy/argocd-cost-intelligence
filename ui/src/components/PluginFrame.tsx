import React from 'react';
import { BrandMark } from './BrandMark';
import { PLUGIN_CLASS, pluginRootStyle, useGlobalStyles } from '../theme/globalStyles';
import { useTokens, visuallyHidden } from '../theme/tokens';

const OCTOPUS_URL = 'https://octopus.com';

interface Props {
  // status sits at the right of the header, such as a freshness note and a reload button.
  status?: React.ReactNode;
  // page is set for the system-level page, which has the whole content area to itself.
  page?: boolean;
  children: React.ReactNode;
}

// PluginFrame is the root of each of the plugin's views: a bordered panel with the Cost Intelligence
// header, inside which the tab bar and the active view sit. It applies the plugin class, font and
// global stylesheet, so a view only has to render inside it.
export const PluginFrame: React.FC<Props> = ({ status, page = false, children }) => {
  const t = useTokens();
  useGlobalStyles();

  return (
    <div
      className={PLUGIN_CLASS}
      style={{
        ...pluginRootStyle(t),
        margin: page ? '24px' : '0 0 20px',
        color: t.text,
        backgroundColor: t.panel,
        border: `1px solid ${t.border}`,
        borderRadius: '4px',
      }}
    >
      {/* When the brand and the status don't fit on one line, the status wraps below. */}
      <header
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '12px 20px',
          padding: '14px 20px',
          backgroundColor: t.chrome,
          borderBottom: `1px solid ${t.border}`,
          borderRadius: '4px 4px 0 0',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <BrandMark size={36} />
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <h2
              style={{
                margin: 0,
                color: t.textStrong,
                fontSize: '18px',
                fontWeight: 700,
                lineHeight: '22px',
                letterSpacing: '-0.01em',
              }}
            >
              Cost Intelligence
            </h2>
            <a
              href={OCTOPUS_URL}
              target="_blank"
              rel="noopener noreferrer"
              style={{ color: t.textMuted, fontSize: '12px', lineHeight: '16px', textDecoration: 'none' }}
            >
              by Octopus Deploy<span style={visuallyHidden}> (opens in a new tab)</span>
            </a>
          </div>
        </div>
        {status && (
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '12px', marginLeft: 'auto' }}>
            {status}
          </div>
        )}
      </header>
      {children}
    </div>
  );
};

// framePanelStyle pads the active tab's panel inside the frame.
export const framePanelStyle: React.CSSProperties = { padding: '24px 20px' };
