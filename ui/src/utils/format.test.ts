import { describe, expect, it } from 'vitest';
import {
  formatBytes,
  formatCarbon,
  formatCores,
  formatDay,
  formatDayIn,
  formatEstimate,
  formatHours,
  formatKmDriven,
  formatMillicores,
  formatMoney,
  formatMonth,
  formatPercent,
  formatRelativeTime,
  formatSignedMoney,
  formatSignedPercent,
  isPartialMeasurement,
  isStale,
  pluralize,
  roundColumns,
  roundParts,
  sumRounded,
} from './format';

describe('formatMoney', () => {
  it('groups thousands and fixes two decimals', () => {
    expect(formatMoney(3525.9)).toBe('$3,525.90');
  });

  it('honours the currency and precision', () => {
    expect(formatMoney(0.5972, 'USD', 4)).toBe('$0.5972');
    expect(formatMoney(12, 'EUR')).toBe('€12.00');
    expect(formatMoney(162.75, 'USD', 0)).toBe('$163');
  });
});

describe('roundParts', () => {
  it('makes rounded parts add up to the rounded total', () => {
    // 0.09 + 0.09 would be shown against a total of 0.17.
    expect(roundParts([0.087, 0.087])).toEqual([0.09, 0.08]);
    // 11.40 + 1.45 would be shown against idle spend of 12.86.
    expect(roundParts([11.4043, 1.4536])).toEqual([11.41, 1.45]);
  });

  it('gives the leftover cents to the largest remainders', () => {
    expect(roundParts([1.004, 1.004, 1.007])).toEqual([1.01, 1, 1.01]);
    expect(roundParts([0.333, 0.333, 0.334], 1)).toEqual([0.33, 0.33, 0.34]);
  });

  it('leaves parts that already add up as they are', () => {
    expect(roundParts([11.4048, 1.5552])).toEqual([11.4, 1.56]);
    expect(roundParts([0.29, 0.71])).toEqual([0.29, 0.71]);
    expect(roundParts([])).toEqual([]);
  });

  it('adds up to a total that is itself rounded', () => {
    // A row of 12.9649 shown as 12.97 after its own parent's rounding, where 11.40 + 1.56 would be less.
    expect(roundParts([11.4048, 1.5601], 12.97)).toEqual([11.41, 1.56]);
  });

  it('rounds each part on its own when they do not add up to the total', () => {
    expect(roundParts([5.7, 0.78], 6.4368)).toEqual([5.7, 0.78]);
    // Each part could take a cent here, but the parts are three cents off the total.
    expect(roundParts([1, 1, 1, 1, 1], 5.03)).toEqual([1, 1, 1, 1, 1]);
    // A total a whole cent above its parts holds something they don't.
    expect(roundParts([0.5], 0.51)).toEqual([0.5]);
  });

  it('never shows an exact zero as a cent', () => {
    expect(roundParts([0, 0], 0.01)).toEqual([0, 0]);
    expect(roundParts([0.0099, 0, 0], 0.0199)).toEqual([0.01, 0, 0]);
  });

  it('shows float noise below zero as zero, without a sign', () => {
    const [, , other] = roundParts([1.0548101881246619, 4.723641146602001, -8.881784197001252e-16], 5.778451334726663);
    expect(formatMoney(other)).toBe('$0.00');
  });

  it('rounds negative halves away from zero, as formatMoney does', () => {
    expect(roundParts([-0.125])).toEqual([-0.13]);
    expect(formatMoney(roundParts([-1.005])[0])).toBe(formatMoney(-1.005));
  });

  it('rounds to whole units for whole-dollar estimates', () => {
    expect(roundParts([100.6, 50.6], 151.2, 0)).toEqual([101, 50]);
  });

  it('handles negative parts of a signed change', () => {
    // 2.01 and 0.00 would be shown against a change of 2.00.
    expect(roundParts([2.006, -0.003])).toEqual([2, 0]);
  });
});

describe('roundColumns', () => {
  it('keeps the columns within a total that holds more than they do', () => {
    // CPU 4.995 and memory 4.995 would both show as 5.00 beside a total of 10.00 that also holds
    // 0.008 of storage.
    expect(roundColumns([4.995, 4.995], 9.998)).toEqual([5, 4.99]);
    expect(roundColumns([1.2, 3.4], 6)).toEqual([1.2, 3.4]);
  });

  it('adds up to a total that is itself rounded', () => {
    // A controller of 0.174 shown as 0.17 after its app's rounding: 0.09 and 0.09 would be more.
    expect(roundColumns([0.087, 0.087], 0.174, 0.17)).toEqual([0.09, 0.08]);
  });
});

describe('sumRounded', () => {
  it('adds the amounts as they are shown', () => {
    // Rows shown as 12.96, 12.84 and 1.69 add up to 27.49, not the exact 27.48.
    expect(sumRounded([12.9551, 12.8447, 1.6851])).toBe(27.49);
    expect(sumRounded([150.4, 20.4], 0)).toBe(170);
    expect(sumRounded([-0.125, -0.125])).toBe(-0.26);
  });
});

describe('formatRelativeTime', () => {
  const now = new Date('2026-09-15T12:00:00Z');

  it.each([
    ['2026-09-15T11:59:30Z', 'just now'],
    ['2026-09-15T11:15:00Z', '45 minutes ago'],
    ['2026-09-15T01:00:00Z', '11 hours ago'],
    ['2026-09-14T10:00:00Z', 'yesterday'],
    ['2026-09-01T14:57:05Z', '13 days ago'],
  ])('%s is %s', (iso, want) => {
    expect(formatRelativeTime(iso, now)).toBe(want);
  });

  it('returns an empty string for an unparseable timestamp', () => {
    expect(formatRelativeTime('not a date', now)).toBe('');
  });
});

