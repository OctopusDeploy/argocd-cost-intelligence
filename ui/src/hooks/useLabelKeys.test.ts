import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useLabelKeys } from './useLabelKeys';
import { fetchChargebackLabels } from '../utils/api';

vi.mock('../utils/api', () => ({ fetchChargebackLabels: vi.fn() }));

// deferred returns a promise the test resolves or rejects when it chooses.
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.mocked(fetchChargebackLabels).mockReset();
});

describe('useLabelKeys', () => {
  it('has no keys until the first answer, then the keys in scope', async () => {
    const answer = deferred<string[]>();
    vi.mocked(fetchChargebackLabels).mockReturnValue(answer.promise);

    const { result } = renderHook(() => useLabelKeys(null));
    expect(result.current).toEqual({ keys: null, failed: false });
    expect(vi.mocked(fetchChargebackLabels)).toHaveBeenCalledWith(undefined);

    await act(async () => answer.resolve(['app', 'team']));
    expect(result.current).toEqual({ keys: ['app', 'team'], failed: false });
  });

  it('keeps the previous keys on screen while the next namespace loads', async () => {
    const next = deferred<string[]>();
    vi.mocked(fetchChargebackLabels).mockResolvedValueOnce(['app', 'team']).mockReturnValueOnce(next.promise);

    const { result, rerender } = renderHook(({ ns }) => useLabelKeys(ns), { initialProps: { ns: null as string | null } });
    await act(async () => {});

    rerender({ ns: 'payments' });
    expect(vi.mocked(fetchChargebackLabels)).toHaveBeenLastCalledWith('payments');
    expect(result.current.keys).toEqual(['app', 'team']);

    await act(async () => next.resolve(['owner']));
    expect(result.current.keys).toEqual(['owner']);
  });

  it('reports a failure so the caller can fall back', async () => {
    vi.mocked(fetchChargebackLabels).mockRejectedValue(new Error('failed to retrieve label keys'));

    const { result } = renderHook(() => useLabelKeys(null));
    await act(async () => {});

    expect(result.current).toEqual({ keys: null, failed: true });
  });

  it('ignores an answer for a namespace no longer in scope', async () => {
    const stale = deferred<string[]>();
    vi.mocked(fetchChargebackLabels).mockReturnValueOnce(stale.promise).mockResolvedValueOnce(['owner']);

    const { result, rerender } = renderHook(({ ns }) => useLabelKeys(ns), { initialProps: { ns: 'argocd' as string | null } });
    rerender({ ns: 'payments' });
    await act(async () => {});
    await act(async () => stale.resolve(['argocd-only']));

    expect(result.current.keys).toEqual(['owner']);
  });
});
