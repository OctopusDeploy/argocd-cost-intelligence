import React, { useEffect, useState } from 'react';

export type Theme = 'light' | 'dark';

// Argo CD raises no event when the theme changes, so the stored preference is polled. This is how
// often.
const POLL_MS = 750;

// Argo CD resolves an 'auto' preference from the operating system's colour scheme, as its own
// useTheme (ui/src/app/shared/utils.ts) does. 'auto' is the default from Argo CD 3.5.
const DARK_SCHEME = '(prefers-color-scheme: dark)';

// jsdom, among others, has no matchMedia.
function schemeQuery(): MediaQueryList | null {
  return typeof window.matchMedia === 'function' ? window.matchMedia(DARK_SCHEME) : null;
}

// Argo CD discards stored preferences without a version or older than this one and uses its
// defaults instead (minVer in ui/src/app/shared/services/view-preferences-service.ts).
const MIN_PREFERENCES_VERSION = 5;

// Argo CD's default theme preference, used when nothing valid is stored: 'light' up to 3.4, 'auto'
// from 3.5. localStorage doesn't say which Argo CD is running, but the React it exposes as
// window.React (which webpack maps 'react' to) does: 16 on 3.4, 19 on 3.5.
function defaultPreference(): string {
  return parseInt(React.version, 10) > 16 ? 'auto' : 'light';
}

// Argo CD stores its view preferences in localStorage under "view_preferences" as a JSON string
// with a top-level "version" and "theme": 'light', 'dark' or 'auto'. Its Layout
// (ui/src/app/shared/components/layout/layout.tsx) does also render the resolved theme as a
// `theme-dark` / `theme-light` class on a wrapper div, but that is private markup rather than an
// API, so the stored preference is read instead.
function readPreference(): string {
  try {
    const raw = localStorage.getItem('view_preferences');
    if (raw) {
      const prefs = JSON.parse(raw);
      if (prefs.version >= MIN_PREFERENCES_VERSION && typeof prefs.theme === 'string') return prefs.theme;
    }
  } catch { /* unreadable, so Argo CD uses its defaults too */ }
  return defaultPreference();
}

function readTheme(): Theme {
  const preference = readPreference();
  if (preference === 'dark') return 'dark';
  if (preference === 'auto') return schemeQuery()?.matches ? 'dark' : 'light';
  return 'light';
}

// Every component that reads a colour token reads the theme, so the poll is shared: one timer for
// the whole page, started with the first subscriber and stopped after the last one leaves.
const listeners = new Set<() => void>();
let current: Theme = readTheme();
let timer: ReturnType<typeof setInterval> | null = null;
let scheme: MediaQueryList | null = null;

function refresh(): void {
  const next = readTheme();
  if (next === current) return;
  current = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (listeners.size === 1) {
    // Nothing was watching until now, so the cached value may be stale.
    refresh();
    // Fires when a different tab or window changes localStorage.
    window.addEventListener('storage', refresh);
    // Fires when the operating system switches between light and dark, which matters under 'auto'.
    scheme = schemeQuery();
    scheme?.addEventListener('change', refresh);
    timer = setInterval(refresh, POLL_MS);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size > 0) return;
    window.removeEventListener('storage', refresh);
    scheme?.removeEventListener('change', refresh);
    scheme = null;
    if (timer !== null) clearInterval(timer);
    timer = null;
  };
}

// useState and useEffect rather than useSyncExternalStore: the plugin runs on whatever React Argo
// CD exposes as window.React, which is React 16 up to Argo CD 3.4, and useSyncExternalStore only
// exists from React 18.
export function useTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(current);
  useEffect(() => {
    const update = () => setTheme(current);
    const unsubscribe = subscribe(update);
    // The theme may have changed between this render and the subscription.
    update();
    return unsubscribe;
  }, []);
  return theme;
}
