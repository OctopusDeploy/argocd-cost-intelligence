import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useRightsizing } from './useRightsizing';
import api, { Rightsizing } from '../utils/api';
import { shopRightsizing } from '../test/rightsizing';

vi.mock('../utils/api', () => ({ default: { getRightsizing: vi.fn() } }));

const getRightsizing = vi.mocked(api.getRightsizing);

// deferred returns a promise the test resolves when it chooses.
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(res => {
    resolve = res;
  });
  return { promise, resolve };
}

describe('useRightsizing', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    getRightsizing.mockReset();
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('loads the recommendations and refreshes them every five minutes', async () => {
    getRightsizing.mockResolvedValue(shopRightsizing());
    const { result } = renderHook(() => useRightsizing('shop', undefined));
    await act(async () => {});

    expect(result.current.rightsizing?.monthlySaving).toBe(41.31);
    expect(getRightsizing).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000 - 1);
    });
    expect(getRightsizing).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(getRightsizing).toHaveBeenCalledTimes(2);
  });

  it('keeps the last answer when a refresh fails', async () => {
    getRightsizing.mockResolvedValueOnce(shopRightsizing()).mockRejectedValueOnce(new Error('boom'));
    const { result } = renderHook(() => useRightsizing('shop', undefined));
    await act(async () => {});

    await act(async () => {
      await result.current.refetch();
    });

    expect(result.current.rightsizing?.monthlySaving).toBe(41.31);
  });

  it('drops a slow answer for the application it showed before', async () => {
    const slow = deferred<Rightsizing>();
    getRightsizing.mockReturnValueOnce(slow.promise).mockResolvedValueOnce(shopRightsizing({ monthlySaving: 7 }));
    const { result, rerender } = renderHook(({ app }) => useRightsizing(app, undefined), {
      initialProps: { app: 'shop' },
    });

    rerender({ app: 'billing' });
    await act(async () => {});
    await act(async () => slow.resolve(shopRightsizing()));

    expect(result.current.rightsizing?.monthlySaving).toBe(7);
  });

  it("clears the previous application's recommendations while the next one loads", async () => {
    const next = deferred<Rightsizing>();
    getRightsizing.mockResolvedValueOnce(shopRightsizing()).mockReturnValueOnce(next.promise);
    const { result, rerender } = renderHook(({ app }) => useRightsizing(app, undefined), {
      initialProps: { app: 'shop' },
    });
    await act(async () => {});
    expect(result.current.rightsizing).not.toBeNull();

    rerender({ app: 'billing' });
    expect(result.current.rightsizing).toBeNull();
  });

  it('asks for nothing without an application', () => {
    renderHook(() => useRightsizing(undefined, undefined));

    expect(getRightsizing).not.toHaveBeenCalled();
  });
});
