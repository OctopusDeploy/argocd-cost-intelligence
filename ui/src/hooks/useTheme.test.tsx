import React from 'react';
import { act, render, renderHook, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useTheme } from './useTheme';
import { useTokens } from '../theme/tokens';

// This jsdom build ships no localStorage, and Argo CD keeps the theme there, so the test provides
// one. It is also what makes readTheme's own try/catch meaningful: without a store it always
// answers 'light'.
const store = new Map<string, string>();
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
    removeItem: (key: string) => void store.delete(key),
    clear: () => store.clear(),
  },
});

// Nor does it ship matchMedia, which Argo CD uses to resolve an 'auto' preference from the
// operating system's colour scheme. This one answers the dark-scheme query from osDark and
// notifies its 'change' listeners when setOsDark flips it.
const DARK_SCHEME = '(prefers-color-scheme: dark)';
let osDark = false;
const schemeListeners = new Set<() => void>();
const fakeMatchMedia = (query: string) => ({
  media: query,
  get matches() {
    return query === DARK_SCHEME && osDark;
  },
  addEventListener: (type: string, listener: () => void) => {
    if (type === 'change') schemeListeners.add(listener);
  },
  removeEventListener: (type: string, listener: () => void) => {
    if (type === 'change') schemeListeners.delete(listener);
  },
});
Object.defineProperty(window, 'matchMedia', { configurable: true, writable: true, value: fakeMatchMedia });

const setOsDark = (dark: boolean) => {
  osDark = dark;
  for (const listener of [...schemeListeners]) listener();
};

// Argo CD ignores preferences stored without a version of at least 5.
const setStoredTheme = (theme: string) =>
  localStorage.setItem('view_preferences', JSON.stringify({ version: 5, theme }));

// When nothing valid is stored, the default depends on the Argo CD release, which the plugin tells
// by the React it exposes: 16 on 3.4, 19 on 3.5. The tests run on React 19.
const realReactVersion = React.version;
const setReactVersion = (version: string) => {
  (React as { version: string }).version = version;
};

// Reader is a stand-in for the many components that read a colour token, each of which reads the
// theme through useTokens.
const Reader: React.FC<{ id: string }> = ({ id }) => {
  const t = useTokens();
  return <span data-testid={id}>{t.surface}</span>;
};

beforeEach(() => {
  store.clear();
  osDark = false;
  vi.useFakeTimers();
});

afterEach(() => {
  setReactVersion(realReactVersion);
  window.matchMedia = fakeMatchMedia as unknown as typeof window.matchMedia;
  vi.useRealTimers();
});

describe('useTheme', () => {
  it('reads the theme Argo CD stored', () => {
    setStoredTheme('dark');

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('dark');
  });

  it("uses Argo CD 3.4's light default when no preference is stored", () => {
    setReactVersion('16.14.0');
    osDark = true;

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('light');
  });

  it("uses Argo CD 3.5's auto default when no preference is stored", () => {
    setReactVersion('19.1.0');
    osDark = true;

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('dark');
  });

  it.each([
    ['unreadable', 'not json'],
    ['stored without a version', JSON.stringify({ theme: 'light' })],
    ['stored by an older version', JSON.stringify({ version: 4, theme: 'light' })],
    ['stored without a theme', JSON.stringify({ version: 5 })],
  ])('uses the default when the preference is %s, as Argo CD does', (_, raw) => {
    store.set('view_preferences', raw);
    osDark = true;

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('dark');
  });

  it('polls once for the whole page, however many components read it', () => {
    render(
      <>
        <Reader id="a" />
        <Reader id="b" />
        <Reader id="c" />
        <Reader id="d" />
        <Reader id="e" />
      </>
    );

    expect(vi.getTimerCount()).toBe(1);
  });

  it('tells every reader about a theme change', () => {
    const { getByTestId } = render(
      <>
        <Reader id="a" />
        <Reader id="b" />
      </>
    );
    const light = getByTestId('a').textContent;

    act(() => {
      setStoredTheme('dark');
      vi.advanceTimersByTime(750);
    });

    expect(getByTestId('a').textContent).not.toBe(light);
    expect(getByTestId('a').textContent).toBe(getByTestId('b').textContent);
  });

  it('stops polling once nothing is reading the theme', () => {
    const { unmount } = render(
      <>
        <Reader id="a" />
        <Reader id="b" />
      </>
    );
    expect(vi.getTimerCount()).toBe(1);

    unmount();

    expect(vi.getTimerCount()).toBe(0);
  });

  it('keeps polling while any reader is left', () => {
    const { rerender } = render(
      <>
        <Reader id="a" />
        <Reader id="b" />
      </>
    );

    rerender(<Reader id="a" />);

    expect(vi.getTimerCount()).toBe(1);
    expect(screen.getByTestId('a')).toBeTruthy();
  });

  it('follows the operating system when the preference is auto', () => {
    setStoredTheme('auto');
    osDark = true;

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('dark');
  });

  it('ignores the operating system when the preference is explicit', () => {
    setStoredTheme('light');
    osDark = true;

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('light');
  });

  it('falls back to light under auto when the browser cannot report a colour scheme', () => {
    setStoredTheme('auto');
    osDark = true;
    window.matchMedia = undefined as unknown as typeof window.matchMedia;

    const { result } = renderHook(() => useTheme());

    expect(result.current).toBe('light');
  });

  it('follows an operating system change under auto without waiting for the poll', () => {
    setStoredTheme('auto');
    const { getByTestId } = render(
      <>
        <Reader id="a" />
        <Reader id="b" />
      </>
    );
    const light = getByTestId('a').textContent;

    act(() => setOsDark(true));

    expect(getByTestId('a').textContent).not.toBe(light);
    expect(getByTestId('a').textContent).toBe(getByTestId('b').textContent);
  });

  it('watches the colour scheme once for the whole page, and stops with the last reader', () => {
    const { unmount } = render(
      <>
        <Reader id="a" />
        <Reader id="b" />
        <Reader id="c" />
      </>
    );
    expect(schemeListeners.size).toBe(1);

    unmount();

    expect(schemeListeners.size).toBe(0);
  });
});
