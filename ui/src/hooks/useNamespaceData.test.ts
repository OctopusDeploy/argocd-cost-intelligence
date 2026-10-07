import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useNamespaceData } from './useNamespaceData';
import { CostData, fetchNamespaceApps, fetchNamespaces, NamespaceSummary } from '../utils/api';

vi.mock('../utils/api', () => ({ fetchNamespaces: vi.fn(), fetchNamespaceApps: vi.fn() }));

const REFRESH_MS = 30_000;
const measuredAt = '2026-09-16T08:00:00Z';

const summary = (namespace: string, totalCost: number): NamespaceSummary => ({
  namespace,
  totalCost,
  idleCost: 0,
  appCount: 1,
  measuredAt,
});

const record = (name: string): CostData => ({ applicationName: name, measuredAt } as CostData);

// Fake timers have to be in place before the hook mounts, or the refresh interval it schedules is
// a real one that advanceTimersByTime never sees.
beforeEach(() => {
  vi.useFakeTimers();
  // mockResolvedValue leaves the call history alone, and these tests count calls.
  vi.mocked(fetchNamespaces).mockClear();
  vi.mocked(fetchNamespaceApps).mockClear();
  vi.mocked(fetchNamespaces).mockResolvedValue([summary('argocd', 2018.1), summary('prometheus', 865.5)]);
  vi.mocked(fetchNamespaceApps).mockResolvedValue([record('redis')]);
});

afterEach(() => {
  vi.useRealTimers();
});

// settle runs the pending fetches to completion; advance also lets the interval fire.
const advance = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)));
const settle = () => advance(0);

describe('useNamespaceData', () => {
  it('starts on the namespace it is given, rather than the most expensive one', async () => {
    const { result } = renderHook(() => useNamespaceData('prometheus'));
    await settle();

    expect(result.current.namespaces).toHaveLength(2);
    expect(result.current.selectedNs).toBe('prometheus');
    expect(vi.mocked(fetchNamespaceApps)).toHaveBeenCalledWith('prometheus');
  });

  it('falls back to the most expensive namespace when the one it is given is gone', async () => {
    const { result } = renderHook(() => useNamespaceData('deleted'));
    await settle();

    expect(result.current.selectedNs).toBe('argocd');
  });

  it('picks up costs measured while the page is open', async () => {
    const { result } = renderHook(() => useNamespaceData());
    await settle();
    expect(result.current.namespaces[0].totalCost).toBe(2018.1);

    vi.mocked(fetchNamespaces).mockResolvedValue([summary('argocd', 2500), summary('prometheus', 865.5)]);
    await advance(REFRESH_MS);

    expect(result.current.namespaces[0].totalCost).toBe(2500);
  });

  it('keeps the figures on screen when a background refresh fails', async () => {
    const { result } = renderHook(() => useNamespaceData());
    await settle();

    vi.mocked(fetchNamespaces).mockRejectedValue(new Error('502: proxy error'));
    vi.mocked(fetchNamespaceApps).mockRejectedValue(new Error('502: proxy error'));
    await advance(REFRESH_MS);
    // The refresh really did run and really did fail.
    expect(vi.mocked(fetchNamespaces)).toHaveBeenCalledTimes(2);

    expect(result.current.namespaces).toHaveLength(2);
    expect(result.current.apps).toHaveLength(1);
    expect(result.current.namespacesError).toBeNull();
    expect(result.current.appsError).toBeNull();
    expect(result.current.namespacesLoading).toBe(false);
  });

  it('reports a failure of the reload the viewer asked for', async () => {
    const { result } = renderHook(() => useNamespaceData());
    await settle();

    vi.mocked(fetchNamespaces).mockRejectedValue(new Error('502: proxy error'));
    await act(async () => {
      await result.current.reload();
    });

    expect(result.current.namespacesError).toBe('502: proxy error');
  });

  it('stops refreshing once the page is closed', async () => {
    const { result, unmount } = renderHook(() => useNamespaceData());
    await settle();
    // One refresh while it is open, so the next assertion is about the unmount, not the timer.
    await advance(REFRESH_MS);
    expect(vi.mocked(fetchNamespaces)).toHaveBeenCalledTimes(2);
    expect(result.current.namespaces).toHaveLength(2);

    unmount();
    vi.mocked(fetchNamespaces).mockClear();
    await advance(REFRESH_MS * 3);

    expect(vi.mocked(fetchNamespaces)).not.toHaveBeenCalled();
  });
});
