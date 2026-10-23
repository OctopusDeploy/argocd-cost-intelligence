# Cost Intelligence UI plugin

The React / TypeScript plugin that shows cost data inside the Argo CD UI. It registers two extensions (`src/index.tsx`):

- an **application tab**, "Cost Intelligence" (`CostPanel`: Actual costs / Resources / Trends)
- a **system-level page** at `/cost-intelligence` (`NamespaceDashboard`: Namespaces / Idle spend / Chargeback)

Installing it is part of the Helm chart; see [installation.md](../docs/installation.md). This file is for working on the plugin. Read the [conventions](#conventions) below and the testing rules in [CONTRIBUTING.md](../CONTRIBUTING.md#writing-tests) before changing a component.

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
- React is not bundled. webpack maps `react` / `react-dom` to the `window.React` / `window.ReactDOM` that Argo CD sets: React 16.14 on Argo CD 3.4, React 19 on 3.5. Use only APIs React 16.14 has (no `useSyncExternalStore`, `useId`, `useTransition` or `react-dom/client`); ESLint rejects them. Keep `"jsx": "react"` in `tsconfig.json`.
- The system-level extension is registered inside `setTimeout(0)` on purpose; the comment in `src/index.tsx` explains why.
- API calls go to `/extensions/cost-intel/...`, Argo CD's extension proxy (`DEFAULT_BASE_URL` in `src/utils/api.ts`). There is no other endpoint to configure. App calls send the `Argocd-Application-Name` / `Argocd-Project-Name` headers; cluster-wide calls send the headers of the application named by `/config`'s `clusterScopeApp`. See the [authorization model](../docs/architecture.md#authorization).

## Try a build in a running Argo CD

`argocd-server` re-reads `/tmp/extensions` on every request, so a new bundle can replace the old one without a restart (lost when the pod restarts). From the repository root:

```bash
cd ui && npm run build && cd ..
POD=$(kubectl get pods -n argocd -l app.kubernetes.io/name=argocd-server -o name | head -1)
CONTAINER=$(kubectl get -n argocd "$POD" -o jsonpath='{.spec.containers[0].name}')
kubectl exec -i -n argocd "$POD" -c "$CONTAINER" -- sh -c \
  'cat > /tmp/extensions/.new.js && mv -f /tmp/extensions/.new.js /tmp/extensions/extension-cost-intel.js' < ui/dist/index.js
```

`make helm-deploy` builds the images and deploys the whole chart to a local k3d cluster; see [development.md](../docs/development.md#running-it-in-argo-cd).

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

## Conventions

### Design system

- **Octopus Deploy look.** The plugin is a branded panel inside Argo CD's page ("Cost Intelligence by Octopus Deploy"), in Octopus's palette and its Inter font, with bordered cards, an icon tile above each card title, sentence-case labels and outlined buttons. Argo CD's own page around it is unchanged. Inter is bundled (`src/theme/font.ts`, inlined by webpack) because Argo CD serves the extension as one JS file and air-gapped clusters can't reach a font host.
- **Colours** come from `src/theme/tokens.ts` (`useTokens()`) only; never hardcode one. `cardStyle(t)` is the standard container, `series` the chart palette.
- **Reuse shared components** before building: `PluginFrame` (root and branded header), `CardHeader` / `IconTile`, `TabBar`, `StatStrip`, `Notice` / `ErrorNotice`, `ReloadButton`, `MeasuredLabel`, `Share`, `AdviceCard`, `Chip`, `levelIcon` / `LEVEL_LABELS`, `icons.tsx`. `BrandMark` is the octoicon from Octopus's official logo pack (octopus.com/company/brand); keep its path as it is.
- **Formatting** goes through `src/utils/format.ts` (`formatMoney`, `formatPercent`, ...). Never `toFixed` a currency by hand.
- The same concept uses the same words and icon on every page, and columns beside a total add up to it.

### Argo CD's CSS

Argo CD ships `outline: none !important` on buttons, links, spans and inputs, so inline focus styles do nothing. `src/theme/globalStyles.ts` injects one stylesheet instead (focus rings, button hover tints). Every root view renders inside `PluginFrame`, which sets `className={PLUGIN_CLASS}`, spreads `pluginRootStyle(t)` and calls `useGlobalStyles()`. Never set `outline: 'none'`. Scope every rule to `.cost-intel` (a test enforces this).

### Layout

The plugin fills the width Argo CD gives it and adapts to **its own width, not the window's** (`useContainerWidth`, thresholds in `src/utils/layout.ts`). Tables scroll inside their own wrapper below their `minWidth`; nothing may overflow the page horizontally. Choose fallbacks so unmeasured views (and jsdom) get the ordinary layout.

### Accessibility

- Clickable things are `<button>` or `<a>`, never `<span onClick>`. Toggles use `aria-pressed`; disclosures use `aria-expanded` + `aria-controls`.
- Tables are CSS grids with `role="table"` / `row` / `columnheader` / `cell` in an `overflowX: 'auto'` wrapper.
- Inputs have a `<label htmlFor>`. Result changes are announced through a visually hidden `role="status"` region.
- Charts are keyboard-readable, start the y-axis at zero, resize with their container and offer a table view.

### Interaction

- No submit buttons for filters: text queries after a 400ms debounce (Enter queries at once); chips, selects and tabs apply immediately.
- View state lives in the URL via `src/hooks/useUrlState.ts` (`costTab`, `costNs`, `costLabel`, `costScope`, `costRange`, `costFrom`, `costTo`). `push` for navigation, `replace` for refinements. Idle spend and Chargeback share `costScope`. Never touch Argo CD's own parameters.
- Pages refresh every 30s in the background without a spinner and keep the last good data if a refresh fails.
- The theme is read from Argo CD's `view_preferences` localStorage key by one shared poller (`src/hooks/useTheme.ts`). Do not add per-component timers.

### API calls

A new endpoint goes into `src/utils/api.ts` and needs the matching route in `pkg/server/api.go`. App calls send `headers(ctx)` and cluster-wide calls send `clusterHeaders()`.
