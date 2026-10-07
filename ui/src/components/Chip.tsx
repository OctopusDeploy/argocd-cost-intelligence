import React from 'react';
import { useTokens } from '../theme/tokens';

type Props = Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'type' | 'style'> & {
  pressed: boolean;
};

// Chip is a toggle in a row of discrete picks, such as a label key or a date range. It applies its
// choice at once, so there is no submit button, and reports its state through aria-pressed.
export const Chip: React.FC<Props> = ({ pressed, children, ...rest }) => {
  const t = useTokens();
  return (
    <button
      {...rest}
      type="button"
      aria-pressed={pressed}
      style={{
        padding: '4px 11px',
        border: `1px solid ${pressed ? t.accent : t.border}`,
        borderRadius: '12px',
        backgroundColor: pressed ? t.accentWash : 'transparent',
        color: pressed ? t.textStrong : t.textMuted,
        fontFamily: 'inherit',
        fontSize: '12px',
        cursor: 'pointer',
      }}
    >
      {children}
    </button>
  );
};
