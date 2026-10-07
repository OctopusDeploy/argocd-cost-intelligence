import type { CSSProperties } from 'react';
import { Theme, useTheme } from '../hooks/useTheme';

// Colour tokens taken from Argo CD's own theme (argo-ui src/styles/config.scss and
// theme.scss), so the plugin reads as part of the host UI in both themes.
export interface Tokens {
  surface: string;
  surfaceSunken: string;
  textStrong: string;
  text: string;
  textMuted: string;
  border: string;
  hairline: string;
  // accent marks non-text elements (active tab, chart line); accentText is for text.
  accent: string;
  accentText: string;
  accentWash: string;
  // hover tints a control's own background on pointer hover, whatever that background is.
  hover: string;
  success: string;
  warning: string;
  danger: string;
  // series is the categorical chart palette in fixed slot order (blue, orange, aqua, yellow,
  // magenta, green, violet), validated for colour-vision deficiency against `surface`.
  series: string[];
}

const dark: Tokens = {
  surface: '#303237',
  surfaceSunken: '#28292a',
  textStrong: '#ffffff',
  text: '#dee6eb',
  textMuted: '#8fa4b1',
  border: '#495763',
  hairline: 'rgba(143, 164, 177, 0.18)',
  accent: '#1fbdd0',
  accentText: '#1fbdd0',
  accentWash: 'rgba(31, 189, 208, 0.18)',
  hover: 'rgba(255, 255, 255, 0.08)',
  success: '#18be94',
  warning: '#f4c030',
  danger: '#e96d76',
  series: ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9'],
};

const light: Tokens = {
  surface: '#ffffff',
  surfaceSunken: '#eff3f5',
  textStrong: '#000000',
  text: '#363c4a',
  textMuted: '#495763',
  border: '#ccd6dd',
  hairline: 'rgba(73, 87, 99, 0.15)',
  accent: '#00a2b3',
  accentText: '#006f8a',
  accentWash: 'rgba(0, 162, 179, 0.15)',
  hover: 'rgba(0, 0, 0, 0.05)',
  success: '#3f946d',
  warning: '#f4c030',
  danger: '#c04b4f',
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

// cardStyle matches Argo CD's white-box container.
export function cardStyle(t: Tokens): CSSProperties {
  return {
    backgroundColor: t.surface,
    borderRadius: '4px',
    boxShadow: '1px 2px 3px rgba(0, 0, 0, 0.1)',
  };
}
