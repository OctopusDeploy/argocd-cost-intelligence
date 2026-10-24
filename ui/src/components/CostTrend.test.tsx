import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CostTrendComponent, RangeView } from './CostTrend';
import { presetRange, RangeId } from '../utils/ranges';
import { DailyCost } from '../utils/api';

const day = (date: string, totalCost: number): DailyCost => ({
  date,
  totalCost,
  cpuCost: totalCost * 0.6,
  memoryCost: totalCost * 0.4,
  networkCost: 0,
  storageCost: 0,
});

const days = [day('2026-09-13', 0.5), day('2026-09-14', 0.6), day('2026-09-15', 0.4)];

// statValue reads the figure shown under a summary label.
const statValue = (label: string) => screen.getByText(label).nextElementSibling?.textContent;

describe('CostTrendComponent', () => {
  it('summarises the daily series', () => {
    render(<CostTrendComponent loading={false} error={null} dailyCosts={days} />);

    expect(statValue('Average per day')).toBe('$0.50');
    expect(statValue('Highest day')).toBe('$0.60');
    expect(statValue('Lowest day')).toBe('$0.40');
    expect(statValue('Total')).toBe('$1.50');
    expect(screen.getByText('Total').parentElement?.textContent).toContain('Sep 13 – Sep 15');
  });

  it('says the series covers the whole namespace when the application shares it', () => {
    const { rerender } = render(<CostTrendComponent loading={false} error={null} dailyCosts={days} />);
    expect(screen.queryByText(/Whole .* namespace/)).toBeNull();

    rerender(<CostTrendComponent loading={false} error={null} dailyCosts={days} namespace="argocd" />);
    expect(screen.getByText('Whole argocd namespace, not only this app, queried from OpenCost for each day')).toBeTruthy();
  });

  it('reads out one day at a time from the keyboard, starting with the latest', () => {
    render(<CostTrendComponent loading={false} error={null} dailyCosts={days} />);
    const chart = screen.getByRole('group', { name: /daily cost chart/i });

    fireEvent.keyDown(chart, { key: 'ArrowLeft' });
    expect(screen.getByRole('status').textContent).toBe('Tue, Sep 15: $0.40 per day');

    fireEvent.keyDown(chart, { key: 'ArrowLeft' });
    expect(screen.getByRole('status').textContent).toBe('Mon, Sep 14: $0.60 per day');

    fireEvent.keyDown(chart, { key: 'Escape' });
    expect(screen.getByRole('status').textContent).toBe('');
  });

  it('switches to a table with the newest day first', () => {
    render(<CostTrendComponent loading={false} error={null} dailyCosts={days} />);

    fireEvent.click(screen.getByRole('button', { name: 'View as table' }));

    expect(screen.getAllByRole('row')).toHaveLength(4);
    expect(screen.getAllByRole('rowheader')[0].textContent).toBe('Tue, Sep 15');
    expect(screen.getByRole('button', { name: 'View as chart' })).toBeTruthy();
  });

  it('rounds the total and the table so they add up as shown', () => {
    // The days show as $12.96, $12.85 and $10.00, which the exact total of $35.7982 would not add up
    // to, and the CPU and memory of the last, rounded on their own, as $5.00 each beside $10.00.
    const uneven = [
      day('2026-09-13', 12.9551),
      day('2026-09-14', 12.8451),
      { ...day('2026-09-15', 9.998), cpuCost: 4.995, memoryCost: 4.995, storageCost: 0.008 },
    ];
    render(<CostTrendComponent loading={false} error={null} dailyCosts={uneven} />);

    expect(statValue('Total')).toBe('$35.81');

    fireEvent.click(screen.getByRole('button', { name: 'View as table' }));
    const latest = screen.getAllByRole('row')[1];
    expect([...latest.querySelectorAll('td')].map(cell => cell.textContent)).toEqual(['$10.00', '$5.00', '$4.99', '$0.00', '$0.01']);
  });

  describe('month-end forecast', () => {
    // Oct 4 – Oct 17 complete at $10/day, Oct 18 still being measured.
    const fortnight = [
      ...Array.from({ length: 14 }, (_, i) => day(`2026-10-${String(4 + i).padStart(2, '0')}`, 10)),
      day('2026-10-18', 3),
    ];

    afterEach(() => {
      vi.useRealTimers();
    });

    const renderOn = (iso: string, dailyCosts: DailyCost[]) => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date(iso));
      return render(<CostTrendComponent loading={false} error={null} dailyCosts={dailyCosts} />);
    };

    it('adds the month so far to the trend for the days left', () => {
      renderOn('2026-10-18T09:00:00Z', fortnight);

      // Oct 4–17 spent $140; Oct 18–31 projected at $10/day.
      expect(statValue('Month-end forecast')).toBe('$280');
      expect(screen.getByText('Month-end forecast').parentElement?.textContent).toContain('$140 so far + $140 projected');
      expect(screen.getByRole('group', { name: /Estimated month-end: \$280/ })).toBeTruthy();
      expect(screen.getByText('Est. month-end $280')).toBeTruthy();
      expect(screen.queryByText(/forecast appears after/)).toBeNull();
    });

    it('rounds what is spent and what is projected so they add up to the forecast', () => {
      // At $10.0357 a day, each rounded on its own would show $141 so far + $141 projected under $281.
      renderOn('2026-10-18T09:00:00Z', [
        ...Array.from({ length: 14 }, (_, i) => day(`2026-10-${String(4 + i).padStart(2, '0')}`, 140.5 / 14)),
        day('2026-10-18', 3),
      ]);

      expect(statValue('Month-end forecast')).toBe('$281');
      expect(screen.getByText('Month-end forecast').parentElement?.textContent).toContain('$141 so far + $140 projected');
    });

    it('reads out today as still being measured and the forecast days after it', () => {
      renderOn('2026-10-18T09:00:00Z', fortnight);
      const chart = screen.getByRole('group', { name: /daily cost chart/i });

      fireEvent.keyDown(chart, { key: 'ArrowLeft' });
      expect(screen.getByRole('status').textContent).toBe(
        'Sun, Oct 18, still being measured: $3.00 so far, trend $10.00 per day'
      );

      fireEvent.keyDown(chart, { key: 'ArrowRight' });
      expect(screen.getByRole('status').textContent).toBe('Mon, Oct 19, forecast: $10.00 per day');

      fireEvent.keyDown(chart, { key: 'End' });
      expect(screen.getByRole('status').textContent).toBe('Sat, Oct 31, forecast: $10.00 per day');
    });

    it('is left out until there are enough complete days for a trend', () => {
      // Oct 12 – 18: Oct 12, where the data starts, was only partly measured, and Oct 18 still is.
      renderOn('2026-10-18T09:00:00Z', fortnight.slice(-7));

      expect(screen.queryByText('Month-end forecast')).toBeNull();
      expect(screen.queryByText(/Est\. month-end/)).toBeNull();
      expect(screen.getByText(/forecast appears after/).textContent).toBe(
        'A month-end forecast appears after 7 complete days of cost data, 2 more days to go.'
      );
    });

    it('marks today as still being measured even without a forecast', () => {
      // Three days of an app OpenCost only started measuring on Oct 1, after $0 days without data.
      renderOn('2026-10-03T17:00:00Z', [
        day('2026-09-29', 0),
        day('2026-09-30', 0),
        day('2026-10-01', 0.02),
        day('2026-10-02', 0.02),
        day('2026-10-03', 0.01),
      ]);

      // Measuring began during Oct 1, so Oct 2 is the only complete day.
      expect(screen.getByText(/forecast appears after/).textContent).toContain('6 more days to go');
      fireEvent.keyDown(screen.getByRole('group', { name: /daily cost chart/i }), { key: 'ArrowLeft' });
      expect(screen.getByRole('status').textContent).toBe('Sat, Oct 3, still being measured: $0.01 so far');
    });
  });

  describe('range picker', () => {
    // OpenCost has Sep 18 – Oct 3 at $10/day; today, Oct 3, is still being measured.
    const recent = Array.from({ length: 16 }, (_, i) => {
      const date = new Date(Date.UTC(2026, 8, 18 + i)).toISOString().slice(0, 10);
      return day(date, i === 15 ? 4 : 10);
    });
    const choose = vi.fn();
    const setCustom = vi.fn();

    const view = (id: RangeId, overrides: Partial<RangeView> = {}): RangeView => ({
      id,
      range: id === 'custom' ? { from: '2026-09-20', to: '2026-09-25' } : presetRange(id, '2026-10-03'),
      choose,
      setCustom,
      dailyCosts: recent,
      loading: false,
      error: null,
      ...overrides,
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    const renderRange = (range: RangeView) => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date('2026-10-03T12:00:00Z'));
      choose.mockClear();
      setCustom.mockClear();
      return render(<CostTrendComponent loading={false} error={null} dailyCosts={recent} range={range} />);
    };

    it('applies a preset at once', () => {
      renderRange(view('30d'));
      expect(screen.getByRole('button', { name: '30 days' }).getAttribute('aria-pressed')).toBe('true');

      fireEvent.click(screen.getByRole('button', { name: 'Last month' }));

      expect(choose).toHaveBeenCalledWith('last');
    });

    it('starts a custom range from the days on screen, up to today', () => {
      renderRange(view('month'));

      fireEvent.click(screen.getByRole('button', { name: 'Custom…' }));

      expect(setCustom).toHaveBeenCalledWith({ from: '2026-10-01', to: '2026-10-03' });
    });

    it('applies custom dates once they make a valid range, and says what is wrong otherwise', () => {
      renderRange(view('custom'));
      const from = screen.getByLabelText('From');

      fireEvent.change(from, { target: { value: '2026-09-30' } });
      expect(setCustom).not.toHaveBeenCalled();
      expect(screen.getByText('The start must be on or before the end.')).toBeTruthy();

      fireEvent.change(from, { target: { value: '2026-09-01' } });
      expect(setCustom).toHaveBeenCalledWith({ from: '2026-09-01', to: '2026-09-25' });
    });

    it('keeps the picker while a range loads, and says where the data starts', () => {
      const { rerender } = renderRange(view('last', { dailyCosts: null, loading: true }));
      expect(screen.getByText('Loading daily costs…')).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Last month' })).toBeTruthy();

      rerender(<CostTrendComponent loading={false} error={null} dailyCosts={recent} range={view('last')} />);
      expect(statValue('Total')).toBe('$130.00'); // Sep 18 – 30
      expect(screen.getByText(/^No cost data before Sep 18\. This app\u2019s pods had not run yet, or OpenCost no longer had those days to record\.$/)).toBeTruthy();
      // September is over, so there is nothing to forecast in it.
      expect(screen.queryByText('Month-end forecast')).toBeNull();
    });

    it('says why a range has no cost data at all', () => {
      renderRange(view('1y', { dailyCosts: [] }));

      expect(
        screen.getByText(
          /^No cost data from Oct 4, 2025 to Oct 3, 2026\. This app\u2019s pods had not run yet, or OpenCost no longer had those days to record\.$/
        )
      ).toBeTruthy();
    });

    it('marks the days before the data as having none, including the $0 days OpenCost reports for them', () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date('2026-10-03T12:00:00Z'));
      const withEmptyDays = [day('2026-09-04', 0), day('2026-09-05', 0), ...recent];
      render(<CostTrendComponent loading={false} error={null} dailyCosts={withEmptyDays} range={view('30d')} />);
      const chart = screen.getByRole('group', { name: /daily cost chart/i });

      fireEvent.keyDown(chart, { key: 'Home' });
      expect(screen.getByRole('status').textContent).toBe('Fri, Sep 4: no cost data');
      fireEvent.keyDown(chart, { key: 'ArrowRight' });
      expect(screen.getByRole('status').textContent).toBe('Sat, Sep 5: no cost data');
      // Neither the empty days nor today, still being measured at $4, is the cheapest day; the total
      // keeps today's spend. Sep 18, where measuring began, is not a complete day either.
      expect(statValue('Lowest day')).toBe('$10.00');
      expect(statValue('Total')).toBe('$154.00');
      expect(screen.getByText('Average per day').parentElement?.textContent).toContain('Over 14 complete days');
    });

    it('leaves the partly measured first day out of the per-day figures but not the total', () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date('2026-10-03T12:00:00Z'));
      const startedLate = [day('2026-09-17', 1), ...recent];
      render(<CostTrendComponent loading={false} error={null} dailyCosts={startedLate} range={view('30d')} />);

      expect(statValue('Lowest day')).toBe('$10.00');
      expect(statValue('Average per day')).toBe('$10.00');
      expect(statValue('Total')).toBe('$155.00');
      expect(screen.getByText('Average per day').parentElement?.textContent).toContain('Over 15 complete days');
    });

    it('counts the first day in the range as complete when the data starts before it', () => {
      renderRange(view('custom', { range: { from: '2026-09-18', to: '2026-09-25' } }));

      expect(screen.getByText('Average per day').parentElement?.textContent).toContain('Over 8 complete days');
    });

    it('draws a year as one bar a month', () => {
      renderRange(view('1y'));

      expect(screen.getByText('Monthly cost')).toBeTruthy();
      fireEvent.keyDown(screen.getByRole('group', { name: /cost per month/i }), { key: 'ArrowLeft' });
      expect(screen.getByRole('status').textContent).toBe('October 2026: $24.00, not complete (3 of 31 days measured)');
      fireEvent.keyDown(screen.getByRole('group', { name: /cost per month/i }), { key: 'Home' });
      expect(screen.getByRole('status').textContent).toBe('October 2025: no cost data');
    });
  });

  it('shows loading, error and empty states', () => {
    const { rerender } = render(<CostTrendComponent loading={true} error={null} dailyCosts={null} />);
    expect(screen.getByText('Loading daily costs…')).toBeTruthy();

    rerender(<CostTrendComponent loading={false} error="proxy returned 502" dailyCosts={null} />);
    expect(screen.getByRole('alert').textContent).toContain('proxy returned 502');

    rerender(<CostTrendComponent loading={false} error={null} dailyCosts={[]} />);
    expect(screen.getByText('No daily cost data yet.')).toBeTruthy();
  });
});
