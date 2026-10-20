import { describe, expect, it } from 'vitest';
import { LOCAL_NAME, LOCAL_SERVER, otherCluster } from './destination';

describe('otherCluster', () => {
  it('takes this cluster by its server, its name or neither', () => {
    expect(otherCluster({ server: LOCAL_SERVER, namespace: 'shop' })).toBeNull();
    expect(otherCluster({ name: LOCAL_NAME })).toBeNull();
    expect(otherCluster({ server: LOCAL_SERVER, name: LOCAL_NAME })).toBeNull();
    expect(otherCluster({})).toBeNull();
    expect(otherCluster(undefined)).toBeNull();
  });

  it('names another cluster by its name, else its server', () => {
    expect(otherCluster({ name: 'prod-eu', server: 'https://10.4.0.12:6443' })).toBe('prod-eu');
    expect(otherCluster({ server: 'https://10.4.0.12:6443' })).toBe('https://10.4.0.12:6443');
    expect(otherCluster({ name: 'prod-eu' })).toBe('prod-eu');
  });

  it('takes any other server or name as another cluster, as the service does', () => {
    expect(otherCluster({ server: LOCAL_SERVER, name: 'local' })).toBe('local');
    expect(otherCluster({ server: 'https://kubernetes.default.svc:443', name: LOCAL_NAME })).toBe(LOCAL_NAME);
  });
});
