import { beforeEach, describe, expect, it } from 'vitest';
import { ensureGlobalStyles, PLUGIN_CLASS, pluginRootStyle } from './globalStyles';
import { FONT_FAMILY } from './font';
import { getTokens } from './tokens';

function sheets(): HTMLStyleElement[] {
  return [...document.head.querySelectorAll('style')];
}

describe('ensureGlobalStyles', () => {
  beforeEach(() => {
    document.head.innerHTML = '';
  });

  it('adds the stylesheet once, however often it is called', () => {
    ensureGlobalStyles();
    ensureGlobalStyles();
    ensureGlobalStyles();

    expect(sheets()).toHaveLength(1);
  });

  // Argo CD's own `outline: none !important` on every button, link and input beats a plain inline
  // style, so the focus ring has to come from a rule that is itself !important.
  it('marks the focus ring !important so Argo CD cannot zero it', () => {
    ensureGlobalStyles();

    const css = sheets()[0].textContent ?? '';
    const focusRule = css.slice(css.indexOf(':focus-visible'), css.indexOf('}', css.indexOf(':focus-visible')));
    expect(focusRule).toContain('outline: 2px solid var(--cost-intel-accent) !important');
  });

  it('scopes every rule to the plugin root', () => {
    ensureGlobalStyles();

    const selectors = (sheets()[0].textContent ?? '')
      .split('}')
      .map(block => block.slice(block.lastIndexOf('{') === -1 ? 0 : 0, block.indexOf('{')).trim())
      .filter(selector => selector !== '' && !selector.startsWith('@') && !selector.startsWith('/*'));

    expect(selectors.length).toBeGreaterThan(0);
    for (const selector of selectors) {
      expect(selector.includes(`.${PLUGIN_CLASS}`)).toBe(true);
    }
  });
});

describe('the bundled font', () => {
  beforeEach(() => {
    document.head.innerHTML = '';
  });

  // Argo CD's page does not load Inter, so the stylesheet has to register the bundled copy under the
  // family name the plugin asks for.
  it('registers the family the plugin roots use', () => {
    ensureGlobalStyles();

    const css = sheets()[0].textContent ?? '';
    const family = FONT_FAMILY.slice(0, FONT_FAMILY.indexOf(','));
    expect(css).toContain('@font-face');
    expect(css).toContain(`font-family: ${family};`);
    expect(pluginRootStyle(getTokens('light')).fontFamily).toBe(FONT_FAMILY);
  });

  // The SIL Open Font License asks for the copyright and licence to travel with the font.
  it('carries the font licence next to the font', () => {
    ensureGlobalStyles();

    const css = sheets()[0].textContent ?? '';
    expect(css).toContain('Copyright 2016 The Inter Project Authors');
    expect(css).toContain('SIL Open Font License, Version 1.1');
  });
});

describe('pluginRootStyle', () => {
  it('publishes the tokens the stylesheet reads, per theme', () => {
    const dark = pluginRootStyle(getTokens('dark')) as Record<string, string>;
    const light = pluginRootStyle(getTokens('light')) as Record<string, string>;

    expect(dark['--cost-intel-accent']).toBe(getTokens('dark').accent);
    expect(dark['--cost-intel-hover']).toBe(getTokens('dark').hover);
    expect(light['--cost-intel-accent']).toBe(getTokens('light').accent);
  });
});
