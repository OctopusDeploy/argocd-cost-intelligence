import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useWasteRanking } from './useWasteRanking';
import { ApplicationListError, fetchWasteRanking, WasteRanking } from '../utils/api';

vi.mock('../utils/api', () => ({ fetchWasteRanking: vi.fn(), ApplicationListError: class extends Error {} }));

const REFRESH_MS = 30_000;

const ranking = (idleCost: number): WasteRanking => ({ apps: [], idleCost, namespaceOnly: 0, noPods: 0 });

// Fake timers have to be in place before the hook mounts, or its refresh interval is a real one.
beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(fetchWasteRanking).mockReset();
  vi.mocked(fetchWasteRanking).mockResolvedValue(ranking(10));
});

afterEach(() => {
  vi.useRealTimers();
});

const advance = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)));
const settle = () => advance(0);

describe('useWasteRanking', () => {
  it('picks up costs measured while the page is open', async () => {
    const { result } = renderHook(() => useWasteRanking(true));
    await settle();
    expect(result.current.ranking?.idleCost).toBe(10);

    vi.mocked(fetchWasteRanking).mockResolvedValue(ranking(20));
    await advance(REFRESH_MS);

    expect(result.current.ranking?.idleCost).toBe(20);
    expect(result.current.loading).toBe(false);
  });

  it('keeps the ranking on screen when a background refresh fails', async () => {
    const { result } = renderHook(() => useWasteRanking(true));
    await settle();

    vi.mocked(fetchWasteRanking).mockRejectedValue(new Error('500: failed to list cost records'));
    await advance(REFRESH_MS);

    expect(result.current.ranking?.idleCost).toBe(10);
    expect(result.current.error).toBeNull();
  });

  it("explains that the applications couldn't be listed", async () => {
    vi.mocked(fetchWasteRanking).mockRejectedValue(new ApplicationListError("Couldn't list the Argo CD applications."));
    const { result } = renderHook(() => useWasteRanking(true));
    await settle();

    expect(result.current.error).toBe("Couldn't list the Argo CD applications.");
  });

  it('reports any other failure of the load the viewer asked for', async () => {
    const { result } = renderHook(() => useWasteRanking(true));
    await settle();

    vi.mocked(fetchWasteRanking).mockRejectedValue(new Error('500: failed to list cost records'));
    await act(() => result.current.reload());

    expect(result.current.error).toBe("Couldn't load the ranking (500: failed to list cost records).");
  });

  it('stops refreshing once the page is closed', async () => {
    const { unmount } = renderHook(() => useWasteRanking(true));
    await settle();
    unmount();

    await advance(REFRESH_MS * 2);

    expect(vi.mocked(fetchWasteRanking)).toHaveBeenCalledTimes(1);
  });

  it('loads nothing until it is enabled', async () => {
    const { result, rerender } = renderHook(({ enabled }) => useWasteRanking(enabled), {
      initialProps: { enabled: false },
    });
    await act(() => result.current.reload());
    await advance(REFRESH_MS * 2);

    expect(vi.mocked(fetchWasteRanking)).not.toHaveBeenCalled();
    expect(result.current.loading).toBe(false);

    rerender({ enabled: true });
    await settle();

    expect(result.current.ranking?.idleCost).toBe(10);
  });

  it('stops refreshing once it is disabled', async () => {
    const { rerender } = renderHook(({ enabled }) => useWasteRanking(enabled), { initialProps: { enabled: true } });
    await settle();
    rerender({ enabled: false });

    await advance(REFRESH_MS * 2);

    expect(vi.mocked(fetchWasteRanking)).toHaveBeenCalledTimes(1);
  });

  it('keeps the latest answer when an older one arrives after it', async () => {
    const { result } = renderHook(() => useWasteRanking(true));
    await settle();

    let answerOld: (value: WasteRanking) => void = () => undefined;
    vi.mocked(fetchWasteRanking).mockReturnValueOnce(new Promise(resolve => (answerOld = resolve)));
    await advance(REFRESH_MS);
    vi.mocked(fetchWasteRanking).mockResolvedValueOnce(ranking(30));
    await act(() => result.current.reload());
    await act(async () => answerOld(ranking(20)));

    expect(result.current.ranking?.idleCost).toBe(30);
  });
});
