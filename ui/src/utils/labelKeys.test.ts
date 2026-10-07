import { describe, expect, it } from 'vitest';
import { sameLabelKey, sanitizeLabelKey, suggestLabelKeys } from './labelKeys';

describe('suggestLabelKeys', () => {
  it('puts commonly used keys first, in their usual order, then the rest alphabetically', () => {
    expect(suggestLabelKeys(['zone', 'app', 'team', 'billing', 'owner'])).toEqual(['team', 'owner', 'app', 'billing', 'zone']);
  });

  it('offers only keys that exist', () => {
    expect(suggestLabelKeys(['app'])).toEqual(['app']);
    expect(suggestLabelKeys([])).toEqual([]);
  });

  it('stops at the limit', () => {
    expect(suggestLabelKeys(['a', 'b', 'c', 'team'], 2)).toEqual(['team', 'a']);
  });

  it('lists a key once', () => {
    expect(suggestLabelKeys(['team', 'team', 'x', 'x'])).toEqual(['team', 'x']);
  });
});

describe('sanitizeLabelKey', () => {
  it('writes a key the way OpenCost reports it', () => {
    expect(sanitizeLabelKey('app.kubernetes.io/name')).toBe('app_kubernetes_io_name');
    expect(sanitizeLabelKey('cost-center')).toBe('cost_center');
    expect(sanitizeLabelKey(' team ')).toBe('team');
  });
});

describe('sameLabelKey', () => {
  it('treats two spellings OpenCost groups alike as the same key', () => {
    expect(sameLabelKey('app.kubernetes.io/name', 'app_kubernetes_io_name')).toBe(true);
    expect(sameLabelKey('app', 'app_kubernetes_io_name')).toBe(false);
  });
});
