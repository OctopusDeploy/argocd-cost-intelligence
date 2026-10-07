import { useLayoutEffect } from 'react';
import type { CSSProperties } from 'react';
import { Tokens } from './tokens';

// PLUGIN_CLASS marks the plugin's two root elements. Every rule below is scoped to it so the
// stylesheet can never reach the rest of Argo CD's UI.
export const PLUGIN_CLASS = 'cost-intel';

const STYLE_ID = 'cost-intel-styles';

// Argo CD's own stylesheet ends with
//   body button, body a, body span, body input { outline: none !important; }
// An !important author rule also beats a plain inline style, so inline `outline` is not an option:
// these rules have to be !important themselves, and more specific than two type selectors.
const CSS = `
.${PLUGIN_CLASS} :focus-visible {
  outline: 2px solid var(--cost-intel-accent) !important;
  outline-offset: 2px;
}

/* An inset shadow tints whatever background the element carries. Components set those inline,
   and a stylesheet only beats an inline declaration when it is !important. */
.${PLUGIN_CLASS} button:hover:not(:disabled) {
  box-shadow: inset 0 0 0 999px var(--cost-intel-hover) !important;
}

.${PLUGIN_CLASS} a:hover {
  text-decoration: underline !important;
}
`;

// ensureGlobalStyles adds the stylesheet the first time it is called. Both root views call it, and
// the extension bundle can be loaded into a page that already has it.
export function ensureGlobalStyles(): void {
  if (typeof document === 'undefined' || document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.appendChild(style);
}

// useGlobalStyles installs the stylesheet before the first paint, so nothing renders unstyled.
export function useGlobalStyles(): void {
  useLayoutEffect(() => {
    ensureGlobalStyles();
  }, []);
}

// pluginRootStyle exposes the tokens the stylesheet needs as custom properties. They are inherited,
// so the rules above work anywhere inside a root element.
export function pluginRootStyle(t: Tokens): CSSProperties {
  return {
    '--cost-intel-accent': t.accent,
    '--cost-intel-hover': t.hover,
  } as CSSProperties;
}
