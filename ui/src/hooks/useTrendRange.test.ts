import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useTrendRange } from './useTrendRange';

const TODAY = '2026-10-03';
const search = () => window.location.search;
const at = (url: string) => window.history.replaceState(null, '', url);

describe('useTrendRange', () => {
  it('shows the last 30 days by default, with nothing in the URL', () => {
    const { result } = renderHook(() => useTrendRange(TODAY));

    expect(result.current.id).toBe('30d');
    expect(result.current.range).toEqual({ from: '2026-09-04', to: TODAY });
    expect(search()).toBe('');
  });

  it('records a preset as navigation, so Back returns to the previous range', () => {
    const { result } = renderHook(() => useTrendRange(TODAY));
    const before = window.history.length;

    act(() => result.current.choose('last'));

    expect(result.current.range).toEqual({ from: '2026-09-01', to: '2026-09-30' });
    expect(search()).toBe('?costRange=last');
    expect(window.history.length).toBe(before + 1);
  });

  it('keeps a custom range in the URL, refining it in place', () => {
    const { result } = renderHook(() => useTrendRange(TODAY));
    const before = window.history.length;

    act(() => result.current.setCustom({ from: '2026-08-01', to: '2026-08-31' }));
    act(() => result.current.setCustom({ from: '2026-08-01', to: '2026-09-15' }));

    expect(result.current.id).toBe('custom');
    expect(result.current.range).toEqual({ from: '2026-08-01', to: '2026-09-15' });
    expect(new URLSearchParams(search()).get('costTo')).toBe('2026-09-15');
    expect(window.history.length).toBe(before + 1);

    act(() => result.current.choose('90d'));
    expect(search()).toBe('?costRange=90d');
  });

  it('opens on a custom range from a link', () => {
    at('/app?costRange=custom&costFrom=2026-07-01&costTo=2026-07-31');

    const { result } = renderHook(() => useTrendRange(TODAY));

    expect(result.current.id).toBe('custom');
    expect(result.current.range).toEqual({ from: '2026-07-01', to: '2026-07-31' });
  });

  it('falls back to the default for a custom range it cannot show, and corrects the URL', () => {
    at('/app?costRange=custom&costFrom=2026-07-31&costTo=2026-07-01');

    const { result } = renderHook(() => useTrendRange(TODAY));

    expect(result.current.id).toBe('30d');
    expect(search()).toBe('');
  });
});