describe('isStale', () => {
  const now = new Date('2026-09-15T12:00:00Z');

  it('is fresh just under the threshold and stale at it', () => {
    expect(isStale('2026-09-08T12:00:01Z', now)).toBe(false);
    expect(isStale('2026-09-08T12:00:00Z', now)).toBe(true);
  });

  it('accepts a custom threshold', () => {
    expect(isStale('2026-09-14T12:00:00Z', now, 1)).toBe(true);
  });

  it('never flags an unparseable timestamp', () => {
    expect(isStale('garbage', now)).toBe(false);
  });
});

describe('isPartialMeasurement', () => {
  it('flags less than a day of data', () => {
    expect(isPartialMeasurement(6)).toBe(true);
    expect(isPartialMeasurement(0.5)).toBe(true);
  });

  it('treats OpenCost\'s usual 23.9 hours as a full day', () => {
    expect(isPartialMeasurement(23.9)).toBe(false);
    expect(isPartialMeasurement(24)).toBe(false);
  });

  it('never flags an unknown or empty period', () => {
    expect(isPartialMeasurement(undefined)).toBe(false);
    expect(isPartialMeasurement(0)).toBe(false);
  });
});

describe('formatHours', () => {
  it('rounds to whole hours', () => {
    expect(formatHours(6.4)).toBe('6 hours');
    expect(formatHours(1.2)).toBe('1 hour');
  });

  it('switches to minutes under an hour, never showing zero', () => {
    expect(formatHours(0.75)).toBe('45 minutes');
    expect(formatHours(0.001)).toBe('1 minute');
  });
});

describe('formatDay', () => {
  it('formats calendar days in UTC', () => {
    expect(formatDay('2026-09-02')).toBe('Sep 2');
    expect(formatDay('2026-09-02', true)).toBe('Wed, Sep 2');
  });
});

describe('formatDayIn', () => {
  it('names the year only when the span crosses one', () => {
    expect(formatDayIn('2026-09-02', { from: '2026-01-01', to: '2026-12-31' })).toBe('Sep 2');
    expect(formatDayIn('2026-09-02', { from: '2025-10-04', to: '2026-10-03' })).toBe('Sep 2, 2026');
  });
});

describe('formatEstimate', () => {
  it('drops the cents from $100, following the reference figure', () => {
    expect(formatEstimate(447.36)).toBe('$447');
    expect(formatEstimate(99.5)).toBe('$99.50');
    expect(formatEstimate(23.4, 447.36)).toBe('$23');
  });
});

describe('formatMonth', () => {
  it('names the month of a calendar day in UTC', () => {
    expect(formatMonth('2026-10-01')).toBe('Oct');
    expect(formatMonth('2026-09-30')).toBe('Sep');
  });
});

describe('formatPercent', () => {
  it.each([
    [1, '100%'],
    [0.5714, '57%'],
    [0.1, '10%'],
    [0.088, '8.8%'],
    [0.0004, '<0.1%'],
    [0, '0%'],
  ])('%s is %s', (fraction, want) => {
    expect(formatPercent(fraction)).toBe(want);
  });
});

describe('formatSignedPercent', () => {
  it.each([
    [-0.8615, '−86%'],
    [0.12, '+12%'],
    [-0.05, '−5.0%'],
    [0.001, '+0.1%'],
    [-0.0004, '<0.1%'],
    [0.0004, '<0.1%'],
    [0, '0%'],
  ])('%s is %s', (fraction, want) => {
    expect(formatSignedPercent(fraction)).toBe(want);
  });
});

describe('formatSignedMoney', () => {
  it.each([
    [41, '+$41.00'],
    [-1234.5, '−$1,234.50'],
    [0, '$0.00'],
    [-0.004, '$0.00'],
  ])('%s is %s', (amount, want) => {
    expect(formatSignedMoney(amount)).toBe(want);
  });
});

describe('pluralize', () => {
  it('adds an s except for one', () => {
    expect(pluralize(1, 'resource')).toBe('1 resource');
    expect(pluralize(3, 'resource')).toBe('3 resources');
  });
});

describe('resource units', () => {
  it('formats cores, switching to millicores below a tenth', () => {
    expect(formatCores(0.4)).toBe('0.40 cores');
    expect(formatCores(2)).toBe('2.00 cores');
    expect(formatCores(0.05)).toBe('50m');
    expect(formatMillicores(0.5)).toBe('500m');
  });

  it.each([
    [1073741824, '1.0 GB'],
    [536870912, '512 MB'],
    [375809638, '358 MB'],
    [2048, '2 KB'],
  ])('%s bytes is %s', (bytes, want) => {
    expect(formatBytes(bytes)).toBe(want);
  });
});

describe('carbon', () => {
  it.each([
    [8.79, '8.8 kg CO₂e'],
    [0.5, '500 g CO₂e'],
    [0.0004, '< 1 g CO₂e'],
    [2500, '2.50 t CO₂e'],
  ])('%s kg is %s', (kg, want) => {
    expect(formatCarbon(kg)).toBe(want);
  });

  it.each([
    [8.79, '42 km'],
    [0.1, 'less than 1 km'],
    [300, '1.4k km'],
  ])('%s kg is about %s driven', (kg, want) => {
    expect(formatKmDriven(kg)).toBe(want);
  });
});
