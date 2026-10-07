// How many label keys the chargeback view offers as one-click chips. The input suggests every key.
export const SUGGESTION_LIMIT = 6;

// Offered when the backend cannot say which label keys exist (OpenCost older than 1.120.3, or the
// request failed).
export const FALLBACK_LABEL_KEYS = ['team', 'app', 'env', 'tier', 'owner', 'cost-center'];

// Keys teams commonly charge back by, most useful first, in the sanitized form OpenCost reports.
const PREFERRED_KEYS = [
  'team',
  'owner',
  'department',
  'product',
  'cost_center',
  'environment',
  'env',
  'app_kubernetes_io_part_of',
  'app_kubernetes_io_name',
  'app',
  'tier',
  'component',
];

// sanitizeLabelKey writes a label key the way OpenCost reports it: app.kubernetes.io/name becomes
// app_kubernetes_io_name. OpenCost groups by either spelling, so two keys that sanitize alike are
// the same grouping.
export function sanitizeLabelKey(key: string): string {
  return key.trim().replace(/[^a-zA-Z0-9_]/g, '_');
}

export function sameLabelKey(a: string, b: string): boolean {
  return sanitizeLabelKey(a) === sanitizeLabelKey(b);
}

// suggestLabelKeys picks the chips from the keys that exist: the commonly used ones first, in
// PREFERRED_KEYS order, then the rest alphabetically, up to limit.
export function suggestLabelKeys(keys: string[], limit = SUGGESTION_LIMIT): string[] {
  const present = new Set(keys);
  const preferred = PREFERRED_KEYS.filter(key => present.has(key));
  const rest = [...present].filter(key => !PREFERRED_KEYS.includes(key)).sort();
  return [...preferred, ...rest].slice(0, limit);
}
