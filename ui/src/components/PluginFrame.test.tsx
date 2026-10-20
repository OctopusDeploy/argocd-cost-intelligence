import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PluginFrame } from './PluginFrame';
import { PLUGIN_CLASS } from '../theme/globalStyles';

describe('PluginFrame', () => {
  it('names the product in the header', () => {
    render(<PluginFrame>body</PluginFrame>);

    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe('Cost Intelligence');
  });

  it('links the maker to its website in a new tab', () => {
    render(<PluginFrame>body</PluginFrame>);

    const link = screen.getByRole('link', { name: 'by Octopus Deploy (opens in a new tab)' });
    expect(link.getAttribute('href')).toBe('https://octopus.com');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toBe('noopener noreferrer');
  });

  it('puts the status in the header, before the view', () => {
    render(
      <PluginFrame status={<span>Measured 5 minutes ago</span>}>
        <p>the view</p>
      </PluginFrame>
    );

    const header = screen.getByRole('banner');
    expect(header.textContent).toContain('Measured 5 minutes ago');
    expect(header.textContent).not.toContain('the view');
  });

  // The global stylesheet's focus rings and hover tints only reach elements under the plugin class.
  it('is the plugin root, so the global stylesheet applies inside it', () => {
    const { container } = render(<PluginFrame>body</PluginFrame>);

    expect((container.firstChild as HTMLElement).classList.contains(PLUGIN_CLASS)).toBe(true);
    expect(document.getElementById('cost-intel-styles')).not.toBeNull();
  });
});
