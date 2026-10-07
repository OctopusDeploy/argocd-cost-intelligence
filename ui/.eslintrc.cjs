// ESLint 8 flat-config predecessor, matching the installed eslint ^8 and @typescript-eslint ^6.
// The bundle runs in the browser inside Argo CD's UI; tests and the test setup also run in Node.
// React APIs added after 16.14 (React 17 added none that matter here).
const NEWER_THAN_REACT_16 = [
  'use',
  'useActionState',
  'useDeferredValue',
  'useId',
  'useInsertionEffect',
  'useOptimistic',
  'useSyncExternalStore',
  'useTransition',
  'startTransition',
];

module.exports = {
  root: true,
  env: { browser: true, es2022: true },
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 2022,
    sourceType: 'module',
    ecmaFeatures: { jsx: true },
  },
  plugins: ['@typescript-eslint', 'react-hooks'],
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:jsx-a11y/recommended',
  ],
  rules: {
    'react-hooks/rules-of-hooks': 'error',
    'react-hooks/exhaustive-deps': 'error',
    // Match tsconfig's noUnusedLocals/noUnusedParameters, which accept a leading underscore
    // as "deliberately unused".
    '@typescript-eslint/no-unused-vars': [
      'error',
      { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
    ],
    // The bundle runs on whatever React Argo CD exposes as window.React: React 16.14 up to
    // Argo CD 3.4, React 19 from 3.5. The tests run on React 19 only (Testing Library needs 18+),
    // so this is what keeps APIs newer than React 16.14 out of the bundle.
    'no-restricted-imports': [
      'error',
      {
        paths: [
          {
            name: 'react',
            importNames: NEWER_THAN_REACT_16,
            message: 'Not in React 16.14, which Argo CD 3.4 provides.',
          },
          { name: 'react-dom/client', message: 'Not in React 16, which Argo CD 3.4 provides.' },
          {
            name: 'react/jsx-runtime',
            message: 'Argo CD 3.4 exposes no ReactJSXRuntime global; keep tsconfig "jsx": "react".',
          },
        ],
      },
    ],
    'no-restricted-properties': [
      'error',
      ...NEWER_THAN_REACT_16.map(property => ({
        object: 'React',
        property,
        message: 'Not in React 16.14, which Argo CD 3.4 provides.',
      })),
    ],
  },
  ignorePatterns: ['dist/', 'node_modules/', '*.d.ts'],
  overrides: [
    {
      files: ['**/*.test.ts', '**/*.test.tsx', 'src/test/**'],
      env: { browser: true, node: true },
      // Tests are not bundled and run on the React installed for development.
      rules: { 'no-restricted-imports': 'off', 'no-restricted-properties': 'off' },
    },
  ],
};
