import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach } from 'vitest';

// Testing Library only unmounts automatically when test globals are enabled; they are not here.
afterEach(() => {
  cleanup();
});

// Views keep state in the query string (see useUrlState), and jsdom carries one location through a
// whole file. Starting every test from a bare URL keeps them independent.
beforeEach(() => {
  window.history.replaceState(null, '', '/');
});
