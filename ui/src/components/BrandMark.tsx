import React from 'react';

// BrandMark is the Octopus Deploy mark in the plugin's header. This is a stand-in drawing: replace
// the SVG below with the official asset, keeping the size prop, and nothing else needs to change.
export const BrandMark: React.FC<{ size?: number }> = ({ size = 36 }) => (
  <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" focusable="false" style={{ flexShrink: 0 }}>
    <rect width="64" height="64" rx="14" fill="#2f93e0" />
    <path
      d="M32 11c-9.4 0-16 7-16 16.3 0 4.7 1.7 8.3 4 11-1.6 3.2-4.3 5.6-8 6.6 3.6 1.6 7.7.6 10.7-2-.6 3.6-2.3 6.7-5.2 9 4.3.2 8-2.4 9.8-6.3.7 3.6 2.6 6.4 4.7 8.1 2.1-1.7 4-4.5 4.7-8.1 1.8 3.9 5.5 6.5 9.8 6.3-2.9-2.3-4.6-5.4-5.2-9 3 2.6 7.1 3.6 10.7 2-3.7-1-6.4-3.4-8-6.6 2.3-2.7 4-6.3 4-11C48 18 41.4 11 32 11z"
      fill="#ffffff"
    />
  </svg>
);
