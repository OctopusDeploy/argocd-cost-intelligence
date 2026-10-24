import React from 'react';
import { fireEvent, render, within } from '@testing-library/react';
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

  it('rounds the cells so they add up to the total beside them, and the controllers to their app', () => {
    const controller = (name: string) => ({
      key: `Deployment/${name}`,
      kind: 'Deployment',
      name,
      pods: [],
      cpuCost: 0.0435,
      ramCost: 0.0435,
      totalCost: 0.087,
      share: 0.5,
    });
    const shop = {
      ...app('shop', 'web'),
      controllers: [controller('web'), controller('api')],
      cpuCost: 0.087,
      ramCost: 0.087,
      totalCost: 0.174,
    };
    const { getAllByRole, getByRole } = render(<NamespaceAppsTable apps={[shop]} totalCost={0.174} />);
    fireEvent.click(getByRole('button', { name: 'Controllers of web' }));

    // Each rounded on its own, CPU and memory would show as $0.09 beside a total of $0.17, and so
    // would each controller.
    const amounts = getAllByRole('row')
      .slice(1)
      .map(row => within(row).queryAllByRole('cell').map(cell => cell.textContent).filter(text => text?.startsWith('$')));
    expect(amounts).toEqual([
      ['$0.09', '$0.08', '$0.17'],
      ['$0.05', '$0.04', '$0.09'],
      ['$0.04', '$0.04', '$0.08'],
    ]);
  });
});
