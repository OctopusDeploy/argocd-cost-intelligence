import React from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ControllerCostTable } from './ControllerCostTable';
import { PodCost } from '../utils/api';

const pod = (name: string): PodCost => ({
  name,
  namespace: 'shop',
  controller: 'web',
  controllerKind: 'Deployment',
  cpuCost: 0.0435,
  ramCost: 0.0435,
  totalCost: 0.087,
});

describe('ControllerCostTable', () => {
  it('rounds the cells so they add up to the total beside them, and the pods to their controller', () => {
    render(
      <ControllerCostTable
        groups={[
          {
            key: 'Deployment/web',
            kind: 'Deployment',
            name: 'web',
            pods: [pod('web-1'), pod('web-2')],
            cpuCost: 0.087,
            ramCost: 0.087,
            totalCost: 0.174,
            share: 1,
          },
        ]}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: 'Pods of web' }));

    // Each rounded on its own, CPU and memory would show as $0.09 beside a total of $0.17, and so
    // would each pod.
    const amounts = screen
      .getAllByRole('row')
      .slice(1)
      .map(row => within(row).queryAllByRole('cell').map(cell => cell.textContent).filter(text => text?.startsWith('$')));
    expect(amounts).toEqual([
      ['$0.09', '$0.08', '$0.17'],
      ['$0.05', '$0.04', '$0.09'],
      ['$0.04', '$0.04', '$0.08'],
    ]);
  });
});
