# CLAUDE.md

Guidance for AI coding assistants (and a quick reference for humans) working in this repository. For setup see [docs/installation.md](docs/installation.md); for internals (components, routes, schema, reasons) see [docs/architecture.md](docs/architecture.md); for running tests and a local cluster see [docs/development.md](docs/development.md); for the contribution process see [CONTRIBUTING.md](CONTRIBUTING.md).

## Overview

A Kubernetes-native service that records the actual cost of Argo CD applications. Argo CD Notifications posts a webhook after each sync; the service queries OpenCost, stores the result in PostgreSQL and serves it over a REST API to a React plugin embedded in the Argo CD UI.

- **Backend:** Go, chi v5, PostgreSQL (`cmd/`, `pkg/`)
- **UI plugin:** React + TypeScript, built with webpack (`ui/`)
- **Deploy:** Helm chart (`helm/`) and raw manifests (`deploy/`); `mock-opencost-server.js` is the local OpenCost (copied into `deploy/mock-opencost.yaml` by `make sync-mock`, which CI checks)

## Commands

```bash
make build            # go build -> bin/argocd-cost-intelligence
make test             # go test -v ./...
make test-integration # + -tags=integration: storage (testcontainers, needs Docker) and OpenCost client (skipped unless an OpenCost or the mock is on :9003)
make lint             # golangci-lint run ./...
make fmt              # go fmt ./...
docker-compose up -d  # service :8080 + PostgreSQL + mock OpenCost :9003

go test -v -run TestName ./pkg/server/...   # a single test
```

UI (`cd ui`): `npm run lint`, `npm run typecheck`, `npm test`, `npm run build`.

## Request flow

```
sync-succeeded event → Argo CD Notifications
  → POST /api/cost-analysis/on-sync          (202 returned immediately)
  → background goroutine:
      wait 5s → query OpenCost (/allocation, 24h window; namespace cost, efficiency, pods)
      → project to a 30-day month → upsert into cost_actuals
  → REST API serves stored records
```

## Packages

| Package | Responsibility |
|---|---|
| `cmd/` | Entry point. `MODE=api`: config → DB → OpenCost client → SyncHook → HTTP server, plus `/metrics`. `MODE=exporter`: the cost exporter |
| `pkg/config` | Environment parsing (`envconfig`) and DSN |
| `pkg/models` | Shared types: `CostActual`, `SyncNotification`, `CloudServiceEstimate`, ... |
| `pkg/server` | Router, auth middleware, handlers |
| `pkg/hook` | Sync orchestration: delay → OpenCost query → storage write |
| `pkg/opencost` | OpenCost allocation client and cost transformation |
| `pkg/cloudcost` | Normalises IaC cost JSON (Infracost) into `CloudServiceEstimate` |
| `pkg/storage` | `CostStorage` interface and PostgreSQL implementation |
| `pkg/backfill` | At startup, measures each local Application without a cost record (`measured_on = 'install'`), one replica at a time under an advisory lock |
| `pkg/history` | Daily cost history: `Recorder` stores each app's cost per UTC day, `Daily` serves `/daily` |
| `pkg/telemetry` | Each API replica's own Prometheus metrics (requests, webhooks, measurements, OpenCost and DB calls, history runs); nil-safe |
| `pkg/exporter` | Singleton exporter: lists Applications live, measures each namespace over the last hour, exports each app's cost per hour |
| `pkg/rightsizing` | VPA recommendations (`KubeLister`) priced from OpenCost (`Recommend`) |

## Design rules

These are the invariants most likely to be broken by a well-meant change.

**Monthly projection.** OpenCost is queried with a 24h window and costs are projected onto a 30-day month from the period it actually has data for (`monthlyFactor` / `measuredPeriod` in `pkg/opencost/client.go`). A namespace younger than a day gets a larger factor. Never multiply an OpenCost cost by a fixed 30.

**Namespace vs. application scope.** A cost record's own columns measure the whole deployment namespace; `app_json` (`CostActual.App`) holds the application's share: the pods of the controllers Argo CD lists in the webhook's `resources`, matched by `models.ManagedResources.OwnsPod`.
- Aggregate namespaces once (`ListNamespaces`). Summing records across apps in one namespace double-counts.
- App routes serve `CostActual.ForApplication()` (`scope: "application"`, or `"namespace"` for old records without `app_json`). Never mix the two scopes in one comparison (`comparableHistory`).
- The notifications template must send `{{.app.spec.destination.namespace}}` (where the app deploys) and the app's `resources`; see `deploy/argocd-notifications-patch.yaml`.

