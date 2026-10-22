# Cost Intelligence UI plugin

The React / TypeScript plugin that shows cost data inside the Argo CD UI. It registers two extensions (`src/index.tsx`):

- an **application tab**, "Cost Intelligence" (`CostPanel`: Actual costs / Resources / Trends)
- a **system-level page** at `/cost-intelligence` (`NamespaceDashboard`: Namespaces / Idle spend / Chargeback)

Installing it is part of the Helm chart; see [installation.md](../docs/installation.md). This file is for working on the plugin. The conventions (design tokens, layout, accessibility, testing rules) are in [CLAUDE.md](../CLAUDE.md#ui-plugin); read them before changing a component.

## Commands

Node.js 22 (the version `Dockerfile` builds with).

```bash
cd ui
npm install
npm run lint        # ESLint 8 + @typescript-eslint 6
npm run typecheck   # tsc --noEmit (includes tests)
npm test            # Vitest + jsdom + Testing Library
npm run build       # webpack 5 production build -> dist/index.js
npm run dev         # webpack --watch, development mode
npm start           # webpack-dev-server on :3000
```

A change is done when lint, typecheck, test and build all pass and `find src -name '*.d.ts'` prints nothing.

## How Argo CD loads it

- Argo CD does not load a plugin by URL. `argocd-server` serves every file in `/tmp/extensions` as `/extensions.js`, and the bundle calls `window.extensionsAPI.registerAppViewExtension` / `registerSystemLevelExtension`.
- With the chart, an init container added to `argocd-server` downloads `index.js` from the chart's UI service (`ui/Dockerfile`: `http-server` on port 3000) into that directory. The chart's post-install hook does this (`ui.autoRegister`).
- React is not bundled. webpack maps `react` / `react-dom` to the `window.React` / `window.ReactDOM` that Argo CD sets: React 16.14 on Argo CD 3.4, React 19 on 3.5. Use only APIs both have; ESLint rejects the newer ones.
- The system-level extension is registered inside `setTimeout(0)` on purpose; the comment in `src/index.tsx` explains why.
- API calls go to `/extensions/cost-intel/...`, Argo CD's extension proxy (`DEFAULT_BASE_URL` in `src/utils/api.ts`). There is no other endpoint to configure. App calls send the `Argocd-Application-Name` / `Argocd-Project-Name` headers; cluster-wide calls send the headers of the application named by `/config`'s `clusterScopeApp`. See the [authorization model](../docs/architecture.md#authorization).

## Try a build in a running Argo CD

`argocd-server` re-reads `/tmp/extensions` on every request, so a new bundle can replace the old one without a restart (lost when the pod restarts). The commands are in [CLAUDE.md](../CLAUDE.md#trying-a-build-in-a-running-argo-cd). `make helm-deploy` builds the images and deploys the whole chart to a local k3d cluster; see [development.md](../docs/development.md).

## Source layout

```
ui/src/
├── index.tsx        # registers the two Argo CD extensions
├── plugin.tsx       # application tab: reads the Application, renders CostPanel
├── components/      # views and cards (CostPanel, CostActual, CostTrend, ResourcesTab,
│                    #   NamespaceDashboard, ChargebackView, RightsizingCard, ...)
├── hooks/           # data fetching (useCostData, useNamespaceData, ...), useUrlState, useTheme
├── utils/           # api.ts (client and types), format.ts, layout.ts, forecast, ranges, ...
├── theme/           # tokens.ts (colours) and globalStyles.ts (focus rings, hover)
└── test/            # shared test setup and fixtures
```

Tests sit next to the code as `*.test.ts(x)`. Build configuration: `webpack.config.js`, `tsconfig.json`, `.eslintrc.cjs`, `vitest.config.ts`.

## Adding to the plugin

- Reuse the shared components and `useTokens()` colours before building new ones. Never hardcode a colour or set `outline: 'none'`.
- Every root view sets `className={PLUGIN_CLASS}`, spreads `pluginRootStyle(t)` and calls `useGlobalStyles()`.
- Put view state in the URL with `useUrlState`, and refresh data in the background every 30 s like the existing hooks.
- A new endpoint goes into `src/utils/api.ts` and needs the matching route in `pkg/server/api.go`.
