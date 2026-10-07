import React from 'react';
import { fireEvent, render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NamespaceAppsTable } from './NamespaceAppsTable';
import { AppCost } from '../utils/namespaceApps';

const app = (namespace: string, controller: string): AppCost => ({
  name: 'web',
  namespace,
  visible: true,
  controllers: [
    {
      key: `Deployment/${controller}`,
      kind: 'Deployment',
      name: controller,
      pods: [],
      cpuCost: 1,
      ramCost: 1,
      totalCost: 2,
      share: 1,
    },
  ],
  podCount: 0,
  cpuCost: 1,
  ramCost: 1,
  totalCost: 2,
  share: 0.5,
  measuredAt: null,
});

describe('NamespaceAppsTable', () => {
  it('expands one of two same-named applications from different namespaces', () => {
    const { getAllByRole } = render(
      <NamespaceAppsTable apps={[app('team-a', 'web-a'), app('team-b', 'web-b')]} totalCost={4} />
    );

    const toggles = getAllByRole('button', { name: 'Controllers of web' });
    expect(toggles).toHaveLength(2);
    fireEvent.click(toggles[0]);

    expect(toggles.map(toggle => toggle.getAttribute('aria-expanded'))).toEqual(['true', 'false']);
  });
});