**Application identity.** An application is its `application_namespace` plus its name: with apps-in-any-namespace, `team-a:web` and `team-b:web` differ. Every per-app query filters on both, and anything grouped "per app" groups by both.

**Idempotent writes.** `cost_actuals` upserts with `ON CONFLICT (id) DO UPDATE`, so replayed webhooks are safe. Replicas of the history recorder may run at once for the same reason.

**Storage behind an interface.** Depend on `storage.CostStorage`, never on the PostgreSQL type. Schema changes are numbered up/down migrations in `pkg/storage/migrations/`.

**Measured at install.** Only a sync or the startup backfill writes `cost_actuals`. A backfilled record has `measured_on = 'install'` and no sync timestamp in the API (the column holds its measurement time, so older replicas can still read it during a rollout); it is only ever a first record (`SaveFirstActual`), and it is never the baseline of a change since the last sync (`measuredAtInstall` in the UI); anomalies may still use it.

**Daily history.** OpenCost keeps only the days its Prometheus retains, so `history.Recorder` stores each app's cost per UTC day in `cost_daily` (progress in `cost_daily_progress`). Days are stored with their scope and never served in another. `DAILY_HISTORY_DAYS` (default 400, `0` = off) bounds retention.

**Rightsizing.** `/rightsizing` reads `VerticalPodAutoscaler`s in the app's namespace and matches them to the app's controllers. Only a VPA in `Off` mode is advice. Current requests and savings come from OpenCost's per-container allocations, not manifests. Requests only; limits are never suggested. Without the VPA API, permission or an in-cluster config the answer is `available: false`.

**Prometheus metrics** ([docs/metrics.md](docs/metrics.md)). Each API replica serves its own health on `METRICS_PORT` (9102), never on the API port, which trusts proxy headers; the NetworkPolicy admits only `metrics.allowedFrom` there.
- Application cost comes from one exporter Deployment (`MODE=exporter`, 1 replica, `Recreate`), never from API replicas, whose identical series would multiply sums.
- Its gauges are measured cost per hour over the last hour (`PodAllocations.HourlyCostOf`), partitioned by `resource` with no total. Never export projections (monthly), derived values (change since sync, anomaly) or old values: those are PromQL and the rules in `helm/files/prometheus-rules.yaml`, tested with promtool (`test/prometheus/`).
- `namespace` / `name` are the Application's, as in `argocd_app_info`; the exporter is scraped with `honorLabels`. Unmeasured apps are absent, counted by `exporter_refresh_errors_total`.
- Storage calls are timed by wrapping `CostStorage` in `storage.Instrumented`; a missing record is `storage.ErrNotFound`, not an error.

## Authorization

See `pkg/server/auth.go`. Argo CD's proxy checks `Argocd-Application-Name` against the viewer's RBAC but not the request path, so the service enforces the rest:

- `/{appName}/...` routes require the header's app to equal `{appName}`. Mount them with `appScoped(...)` and look data up by the `models.AppRef` from the request context, never by `chi.URLParam(r, "appName")`.
- Cluster-wide routes require the header to name `CLUSTER_SCOPE_APP` (disabled when unset).
- `/on-sync` is not proxied; it requires `Authorization: Bearer <WEBHOOK_SECRET>`.
- Never trust `Argocd-Username` / `Argocd-User-Groups`: a client-supplied value passes through the proxy.
- New routes go in the group matching their scope and must never take a namespace from the caller on an app-scoped route.

In the UI, app calls send `headers(ctx)` and cluster calls send `clusterHeaders()`.

## Configuration

