import React from 'react';
import { cardStyle, useTokens } from '../theme/tokens';
import { ErrorIcon } from './icons';

// Notice is the neutral card for loading and empty states.
export const Notice: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const t = useTokens();
  return (
    <div style={{ ...cardStyle(t), padding: '20px 24px', fontSize: '13px', color: t.textMuted }}>
      {children}
    </div>
  );
};

export const ErrorNotice: React.FC<{ message: string }> = ({ message }) => {
  const t = useTokens();
  return (
    <div
      role="alert"
      style={{
        ...cardStyle(t),
        padding: '16px 24px',
        display: 'flex',
        alignItems: 'center',
        gap: '10px',
        fontSize: '13px',
        color: t.text,
      }}
    >
      <ErrorIcon color={t.danger} />
      <span>Couldn&apos;t load cost data: {message}</span>
    </div>
  );
};
