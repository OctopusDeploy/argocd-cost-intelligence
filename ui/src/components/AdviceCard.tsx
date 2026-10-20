import React from 'react';
import { IconTile } from './CardHeader';
import { GaugeIcon } from './icons';
import { cardStyle, useTokens } from '../theme/tokens';

export interface AdviceCardProps {
  title: string;
  // children is the advice itself; notes are the muted lines under it.
  children: React.ReactNode;
  notes?: React.ReactNode;
  onShowResources?: () => void;
  // banner spans a page and puts the button beside the text, wrapping it underneath when there is
  // no room; column stacks everything for a card column. flush drops the space below the card.
  layout?: 'banner' | 'column';
  flush?: boolean;
}

// AdviceCard is the frame of a recommendation: an icon, a title, the advice, and a button to the
// resources it is about. RecommendationCard and RightsizingCard fill it in.
export const AdviceCard: React.FC<AdviceCardProps> = ({
  title,
  children,
  notes,
  onShowResources,
  layout = 'banner',
  flush = false,
}) => {
  const t = useTokens();

  const icon = (
    <IconTile color={t.tileAdvice}>
      <GaugeIcon />
    </IconTile>
  );

  // In a banner the text takes the free width but wraps the button onto its own line below 320px.
  const text = (
    <div
      style={{
        display: 'flex',
        flex: layout === 'banner' ? '1 1 320px' : 1,
        flexDirection: 'column',
        gap: '4px',
        minWidth: 0,
      }}
    >
      <h4 style={{ margin: 0, color: t.textStrong, fontSize: '14px', fontWeight: 600, lineHeight: '20px' }}>{title}</h4>
      <div style={{ color: t.text, fontSize: '13px', lineHeight: '20px' }}>{children}</div>
      {notes}
    </div>
  );

  const button = onShowResources && (
    <button
      type="button"
      onClick={onShowResources}
      style={{
        flexShrink: 0,
        height: '32px',
        padding: '0 14px',
        border: `1px solid ${t.accent}`,
        borderRadius: '4px',
        backgroundColor: 'transparent',
        color: t.accentText,
        fontFamily: 'inherit',
        fontSize: '13px',
        fontWeight: 500,
        cursor: 'pointer',
      }}
    >
      View resources
    </button>
  );

  const card: React.CSSProperties = { ...cardStyle(t), marginBottom: flush ? 0 : '16px', padding: '20px 24px' };

  if (layout === 'column') {
    return (
      <section aria-label="Recommendation" style={{ ...card, display: 'flex', flexDirection: 'column', gap: '16px' }}>
        <div style={{ display: 'flex', gap: '16px' }}>
          {icon}
          {text}
        </div>
        {/* Indented to line up with the text, past the icon and its gap. */}
        {button && <div style={{ paddingLeft: '52px' }}>{button}</div>}
      </section>
    );
  }

  return (
    <section
      aria-label="Recommendation"
      style={{ ...card, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '16px' }}
    >
      {icon}
      {text}
      {button && <div style={{ marginLeft: 'auto' }}>{button}</div>}
    </section>
  );
};

// AdviceNote is a muted line under the advice, led by an icon.
export const AdviceNote: React.FC<{ icon: React.ReactNode; children: React.ReactNode }> = ({ icon, children }) => {
  const t = useTokens();
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: '6px', color: t.textMuted, fontSize: '12px' }}>
      {icon}
      {children}
    </span>
  );
};
