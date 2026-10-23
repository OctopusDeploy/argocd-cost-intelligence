# CLAUDE.md

Guidance for AI coding assistants (and a quick reference for humans) working in this repository. For setup see [docs/installation.md](docs/installation.md); for internals (components, data flows, design decisions) see [docs/architecture.md](docs/architecture.md); for running tests and a local cluster see [docs/development.md](docs/development.md); for the contribution process see [CONTRIBUTING.md](CONTRIBUTING.md).

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
docker compose up -d  # service :8080 + PostgreSQL + mock OpenCost :9003

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
- The notifications template must send `{{.app.spec.destination.namespace}}` (where the app deploys), the app's `resources` and its destination cluster (`destinationServer`, `destinationName`); see `deploy/argocd-notifications-patch.yaml`.

**Only this cluster.** OpenCost measures the cluster it runs in. An Application deploying elsewhere (`exporter.Destination.Local`, `hook.DeploysHere`) is never measured, by the webhook, the exporter or the startup backfill, and the history recorder never records it: a same-named namespace here is not its namespace. It is expected, so it is never counted as an error. With `PRUNE_REMOTE_COST` (off by default, as it can't be undone), cost already stored for one is deleted (`DeleteMeasurements`) by the backfill and the recorder, driven only by the live Application list, never by a webhook. The tab shows `RemoteClusterPanel` for it (`otherCluster` in `ui/src/utils/destination.ts`, the same rule).

**Application identity.** An application is its `application_namespace` plus its name: with apps-in-any-namespace, `team-a:web` and `team-b:web` differ. Every per-app query filters on both, and anything grouped "per app" groups by both.

**Idempotent writes.** `cost_actuals` upserts with `ON CONFLICT (id) DO UPDATE`, so replayed webhooks are safe. Replicas of the history recorder may run at once for the same reason.

**Storage behind an interface.** Depend on `storage.CostStorage`, never on the PostgreSQL type. Schema changes are numbered up/down migrations in `pkg/storage/migrations/`.

**Measured at install.** Only a sync or the startup backfill writes `cost_actuals`. A backfilled record has `measured_on = 'install'` and no sync timestamp in the API (the column holds its measurement time, so older replicas can still read it during a rollout); it is only ever a first record (`SaveFirstActual`), and it is never the baseline of a change since the last sync (`measuredAtInstall` in the UI); anomalies may still use it.

**Daily history.** OpenCost keeps only the days its Prometheus retains, so `history.Recorder` stores each app's cost per UTC day in `cost_daily` (progress in `cost_daily_progress`). Days are stored with their scope and never served in another. `DAILY_HISTORY_DAYS` (default 400, `0` = off) bounds retention.

**Rightsizing.** `/rightsizing` reads `VerticalPodAutoscaler`s in the app's namespace and matches them to the app's controllers. Only a VPA in `Off` mode is advice, and only once its recommendation has a day of history (`minHistory`; before that the workload is `learning`). Current requests and savings come from OpenCost's per-container allocations, not manifests. Requests only; limits are never suggested. Without the VPA API, permission or an in-cluster config the answer is `available: false`.

**Prometheus metrics** ([docs/metrics.md](docs/metrics.md)). Each API replica serves its own health on `METRICS_PORT` (9102), never on the API port, which trusts proxy headers; the NetworkPolicy admits only `metrics.allowedFrom` there.
- Application cost comes from one exporter Deployment (`MODE=exporter`, 1 replica, `Recreate`), never from API replicas, whose identical series would multiply sums.
- Its gauges are measured cost per hour over the last hour (`PodAllocations.HourlyCostOf`), partitioned by `resource` with no total. Never export projections (monthly), derived values (change since sync, anomaly) or old values: those are PromQL and the rules in `helm/files/prometheus-rules.yaml`, tested with promtool (`test/prometheus/`).
- `namespace` / `name` are the Application's, as in `argocd_app_info`; the exporter is scraped with `honorLabels`. Unmeasured apps are absent, counted by `exporter_refresh_errors_total`.
- Storage calls are timed by wrapping `CostStorage` in `storage.Instrumented`; a missing record is `storage.ErrNotFound`, not an error.

## Authorization

See `pkg/server/auth.go`. Argo CD's proxy checks `Argocd-Application-Name` against the viewer's RBAC but not the request path, so the service enforces the rest:

- `/{appName}/...` routes require the header's app to equal `{appName}`. Mount them with `appScoped(...)` and look data up by the `models.AppRef` from the request context, never by `chi.URLParam(r, "appName")`.
- Cluster-wide routes require the header to name the gate Application: `CLUSTER_SCOPE_APP` when set (`off` disables them), else the one `pkg/clusterscope` resolves (the Application whose tracking annotation is on the service's own Deployment, verified against its status, then the chart's labelled access Application). Read it through `s.clusterScopeApp(ctx)`; it can change at runtime.
- The access Application deploys nothing and is never measured (`models.ClusterAccessLabel`). Its empty chart is served unauthenticated at `/charts/`; keep that chart free of templates.
- `/on-sync` is not proxied; it requires `Authorization: Bearer <WEBHOOK_SECRET>`.
- Never trust `Argocd-Username` / `Argocd-User-Groups`: a client-supplied value passes through the proxy.
- New routes go in the group matching their scope and must never take a namespace from the caller on an app-scoped route.

In the UI, app calls send `headers(ctx)` and cluster calls send `clusterHeaders()`.

## Configuration

Required: `DB_PASSWORD`, `OPENCOST_BASE_URL`, `WEBHOOK_SECRET` (16+ characters). Optional: `CLUSTER_SCOPE_APP` (`<namespace>:<name>`; empty = find one, `off` = off), `CLUSTER_ACCESS_APP`, `DAILY_HISTORY_DAYS`, `BACKFILL_CONCURRENCY` (default 4, `0` = off), `PRUNE_REMOTE_COST` (default off), `VPA_ENABLED`, `METRICS_PORT` (default 9102; `0` = off in API mode; must differ from `SERVER_PORT`). `MODE=exporter` runs the exporter, which needs no `WEBHOOK_SECRET` and refreshes every `EXPORTER_INTERVAL` (default 5m, at least 1m). The full list with defaults is in [.env.example](.env.example) and [docs/configuration.md](docs/configuration.md#running-without-helm).

## Go conventions

The Go conventions are in the development guide, with the commands and local setup. It is imported here:

@docs/development.md

## UI plugin

The plugin's commands, how Argo CD loads it and its conventions (design system, Argo CD's CSS, layout, accessibility, interaction) are in the plugin's README. It is imported here:

@ui/README.md

Read [Writing tests](CONTRIBUTING.md#writing-tests) in CONTRIBUTING before writing a Go or UI test (jsdom limits, fake timers, stubbing widths).
