import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { OrphanedPodsNotice } from './OrphanedPodsNotice';
import { useOrphanedPods } from '../hooks/useOrphanedPods';
import { OrphanedPod } from '../utils/api';

vi.mock('../hooks/useOrphanedPods', () => ({ useOrphanedPods: vi.fn() }));

const pod = (name: string): OrphanedPod => ({
  name,
  namespace: 'shop',
  controller: 'batch',
  controllerKind: 'Job',
  cpuCost: 0,
  ramCost: 0,
  storageCost: 0.125,
  totalCost: 0.125,
});

describe('OrphanedPodsNotice', () => {
  it('totals the pods as they are listed', () => {
    // Each pod shows as $0.13, so the exact $0.25 would not be their sum.
    vi.mocked(useOrphanedPods).mockReturnValue({ pods: [pod('stuck-0'), pod('stuck-1')], loading: false });
    render(<OrphanedPodsNotice namespace="shop" />);

    expect(screen.getByRole('button', { name: /2 orphaned pods in shop, \$0\.26\/mo/ })).toBeTruthy();
  });
});
