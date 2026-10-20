import React from 'react';

// Inline SVG icons. They are decorative: the text next to them carries the meaning.

export const RefreshIcon: React.FC<{ size?: number }> = ({ size = 16 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path d="M20 11a8 8 0 1 0-2.3 5.7" />
    <path d="M20 4v7h-7" />
  </svg>
);

// ChevronIcon points right when collapsed and down when expanded.
export const ChevronIcon: React.FC<{ expanded: boolean; size?: number }> = ({ expanded, size = 16 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={2}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
    style={{ transform: expanded ? 'rotate(90deg)' : undefined, transition: 'transform 0.15s ease' }}
  >
    <path d="M9 6l6 6-6 6" />
  </svg>
);

// Status icons take their colour explicitly so they keep their meaning next to neutral text.

export const WarningIcon: React.FC<{ color: string; size?: number }> = ({ color, size = 14 }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <path d="M12 3L2 21h20z" fill={color} />
    <path d="M12 10v5M12 18v.5" stroke="#100f0f" strokeWidth={2} strokeLinecap="round" />
  </svg>
);

export const ErrorIcon: React.FC<{ color: string; size?: number }> = ({ color, size = 16 }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <circle cx="12" cy="12" r="10" fill={color} />
    <path d="M12 7v6M12 16.5v.5" stroke="#ffffff" strokeWidth={2.2} strokeLinecap="round" />
  </svg>
);

export const CheckIcon: React.FC<{ color: string; size?: number }> = ({ color, size = 16 }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <circle cx="12" cy="12" r="10" fill={color} />
    <path d="M7.5 12.5l3 3 6-6.5" fill="none" stroke="#ffffff" strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

export const InfoIcon: React.FC<{ color: string; size?: number }> = ({ color, size = 14 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke={color}
    strokeWidth={2}
    strokeLinecap="round"
    aria-hidden="true"
    focusable="false"
  >
    <circle cx="12" cy="12" r="9" />
    <path d="M12 11v5M12 8v.5" />
  </svg>
);

// TrendIcon draws a line going down or up; the caller picks the colour for what the direction means.
export const TrendIcon: React.FC<{ direction: 'up' | 'down'; color: string; size?: number }> = ({
  direction,
  color,
  size = 18,
}) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke={color}
    strokeWidth={2}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
    style={{ transform: direction === 'up' ? 'scaleY(-1)' : undefined }}
  >
    <path d="M4 7l6 6 4-4 6 6" />
    <path d="M20 10v5h-5" />
  </svg>
);

export const GaugeIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path d="M4 17a8 8 0 1 1 16 0" />
    <path d="M12 17l4-5" />
  </svg>
);

export const LeafIcon: React.FC<{ size?: number }> = ({ size = 14 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path d="M5 19c0-8 5-13 14-14-1 9-6 14-14 14z" />
    <path d="M5 19l7-7" />
  </svg>
);

// Stroke is a 24px line icon in the current colour, the shape the card tiles below share.
const Stroke: React.FC<{ size: number; children: React.ReactNode }> = ({ size, children }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.8}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    {children}
  </svg>
);

export const PieIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <Stroke size={size}>
    <path d="M12 3v9h9a9 9 0 1 1-9-9z" />
    <path d="M15 3.5A9 9 0 0 1 20.5 9H15z" />
  </Stroke>
);

export const ChartIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <Stroke size={size}>
    <path d="M4 20h16" />
    <path d="M7 16v-5M12 16V6M17 16v-8" />
  </Stroke>
);

export const TagIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <Stroke size={size}>
    <path d="M3 3h8l10 10-8 8L3 11z" />
    <circle cx="7.5" cy="7.5" r="1.5" />
  </Stroke>
);

export const ChangeIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <Stroke size={size}>
    <path d="M4 8h13l-3-3M20 16H7l3 3" />
  </Stroke>
);

export const SlidersIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <Stroke size={size}>
    <path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0" />
    <circle cx="16" cy="6" r="2" />
    <circle cx="10" cy="12" r="2" />
    <circle cx="18" cy="18" r="2" />
  </Stroke>
);

export const AppsIcon: React.FC<{ size?: number }> = ({ size = 20 }) => (
  <Stroke size={size}>
    <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z" />
    <path d="M4 7.5l8 4.5 8-4.5M12 12v9" />
  </Stroke>
);
