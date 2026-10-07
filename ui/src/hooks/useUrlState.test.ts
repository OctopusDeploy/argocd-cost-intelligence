import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useUrlState } from './useUrlState';

const TABS = ['actual', 'resources', 'trends'] as const;
type TabId = (typeof TABS)[number];

const search = () => window.location.search;
const at = (url: string) => window.history.replaceState(null, '', url);

describe('useUrlState', () => {
  it('starts on the value in the query string', () => {
    at('/cost-intelligence?costTab=trends');

    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    expect(result.current[0]).toBe('trends');
  });

  it('ignores a value the view does not have', () => {
    at('/cost-intelligence?costTab=nonsense');

    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    expect(result.current[0]).toBe('actual');
  });

  it('accepts any value when there is no list to check against', () => {
    at('/cost-intelligence?costNs=prometheus');

    const { result } = renderHook(() => useUrlState<string>('costNs', ''));

    expect(result.current[0]).toBe('prometheus');
  });

  it('writes the value to the query string', () => {
    at('/cost-intelligence');
    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    act(() => result.current[1]('resources'));

    expect(result.current[0]).toBe('resources');
    expect(search()).toBe('?costTab=resources');
  });

  it('leaves the default out of the URL', () => {
    at('/cost-intelligence?costTab=trends');
    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    act(() => result.current[1]('actual'));

    expect(search()).toBe('');
  });

  it('keeps parameters it does not own, such as Argo CD’s own', () => {
    at('/applications/argocd/redis?view=Cost+Intelligence');
    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    act(() => result.current[1]('trends'));

    expect(new URLSearchParams(search()).get('view')).toBe('Cost Intelligence');
    expect(new URLSearchParams(search()).get('costTab')).toBe('trends');
  });

  it('records navigation so Back returns to the previous view', () => {
    at('/cost-intelligence');
    const before = window.history.length;
    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    act(() => result.current[1]('trends'));

    expect(window.history.length).toBe(before + 1);
  });

  it('does not record a refinement of the view already shown', () => {
    at('/cost-intelligence');
    const before = window.history.length;
    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    act(() => result.current[1]('trends', 'replace'));

    expect(window.history.length).toBe(before);
    expect(search()).toBe('?costTab=trends');
  });

  it('follows Back and Forward', () => {
    at('/cost-intelligence?costTab=trends');
    const { result } = renderHook(() => useUrlState<TabId>('costTab', 'actual', TABS));

    act(() => {
      at('/cost-intelligence?costTab=resources');
      window.dispatchEvent(new PopStateEvent('popstate'));
    });

    expect(result.current[0]).toBe('resources');
  });
});
