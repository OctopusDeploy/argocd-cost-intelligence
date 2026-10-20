import React from 'react';
import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApplicationDetailsTabProps, ArgoCDCostIntelligenceTab } from './plugin';

vi.mock('./components/CostPanel', () => ({ CostPanel: () => 'cost views' }));

function application(destination: NonNullable<ApplicationDetailsTabProps['application']['spec']>['destination']) {
  return { metadata: { name: 'web', namespace: 'argocd' }, spec: { destination } };
}

describe('ArgoCDCostIntelligenceTab', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('shows the cost views of an application that deploys to this cluster', () => {
    render(<ArgoCDCostIntelligenceTab application={application({ server: 'https://kubernetes.default.svc', namespace: 'shop' })} />);

    expect(document.body.textContent).toContain('cost views');
  });

  it('explains, without asking for costs, that an application on another cluster is not measured', () => {
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);

    render(<ArgoCDCostIntelligenceTab application={application({ name: 'prod-eu', server: 'https://10.4.0.12:6443', namespace: 'shop' })} />);

    expect(screen.getByRole('heading', { name: 'Deploys to another cluster' })).not.toBeNull();
    expect(document.body.textContent).toContain('prod-eu');
    expect(screen.getByRole('banner').textContent).toContain('Not measured');
    expect(document.body.textContent).not.toContain('cost views');
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('names another cluster by its server when it has no name', () => {
    render(<ArgoCDCostIntelligenceTab application={application({ server: 'https://10.4.0.12:6443', namespace: 'shop' })} />);

    expect(document.body.textContent).toContain('https://10.4.0.12:6443');
  });

  it('links the single-cluster limit in a new tab', () => {
    render(<ArgoCDCostIntelligenceTab application={application({ name: 'prod-eu' })} />);

    const link = screen.getByRole('link', { name: 'Single-cluster limit (opens in a new tab)' });
    expect(link.getAttribute('href')).toContain('docs/architecture.md#known-limitations');
    expect(link.getAttribute('rel')).toBe('noopener noreferrer');
  });
});