Required: `DB_PASSWORD`, `OPENCOST_BASE_URL`, `WEBHOOK_SECRET` (16+ characters). Optional: `CLUSTER_SCOPE_APP` (`<namespace>:<name>`), `DAILY_HISTORY_DAYS`, `BACKFILL_CONCURRENCY` (default 4, `0` = off), `VPA_ENABLED`, `METRICS_PORT` (default 9102; `0` = off in API mode; must differ from `SERVER_PORT`). `MODE=exporter` runs the exporter, which needs no `WEBHOOK_SECRET` and refreshes every `EXPORTER_INTERVAL` (default 5m, at least 1m). The full list with defaults is in [.env.example](.env.example) and [docs/configuration.md](docs/configuration.md#environment-variables).

## Go conventions

Linting is golangci-lint v2 (`.golangci.yaml`) with `gofumpt` and `goimports`.

- Use `github.com/golang-jwt/jwt/v5`, `dario.cat/mergo` and stdlib `errors`.
- Kubernetes import aliases are enforced: `appsv1`, `corev1`, `rbacv1`, `apierrors`, `metav1`, `stderrors`.
- `unparam` is disabled in `_test.go` files.

## UI plugin

`ui/src/index.tsx` registers an application tab (`CostPanel`: Actual costs / Resources / Trends) and a system page at `/cost-intelligence` (`NamespaceDashboard`: Namespaces / Chargeback).

**React is not bundled.** Webpack maps it to the `window.React` that Argo CD sets: React 16.14 on Argo CD 3.4, React 19 on 3.5. Use only APIs React 16.14 has (no `useSyncExternalStore`, `useId`, `useTransition`, `react-dom/client`); ESLint rejects them. Keep `"jsx": "react"` in tsconfig.

**Definition of done:** lint, typecheck, test and build all pass, and `find src -name '*.d.ts'` is empty.

### Design system

- **Octopus Deploy look.** The plugin is a branded panel inside Argo CD's page ("Cost Intelligence by Octopus Deploy"), in Octopus's palette and its Inter font, with bordered cards, an icon tile above each card title, sentence-case labels and outlined buttons. Argo CD's own page around it is unchanged. Inter is bundled (`src/theme/font.ts`, inlined by webpack) because Argo CD serves the extension as one JS file and air-gapped clusters can't reach a font host.
- **Colours** come from `src/theme/tokens.ts` (`useTokens()`) only; never hardcode one. `cardStyle(t)` is the standard container, `series` the chart palette.
- **Reuse shared components** before building: `PluginFrame` (root and branded header), `CardHeader` / `IconTile`, `TabBar`, `StatStrip`, `Notice` / `ErrorNotice`, `ReloadButton`, `MeasuredLabel`, `Share`, `AdviceCard`, `Chip`, `levelIcon` / `LEVEL_LABELS`, `icons.tsx`. `BrandMark` is a stand-in for the official Octopus Deploy logo; swap its SVG when the asset arrives.
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
- View state lives in the URL via `src/hooks/useUrlState.ts` (`costTab`, `costNs`, `costLabel`, `costScope`, `costRange`, `costFrom`, `costTo`). `push` for navigation, `replace` for refinements. Never touch Argo CD's own parameters.
- Pages refresh every 30s in the background without a spinner and keep the last good data if a refresh fails.
- The theme is read from Argo CD's `view_preferences` localStorage key by one shared poller (`src/hooks/useTheme.ts`). Do not add per-component timers.

### Testing

- Test behaviour and logic; test styling only where it guards accessibility.
- jest-dom is not installed: assert with `getAttribute` / `textContent`.
- jsdom has no `localStorage`: stub it (see `useTheme.test.tsx`).
- Call `vi.useFakeTimers()` before rendering anything that schedules a timer, and advance with `advanceTimersByTimeAsync`; `waitFor` hangs under fake timers.
- Width-dependent layouts are tested by stubbing `HTMLElement.prototype.getBoundingClientRect` (see `StatStrip.test.tsx`). Restore mocks in `afterEach`.
- Check a new test can fail: break the behaviour, watch it fail, restore.

### Trying a build in a running Argo CD

`argocd-server` re-reads `/tmp/extensions` on every request, so a bundle can be swapped in without a restart (lost when the pod restarts):

```bash
cd ui && npm run build && cd ..
POD=$(kubectl get pods -n argocd -l app.kubernetes.io/name=argocd-server -o name | head -1)
CONTAINER=$(kubectl get -n argocd "$POD" -o jsonpath='{.spec.containers[0].name}')
kubectl exec -i -n argocd "$POD" -c "$CONTAINER" -- sh -c \
  'cat > /tmp/extensions/.new.js && mv -f /tmp/extensions/.new.js /tmp/extensions/extension-cost-intel.js' < ui/dist/index.js
```

System-level extension registration is deferred with `setTimeout(0)` in `index.tsx` on purpose; read the comment there before changing it.
