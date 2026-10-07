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
