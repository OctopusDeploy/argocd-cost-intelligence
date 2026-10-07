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
