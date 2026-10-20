import React, { useState } from 'react';
import { RefreshIcon } from './icons';
import { useTokens } from '../theme/tokens';

interface Props {
  onReload: () => Promise<unknown>;
  label?: string;
}

// ReloadButton re-fetches on demand. Only a reload the user asked for disables it, so background
// polling never makes it flicker.
export const ReloadButton: React.FC<Props> = ({ onReload, label = 'Reload cost data' }) => {
  const t = useTokens();
  const [reloading, setReloading] = useState(false);

  const reload = async () => {
    setReloading(true);
    try {
      await onReload();
    } finally {
      setReloading(false);
    }
  };

  return (
    <button
      type="button"
      onClick={reload}
      disabled={reloading}
      aria-busy={reloading}
      aria-label={label}
      title={label}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
        height: '32px',
        padding: '0 12px',
        border: `1px solid ${t.border}`,
        borderRadius: '4px',
        backgroundColor: t.surfaceSunken,
        color: t.text,
        fontFamily: 'inherit',
        fontSize: '13px',
        fontWeight: 500,
        opacity: reloading ? 0.5 : 1,
        cursor: reloading ? 'progress' : 'pointer',
      }}
    >
      <RefreshIcon />
      Reload
    </button>
  );
};
