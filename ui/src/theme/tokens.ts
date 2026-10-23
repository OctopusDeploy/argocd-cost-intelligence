import type { CSSProperties } from 'react';
import { Theme, useTheme } from '../hooks/useTheme';

// Colour tokens for the plugin's Octopus Deploy look, in light and dark to follow Argo CD's theme
// setting. The plugin sits inside Argo CD's page as a branded panel, so its colours are Octopus's
// own rather than Argo CD's.
export interface Tokens {
  // panel is the frame's background, chrome its header strip; surface is a card inside the panel.
  panel: string;
  chrome: string;
  surface: string;
  surfaceSunken: string;
  textStrong: string;
  text: string;
  textMuted: string;
  border: string;
  hairline: string;
  // shadow lifts cards off the panel in the light theme; dark cards rely on their border alone.
  shadow: string;
  // accent marks non-text elements (active tab, chart line); accentText is for text.
  accent: string;
  accentText: string;
  accentWash: string;
  // hover tints a control's own background on pointer hover, whatever that background is.
  hover: string;
  // brand is the octoicon's blue, the same on both themes as in Octopus's logo pack.
  brand: string;
  // tile is the background of a card's icon tile, white in both themes as in Octopus Deploy;
  // tileIcon and tileAdvice are the icons drawn on it.
  tile: string;
  tileIcon: string;
  tileAdvice: string;
  success: string;
  warning: string;
  danger: string;
  // series is the categorical chart palette in fixed slot order (blue, orange, aqua, yellow,
  // magenta, green, violet), validated for colour-vision deficiency against `surface`.
  series: string[];
}

const dark: Tokens = {
  panel: '#1a2430',
  chrome: '#111a23',
  surface: '#222e3b',
  surfaceSunken: '#16202a',
  textStrong: '#f4f6f8',
  text: '#c5ced7',
  textMuted: '#8e9cab',
  border: '#2f3d4c',
  hairline: 'rgba(142, 156, 171, 0.18)',
  shadow: 'none',
  accent: '#3d8fe0',
  accentText: '#6db3f4',
  accentWash: 'rgba(61, 143, 224, 0.18)',
  hover: 'rgba(255, 255, 255, 0.08)',
  brand: '#0d80d8',
  tile: '#ffffff',
  tileIcon: '#1a77ca',
  tileAdvice: '#a06c00',
  success: '#3fcf8e',
  warning: '#f2c14e',
  danger: '#f07a72',
  series: ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9'],
};

const light: Tokens = {
  panel: '#ffffff',
  chrome: '#f4f6f8',
  surface: '#ffffff',
  surfaceSunken: '#f3f5f7',
  textStrong: '#111a23',
  text: '#2f3b47',
  textMuted: '#5f6c79',
  border: '#dde3e9',
  hairline: 'rgba(95, 108, 121, 0.15)',
  shadow: '0 1px 2px rgba(17, 26, 35, 0.06), 0 1px 4px rgba(17, 26, 35, 0.04)',
  accent: '#1a77ca',
  accentText: '#0f64b0',
  accentWash: '#e6f1fb',
  hover: 'rgba(17, 26, 35, 0.05)',
  brand: '#0d80d8',
  tile: '#ffffff',
  tileIcon: '#1a77ca',
  tileAdvice: '#a06c00',
  success: '#00875a',
  warning: '#e8a30a',
  danger: '#c7372f',
  series: ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#4a3aa7'],
};

export function getTokens(theme: Theme): Tokens {
  return theme === 'dark' ? dark : light;
}

export function useTokens(): Tokens {
  return getTokens(useTheme());
}

// visuallyHidden keeps content available to screen readers without showing it.
export const visuallyHidden: CSSProperties = {
  position: 'absolute',
  width: '1px',
  height: '1px',
  padding: 0,
  margin: '-1px',
  overflow: 'hidden',
  clip: 'rect(0, 0, 0, 0)',
  whiteSpace: 'nowrap',
  border: 0,
};

// cardStyle is the standard container: a bordered card, as in Octopus Deploy.
export function cardStyle(t: Tokens): CSSProperties {
  return {
    backgroundColor: t.surface,
    border: `1px solid ${t.border}`,
    borderRadius: '4px',
    boxShadow: t.shadow,
  };
}
