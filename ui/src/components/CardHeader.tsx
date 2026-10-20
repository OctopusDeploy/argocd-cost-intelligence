import React from 'react';
import { useTokens } from '../theme/tokens';

interface Props {
  // id lets the card's section and table point at the title with aria-labelledby.
  id?: string;
  title: React.ReactNode;
  // detail is the muted line under the title, such as the period or the source of the figures.
  detail?: React.ReactNode;
  icon?: React.ReactNode;
  // action sits at the top right, such as a "View as table" button.
  action?: React.ReactNode;
  // as is the heading level; cards are h4, a section that holds cards is h3.
  as?: 'h3' | 'h4';
}

// CardHeader is the top of a card in Octopus Deploy's style: an icon tile above the title, and a
// muted line under it.
export const CardHeader: React.FC<Props> = ({ id, title, detail, icon, action, as: Heading = 'h4' }) => {
  const t = useTokens();
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '16px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', minWidth: 0 }}>
        {icon && <IconTile>{icon}</IconTile>}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
          <Heading
            id={id}
            style={{ margin: 0, color: t.textStrong, fontSize: '16px', fontWeight: 600, lineHeight: '22px' }}
          >
            {title}
          </Heading>
          {detail && <span style={{ color: t.textMuted, fontSize: '12px', lineHeight: '18px' }}>{detail}</span>}
        </div>
      </div>
      {action && <div style={{ flexShrink: 0 }}>{action}</div>}
    </div>
  );
};

// IconTile is the small white square an icon sits on, at the top of a card or beside advice.
export const IconTile: React.FC<{ children: React.ReactNode; color?: string }> = ({ children, color }) => {
  const t = useTokens();
  return (
    <span
      aria-hidden="true"
      style={{
        display: 'flex',
        flexShrink: 0,
        alignItems: 'center',
        justifyContent: 'center',
        width: '36px',
        height: '36px',
        border: `1px solid ${t.border}`,
        borderRadius: '4px',
        backgroundColor: t.tile,
        color: color ?? t.tileIcon,
      }}
    >
      {children}
    </span>
  );
};
