import React from 'react';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useContainerWidth } from './useContainerWidth';

// jsdom has no layout and no ResizeObserver, so both are stood in for here.
let observed: Array<{ element: Element; callback: () => void; disconnected: boolean }> = [];
class FakeResizeObserver {
  private entry: { element: Element; callback: () => void; disconnected: boolean } | null = null;
  constructor(private callback: () => void) {}
  observe(element: Element) {
    this.entry = { element, callback: this.callback, disconnected: false };
    observed.push(this.entry);
  }
  disconnect() {
    if (this.entry) this.entry.disconnected = true;
  }
}

let widthOnScreen = 0;

const Probe: React.FC<{ show?: boolean }> = ({ show = true }) => {
  const [ref, width] = useContainerWidth<HTMLDivElement>(123);
  return show ? <div ref={ref}>{width}</div> : <span>{width}</span>;
};

beforeEach(() => {
  observed = [];
  widthOnScreen = 0;
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
    () => ({ width: widthOnScreen }) as DOMRect
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('useContainerWidth', () => {
  it('reports the fallback while there is nothing to measure', () => {
    render(<Probe />);

    expect(screen.getByText('123')).toBeTruthy();
  });

  it('measures the element as soon as it mounts', () => {
    widthOnScreen = 1880.4;

    render(<Probe />);

    expect(screen.getByText('1880')).toBeTruthy();
  });

  it('follows the element as it resizes', () => {
    widthOnScreen = 1200;
    render(<Probe />);

    widthOnScreen = 700;
    act(() => observed[0].callback());

    expect(screen.getByText('700')).toBeTruthy();
  });

  it('starts measuring an element that mounts after a first render without it', () => {
    widthOnScreen = 900;
    const { rerender } = render(<Probe show={false} />);
    expect(observed).toHaveLength(0);

    rerender(<Probe show />);

    expect(screen.getByText('900')).toBeTruthy();
    expect(observed).toHaveLength(1);
  });

  it('stops observing once the element is gone', () => {
    widthOnScreen = 900;
    const { unmount } = render(<Probe />);

    unmount();

    expect(observed[0].disconnected).toBe(true);
  });
});
