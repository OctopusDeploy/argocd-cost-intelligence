import { useCallback, useEffect, useState } from 'react';

// How a change is recorded. Navigation ('push') can be undone with the browser's Back button;
// refinements of what is already on screen ('replace') only keep the address bar truthful.
export type UrlWrite = 'push' | 'replace';

function readParam(key: string): string | null {
  if (typeof window === 'undefined') return null;
  return new URLSearchParams(window.location.search).get(key);
}

function writeParam(key: string, value: string, mode: UrlWrite): void {
  if (typeof window === 'undefined') return;
  const url = new URL(window.location.href);
  if (value === '') url.searchParams.delete(key);
  else url.searchParams.set(key, value);
  const next = `${url.pathname}${url.search}${url.hash}`;
  // Argo CD's router keeps its own key in history.state; carrying it over leaves it intact.
  const state = window.history.state;
  if (mode === 'push') window.history.pushState(state, '', next);
  else window.history.replaceState(state, '', next);
}

// useUrlState keeps one piece of view state in the query string, so a view can be linked to and
// survives a reload. The parameter is dropped while the value is the default, which keeps the
// plain URL clean. Values outside `allowed` are ignored, so a hand-edited link cannot break a view.
export function useUrlState<T extends string>(
  key: string,
  fallback: T,
  allowed?: readonly T[]
): [T, (next: T, mode?: UrlWrite) => void] {
  const parse = useCallback(
    (raw: string | null): T => {
      if (raw === null || raw === '') return fallback;
      if (allowed && !allowed.includes(raw as T)) return fallback;
      return raw as T;
    },
    [fallback, allowed]
  );

  const [value, setValue] = useState<T>(() => parse(readParam(key)));

  // Back and Forward move between recorded views.
  useEffect(() => {
    const onPopState = () => setValue(parse(readParam(key)));
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, [key, parse]);

  const update = useCallback(
    (next: T, mode: UrlWrite = 'push') => {
      setValue(next);
      writeParam(key, next === fallback ? '' : next, mode);
    },
    [key, fallback]
  );

  return [value, update];
}
