const DAY_MS = 86_400_000;
const HOUR_MS = 3_600_000;
const MINUTE_MS = 60_000;

// Cost data older than this is flagged as stale in the panel header.
export const STALE_AFTER_DAYS = 7;

const moneyFormatters = new Map<string, Intl.NumberFormat>();

// formatMoney renders an amount with grouping and a fixed number of decimals,
// e.g. formatMoney(3525.9) === '$3,525.90'.
export function formatMoney(amount: number, currency = 'USD', fractionDigits = 2): string {
  const key = `${currency}:${fractionDigits}`;
  let formatter = moneyFormatters.get(key);
  if (!formatter) {
    formatter = new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    });
    moneyFormatters.set(key, formatter);
  }
  return formatter.format(amount);
}

// formatEstimate renders a projected amount: whole dollars from $100, where cents would claim a
// precision a forecast does not have. Pass the headline figure as reference so the parts of an
// estimate are rounded like the whole.
export function formatEstimate(amount: number, reference = amount): string {
  return formatMoney(amount, 'USD', Math.abs(reference) >= 100 ? 0 : 2);
}

// formatSignedMoney is formatMoney with an explicit sign, e.g. "+$41.00" or "−$12.50". An amount that
// rounds to zero cents has no sign.
export function formatSignedMoney(amount: number, currency = 'USD'): string {
  const formatted = formatMoney(Math.abs(amount), currency);
  if (Math.abs(amount) < 0.005) return formatted;
  return `${amount < 0 ? '−' : '+'}${formatted}`;
}

const relativeTime = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

// formatRelativeTime describes `iso` relative to `now`, e.g. "13 days ago" or "yesterday".
// It returns an empty string when the timestamp cannot be parsed.
export function formatRelativeTime(iso: string, now: Date = new Date()): string {
  const time = new Date(iso).getTime();
  if (Number.isNaN(time)) return '';
  const diff = time - now.getTime();
  const abs = Math.abs(diff);
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['day', DAY_MS],
    ['hour', HOUR_MS],
    ['minute', MINUTE_MS],
  ];
  for (const [unit, ms] of units) {
    if (abs >= ms) return relativeTime.format(Math.sign(diff) * Math.floor(abs / ms), unit);
  }
  return 'just now';
}

// isStale reports whether `iso` is at least `afterDays` old. An unparseable timestamp is never stale.
export function isStale(iso: string, now: Date = new Date(), afterDays = STALE_AFTER_DAYS): boolean {
  const time = new Date(iso).getTime();
  return !Number.isNaN(time) && now.getTime() - time >= afterDays * DAY_MS;
}

// A measurement covering less than this is partial. OpenCost's data ends at its last resolution step
// before the query, so a full day comes back as about 23.9 hours, not 24.
const FULL_DAY_HOURS = 23;

// isPartialMeasurement reports whether OpenCost had less than a day of data for a measurement, as for a
// namespace created that day. Unknown or zero periods are not flagged: there is nothing to qualify.
export function isPartialMeasurement(hours: number | undefined): boolean {
  return hours !== undefined && hours > 0 && hours < FULL_DAY_HOURS;
}

// formatHours renders a period of data as whole hours, or minutes under an hour, e.g. "6 hours".
export function formatHours(hours: number): string {
  if (hours < 1) return pluralize(Math.max(1, Math.round(hours * 60)), 'minute');
  return pluralize(Math.round(hours), 'hour');
}

const shortDay = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
const longDay = new Intl.DateTimeFormat('en-US', { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });

// formatDay formats a calendar day ("2026-09-02") as "Sep 2", or "Wed, Sep 2" with the weekday.
// Days are interpreted in UTC so they never shift by one in the viewer's time zone.
export function formatDay(day: string, withWeekday = false): string {
  return (withWeekday ? longDay : shortDay).format(new Date(`${day}T00:00:00Z`));
}

const datedDay = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' });

// formatDayIn formats a day for a sentence about a span of days: "Sep 2", or "Sep 2, 2026" when the
// span crosses a year, where the month and day alone could be either year.
export function formatDayIn(day: string, span: { from: string; to: string }): string {
  if (span.from.slice(0, 4) === span.to.slice(0, 4)) return formatDay(day);
  return datedDay.format(new Date(`${day}T00:00:00Z`));
}

const shortMonth = new Intl.DateTimeFormat('en-US', { month: 'short', timeZone: 'UTC' });

// formatMonth names the month a calendar day falls in, e.g. "Oct" for "2026-10-01", in UTC.
export function formatMonth(day: string): string {
  return shortMonth.format(new Date(`${day}T00:00:00Z`));
}

// formatPercent renders a 0–1 fraction as a percentage: whole numbers from 10%, one decimal below.
export function formatPercent(fraction: number): string {
  const percent = fraction * 100;
  if (percent > 0 && percent < 0.1) return '<0.1%';
  return `${percent > 0 && percent < 10 ? percent.toFixed(1) : Math.round(percent)}%`;
}

// formatSignedPercent is formatPercent with an explicit sign, e.g. "−86%" or "+12%". A change below
// 0.1% has no sign: "−<0.1%" reads badly, so callers show its direction some other way.
export function formatSignedPercent(fraction: number): string {
  const unsigned = formatPercent(Math.abs(fraction));
  if (fraction === 0 || unsigned.startsWith('<')) return unsigned;
  return `${fraction < 0 ? '−' : '+'}${unsigned}`;
}

export const pluralize = (count: number, noun: string) => `${count} ${noun}${count === 1 ? '' : 's'}`;

// formatCores renders CPU cores, switching to Kubernetes millicores below a tenth of a core.
export function formatCores(cores: number): string {
  return cores < 0.1 ? formatMillicores(cores) : `${cores.toFixed(2)} cores`;
}

export function formatMillicores(cores: number): string {
  return `${Math.round(cores * 1000)}m`;
}

// formatBytes renders memory in binary units, labelled the way Kubernetes users read them.
export function formatBytes(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
  if (bytes >= 1024 ** 2) return `${Math.round(bytes / 1024 ** 2)} MB`;
  return `${Math.round(bytes / 1024)} KB`;
}

export function formatCarbon(kg: number): string {
  if (kg < 0.001) return '< 1 g CO₂e';
  if (kg < 1) return `${Math.round(kg * 1000)} g CO₂e`;
  if (kg < 1000) return `${kg.toFixed(1)} kg CO₂e`;
  return `${(kg / 1000).toFixed(2)} t CO₂e`;
}

// An average passenger car emits about 0.21 kg CO₂e per km.
const CAR_KG_PER_KM = 0.21;

export function formatKmDriven(kg: number): string {
  const km = kg / CAR_KG_PER_KM;
  if (km < 1) return 'less than 1 km';
  if (km < 1000) return `${Math.round(km)} km`;
  return `${(km / 1000).toFixed(1)}k km`;
}
