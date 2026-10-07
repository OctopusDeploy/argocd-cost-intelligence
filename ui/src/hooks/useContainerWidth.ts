import { useCallback, useRef, useState } from 'react';

// useContainerWidth tracks an element's rendered width, so a view can lay itself out for the space
// it actually has rather than for the window. Attach the returned callback as the element's `ref`.
//
// It is a callback ref rather than a ref object so that measuring starts whenever the element
// mounts, including after a loading or error state that rendered something else first. It measures
// during commit, before paint, so nothing flashes at the fallback width.
//
// `fallback` is the width reported until a measurement arrives, and wherever there is no layout to
// measure (jsdom reports 0).
export function useContainerWidth<T extends HTMLElement>(fallback: number): [(element: T | null) => void, number] {
  const [width, setWidth] = useState(fallback);
  const observer = useRef<ResizeObserver | null>(null);

  const ref = useCallback((element: T | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!element) return;

    // The border box, the same measure in both places: contentRect would leave out padding.
    const measure = () => {
      const measured = Math.round(element.getBoundingClientRect().width);
      if (measured > 0) setWidth(measured);
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    observer.current = new ResizeObserver(measure);
    observer.current.observe(element);
  }, []);

  return [ref, width];
}
