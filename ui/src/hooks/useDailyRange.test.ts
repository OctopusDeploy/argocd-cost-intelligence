import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useDailyRange } from './useDailyRange';
import api, { DailyCost, DailyCostData } from '../utils/api';

vi.mock('../utils/api', () => ({ default: { getDailyCosts: vi.fn() } }));

const TODAY = '2026-10-03';
const REFRESH_MS = 30_000;
const day = (date: string, totalCost: number): DailyCost => ({
  date,
  totalCost,
  cpuCost: 0,
  memoryCost: 0,
  networkCost: 0,
  storageCost: 0,
});
const answer = (...days: DailyCost[]) => ({ dailyCosts: days }) as DailyCostData;
const getDailyCosts = vi.mocked(api.getDailyCosts);

beforeEach(() => {
  vi.useFakeTimers();
  getDailyCosts.mockReset();
  getDailyCosts.mockResolvedValue(answer(day('2026-09-01', 1)));
});

afterEach(() => {
  vi.useRealTimers();
});

const advance = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)));
const settle = () => advance(0);

describe('useDailyRange', () => {
  it('fetches nothing without a range', async () => {
    const { result } = renderHook(() => useDailyRange('redis', undefined, null, TODAY));
    await settle();

    expect(getDailyCosts).not.toHaveBeenCalled();
    expect(result.current).toMatchObject({ loading: false, dailyCosts: null, error: null });
  });

  it('fetches the range, and a new range from scratch', async () => {
    const { result, rerender } = renderHook(({ range }) => useDailyRange('redis', undefined, range, TODAY), {
      initialProps: { range: { from: '2026-09-01', to: '2026-09-30' } },
    });
    expect(result.current.loading).toBe(true);
    await settle();
    expect(getDailyCosts).toHaveBeenCalledWith('redis', { from: '2026-09-01', to: '2026-09-30' }, undefined);
    expect(result.current.dailyCosts).toHaveLength(1);

    getDailyCosts.mockReturnValue(new Promise(() => {}));
    rerender({ range: { from: '2026-07-06', to: TODAY } });
    expect(result.current).toMatchObject({ loading: true, dailyCosts: null });
  });

  it('drops the answer for a range that is no longer shown', async () => {
    let answerOld: (data: DailyCostData) => void = () => {};
    getDailyCosts.mockReturnValueOnce(new Promise(resolve => (answerOld = resolve)));
    const { result, rerender } = renderHook(({ range }) => useDailyRange('redis', undefined, range, TODAY), {
      initialProps: { range: { from: '2026-09-01', to: '2026-09-30' } },
    });
    getDailyCosts.mockResolvedValueOnce(answer(day('2026-08-01', 7)));
    rerender({ range: { from: '2026-08-01', to: '2026-08-31' } });
    await settle();

    answerOld(answer(day('2026-09-01', 99)));
    await settle();

    expect(result.current.dailyCosts?.[0].totalCost).toBe(7);
  });

  it('refreshes a short range that reaches today, keeping its data when a refresh fails', async () => {
    const { result } = renderHook(() => useDailyRange('redis', undefined, { from: '2026-09-04', to: TODAY }, TODAY));
    await settle();

    getDailyCosts.mockRejectedValueOnce(new Error('proxy returned 502'));
    await advance(REFRESH_MS);

    expect(getDailyCosts).toHaveBeenCalledTimes(2);
    expect(result.current).toMatchObject({ error: null, loading: false });
    expect(result.current.dailyCosts).toHaveLength(1);

    getDailyCosts.mockRejectedValueOnce(new Error('proxy returned 502'));
    await act(() => result.current.refetch());
    expect(result.current.error).toBe('proxy returned 502');
  });

  it('does not refresh a year, or a range that has ended', async () => {
    renderHook(() => useDailyRange('redis', undefined, { from: '2025-10-04', to: TODAY }, TODAY));
    renderHook(() => useDailyRange('redis', undefined, { from: '2026-09-01', to: '2026-09-30' }, TODAY));
    await settle();
    await advance(REFRESH_MS * 3);

    expect(getDailyCosts).toHaveBeenCalledTimes(2);
  });
});
