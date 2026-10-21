# Architecture

How the service is put together, and why. Read this before you change behaviour. [CLAUDE.md](../CLAUDE.md) lists the rules that are easiest to break, and [development.md](development.md) explains how to run things.

## Overview

Argo CD Notifications calls a webhook when a sync succeeds. The service asks OpenCost what the application cost over the last 24 hours, stores the result in PostgreSQL and serves it to a UI extension inside Argo CD. Cloud services the cluster cannot see (RDS, S3, ...) come from Infracost estimates that CI pushes.

```
Argo CD sync succeeds
  -> Notifications controller
  -> POST /api/cost-analysis/on-sync          202 returned at once
  -> goroutine: wait 5s -> query OpenCost (3 calls in parallel) -> store in PostgreSQL
  -> REST API, reached through Argo CD's extension proxy (/extensions/cost-intel/api/...)
  -> React plugin in the Argo CD UI
```

The same binary also runs as a Prometheus exporter (`MODE=exporter`), and a background recorder keeps daily cost history. Both are described below.

## Components

### HTTP server (`pkg/server/api.go`, `pkg/server/auth.go`)

Receives the webhook and serves cost data. Every route except the webhook is called through Argo CD's extension proxy and is authorized by the `Argocd-Application-Name` header the proxy has checked (see *Authorization*). The routes are registered in `setupRoutes` in `api.go`, grouped by what the header must name:

| Group | Routes | Header must name |
|---|---|---|
| Probes | `/health`, `/ready` | nothing |
| Webhook | `POST /api/cost-analysis/on-sync` | nothing; `Authorization: Bearer <WEBHOOK_SECRET>` instead (not proxied) |
| Any application | `/api/cost-analysis/config`, `/data-quality` | any application |
| One application | `/api/cost-analysis/{appName}/` `actual`, `cloud-estimate` (POST), `trend`, `daily`, `orphaned-pods`, `rightsizing` | `{appName}` |
| Cluster-wide | `/api/cost-analysis/namespaces`, `namespace/{ns}/` `summary`, `apps`, `orphaned-pods`; `chargeback`, `chargeback/labels`, `cluster/overhead` | the gate Application (`CLUSTER_SCOPE_APP`, or the one `pkg/clusterscope` finds) |

The request parameters are in the API section of the [README](../README.md#api). `/actual` attaches the anomaly score and the latest cloud estimate at read time; neither is stored.

The webhook is asynchronous. It validates, answers 202 and measures in a goroutine, so the notifications controller never waits. Each replica runs at most 20 measurements at once (`maxConcurrentSyncs`), and a webhook beyond that gets 429 instead of queuing unbounded work. Webhook bodies are capped at 64 KB and cloud-estimate bodies at 10 MB. A webhook for an application that deploys to another cluster gets 200 and is not measured (see *Sync hook*); it is answered before the 20-measurement limit, as nothing runs in the background.

### Sync hook (`pkg/hook/sync_hook.go`)

Measures and stores an application's cost after a sync.

1. Validate the notification (`ValidateNotification`): application name and namespace are required, namespaces must be DNS labels, resources must have valid names.
2. Skip an application that deploys to another cluster (`DeploysHere`). OpenCost measures only the cluster it runs in, so a namespace of the same name here is not the application's. The cluster is the notification's `destinationServer` or `destinationName`. Local means the server is empty or `https://kubernetes.default.svc`, and the name is empty or `in-cluster`. A template that sends neither field is taken to be local.
3. Skip anything but `SyncResult == "Succeeded"`.
4. Wait for metrics to be recorded. The delay is passed to `hook.NewSyncHook` in `cmd/main.go` (5 seconds); there is no environment variable.
5. Query OpenCost for the deployment namespace: cost, efficiency summary and per-pod breakdown. The cost query must succeed; the other two only log a warning and leave their fields empty.
6. Project the 24-hour cost onto a month (see *Monthly figures*).
7. Measure the namespace's idle cost from its pods (`PodAllocations.Efficiency`, see *Idle cost*) and store it as `idle_monthly_cost` and in the efficiency's idle fields. Without the pod breakdown it stays unmeasured.
8. If the webhook carried the application's `resources`, compute the application's own share from the pods (`PodAllocations.ForApplication`) and store it as `app_json`.
9. Save the record.

### Measuring at install (`pkg/backfill/`)

Only the sync hook writes cost records, so without this a fresh install would show nothing until every application synced. When an API replica starts (unless `BACKFILL_CONCURRENCY=0`, or outside a cluster), `runBackfill` in `cmd/main.go` lists the Argo CD Applications and measures each one that has no record yet:

1. Take a PostgreSQL advisory lock (`CostStorage.Lock`), so replicas run one at a time. A replica that waited finds the records the first one saved and measures only what is still missing; OpenCost is never queried once per replica. The lock lives on one database session and is released when the run ends or the session drops.
2. Skip Applications that already have a record, that deploy to another cluster (a destination other than `https://kubernetes.default.svc` / `in-cluster`, since OpenCost measures only its own cluster), or that have no valid destination namespace. With `PRUNE_REMOTE_COST=true`, an Application on another cluster that has cost stored anyway, from before such Applications were skipped, has its cost records and daily history deleted (`CostStorage.DeleteMeasurements`); its cloud estimates stay, as they come from CI. A failed delete counts as failed, so the run is retried.
3. Group the rest by destination namespace and measure each namespace once with the sync hook's queries (`hook.MeasureNamespace`), at most `BACKFILL_CONCURRENCY` at a time.
4. Build each Application's record as the sync hook would, its share taken from the controllers in its `status.resources`, but with no sync timestamp and `measured_on = 'install'`. A namespace with no OpenCost data, or one whose measurement failed, is tried again after 1, 2, 4 and 8 minutes (OpenCost may start alongside the service), then left for the next sync or start. Each run, the wait for the lock included, is cut off after 15 minutes so a hung dependency cannot keep the lock.
5. Save it only if the Application still has no record (`SaveFirstActual`, a conditional insert), so a sync that lands during the run is never overwritten. Any record saved wakes the history recorder (`Recorder.RecordSoon`), which then records those applications' days.

A record measured at install is never the baseline of a change since the last sync, and the UI labels it "Measured at install". The Application lister is the exporter's, reading the same ClusterRole rule.

### OpenCost client (`pkg/opencost/client.go`)

Queries OpenCost and turns its responses into the service's models. Each method is one use case (the doc comment on each says which OpenCost endpoint and aggregation it uses): the namespace measurement, efficiency summary, per-pod and per-container allocations, orphaned pods, chargeback by label, label keys, daily time series, pricing source and control-plane overhead. Calls go through an instrumented `http.RoundTripper` so they are counted per path and status.

### Storage (`pkg/storage/`)

`CostStorage` (`interface.go`) is the only thing the rest of the code depends on; `postgres.go` implements it and `instrumented.go` wraps any implementation with timing and counters. A missing record is `storage.ErrNotFound`, not a generic error. Schema changes are numbered migrations in `pkg/storage/migrations/`, applied at startup (`migrate.go`).

Tables:
- **`cost_actuals`**: one row per sync measurement, primary key a random 32-hex ID. `application_namespace` + `application_name` identify the application; `deployment_namespace` is where it deploys and what OpenCost was asked about. `measured_on` is `sync`, or `install` for a record measured when the service started (migration 009). `idle_monthly_cost` is the namespace's idle cost (migration 010); it is NULL for records measured before it, or without the pod breakdown, and the Namespaces page shows those as not measured rather than $0. An install record follows no sync: its `sync_timestamp` holds its measurement time, so the column stays `NOT NULL` for older replicas, and the storage layer reads it back as no sync timestamp. The record's own cost columns cover that whole namespace; `app_json` holds the application's share. Breakdowns are JSONB (`resources_json`, `opencost_output_json`, `efficiency_json`, `pods_json`, `app_json`). Indexed on `(application_namespace, application_name, measured_at DESC)` and `(deployment_namespace, measured_at DESC)`.
- **`cloud_estimates`**: one row per `(app_namespace, app_name, source)`, upserted on every CI push. Separate from `cost_actuals` because estimates arrive at a different cadence, and keyed by source so Infracost and a future tool coexist.
- **`cost_daily`**: one row per `(application_namespace, application_name, day)` with the day's cost, its `deployment_namespace` and its `scope`.
- **`cost_daily_progress`**: the last day recorded per application and its scope. It exists because a day without cost leaves no `cost_daily` row.

Exact columns are in the migrations; the Go types are in `pkg/models`. Writes are upserts (`ON CONFLICT ... DO UPDATE`), so replayed webhooks and concurrent recorders are harmless.

### Daily cost history (`pkg/history/`)

OpenCost answers only for the days its Prometheus keeps (often 15), and `cost_actuals` has a row per sync, so an application that has not synced for weeks would have gaps. `Recorder.Run` (started in `cmd/main.go` unless `DAILY_HISTORY_DAYS=0`) runs at startup and then hourly:

1. List every application's latest cost record. Each application is measured the way that record measured it: by the controllers Argo CD listed at the sync (`application` scope) or by its whole deployment namespace (`namespace` scope).
   In a cluster, the recorder first lists the Argo CD Applications (`Recorder.WithApplications`, the exporter's lister). One that deploys to another cluster is not recorded; with `PRUNE_REMOTE_COST=true` its cost records and history are deleted, as at install, before any application is measured, so the list is as fresh as it can be. If the list fails, every application is recorded as outside a cluster, nothing is deleted, and the run reports the error. An application missing from the list, deleted since its last record, is still recorded. Outside a cluster nothing is listed and every record is recorded.
2. Record the days that are over and not yet recorded. A day counts as over an hour after it ends, so its last metrics are in.
3. An application with no progress, or whose scope changed, is backfilled instead: newest 30 days first, back until 30 days without cost, which is where OpenCost's data or the application begins. It is marked recorded only when the backfill completes, so one cut short starts over.
4. Delete days older than `DAILY_HISTORY_DAYS` (default 400).

`history.Daily` serves `/daily`: stored days in the application's current scope, then OpenCost for the days after the last recorded one (today, and yesterday until an hour after midnight). A range that is already over needs no OpenCost query.

There is no leader election. Replicas that record at the same time store the same figures, and the price is one extra OpenCost query per application per day. Each series has one scope: days recorded in another scope are never served, so an application's own pods are never mixed with its whole namespace.

### Rightsizing (`pkg/rightsizing/`)

Turns Vertical Pod Autoscaler recommendations into the requests to set for each container and what that changes per month (`GET /{appName}/rightsizing`).

1. Read the application's latest record (for its deployment namespace) and the controllers Argo CD lists for it.
2. List the namespace's `VerticalPodAutoscaler`s (`KubeLister`, client-go dynamic client, cached for a minute per namespace). No VPA API, no permission or no in-cluster credentials all answer `available: false`.
3. Match each VPA to a controller by `targetRef`. Only a VPA in `Off` mode is advice; any other mode sets requests itself, so the workload is shown as managed.
4. With at least one VPA, fetch the namespace's per-container allocations (`GetContainerAllocations`). A container's current requests and memory limit are those of its longest run.
5. Round the target up to 0.01 cores and 16Mi and price it against each run. OpenCost charges the larger of request and usage, so the change is `price x (max(suggested, usage) x hours - billed hours) x 30`, at each run's own price.

Some choices worth knowing:

- Current requests come from OpenCost, not from manifests. That needs no permission on workloads, and the saving is priced the same way the cost is.
- Only requests are suggested. Limits never are, but a memory limit below the VPA's upper bound gets a warning.
- A new VPA is unsettled. Its upper bound starts at the VPA's maximum and narrows over about a day. A container whose upper bound is more than 3x its target is marked `unsettled` and its workload low confidence, and the UI shows neither the bound nor the limit warning.
- Without a VPA nothing shows. The UI shows the section only when a workload has one, and the VPA card replaces the CPU suggestion only when it saves or costs at least $1 a month (`hasAdvice` in `ui/src/utils/rightsizing.ts`).

### Prometheus metrics (`pkg/telemetry/`, `pkg/exporter/`)

[metrics.md](metrics.md) is the metric reference. The same binary runs in one of two modes (`MODE`):

- **`api`** (the Deployment behind the HPA) serves the REST API and, on `METRICS_PORT`, its own health (`pkg/telemetry`): requests by route pattern, webhooks and measurements by result, OpenCost calls, storage operations, history runs.
- **`exporter`** (one replica) exports each Application's cost. Every `EXPORTER_INTERVAL` it lists Applications from the Kubernetes API (`KubeApplicationLister`), queries OpenCost once per namespace over the last hour (`GetRecentPodAllocations`), attributes pods with `ManagedResources.OwnsPod` and divides by the hours OpenCost measured (`PodAllocations.HourlyCostOf`). Scrapes read the last refresh, an atomic snapshot.

Some choices worth knowing:

- Cost comes from one process, and health from every replica. Cost is global state, so if each API replica exported it, sums would multiply by the replica count. The exporter is a singleton (`Recreate`, one replica), like kube-state-metrics. A leader-elected API replica would need a Lease and RBAC for it.
- The gauges are measured rates, not projections: cost per hour over the last hour, split by `resource` with no total. Monthly figures, change since a sync and anomalies are queries and recording rules (`helm/files/prometheus-rules.yaml`), not metrics.
- `namespace` and `name` are the Application's, as in `argocd_app_info`, so the two join without relabelling. The exporter is scraped with `honorLabels`.
- An Application that OpenCost couldn't measure has no series until it can be, and `exporter_refresh_errors_total` counts the failure. That's better than serving an old value. A deleted Application's series go at the next refresh, because Applications are listed live.
- An Application that deploys to another cluster (`Destination.Local`) has no cost or workload series: OpenCost here would price a namespace of the same name in this cluster instead. It isn't an error, so it is counted by the `exporter_remote_applications` gauge, not by `exporter_refresh_errors_total`, whose alert would never clear on a hub cluster. Its cloud estimates are still exported, because they come from CI.
- `/metrics` is never on the API port, which trusts Argo CD's proxy headers.

### Configuration (`pkg/config/config.go`)

Environment variables parsed by `envconfig`, validated at startup (`Validate`) so a bad setup fails fast. The variables and defaults are in the struct tags, in [.env.example](../.env.example) and in [configuration.md](configuration.md#environment-variables), so they aren't repeated here.

## Design decisions

### Monthly figures

Costs are projected onto a 30-day month from the period OpenCost actually measured, not from the 24 hours queried (`monthlyFactor` / `measuredPeriod` in `pkg/opencost/client.go`):

```
Full day:       $5 over 24 h      ->  $5 x 720 / 24 = $150 per month
New namespace:  $1 over 6 h       ->  $1 x 720 / 6  = $120 per month
```

OpenCost reports a namespace only from when its first pod ran, so a fixed x 30 would put a young namespace's monthly cost far too low. Pods share their response's period, so they still add up to the namespace. Never multiply an OpenCost cost by a fixed 30.

The 24-hour window itself is a trade-off: long enough to include off-peak hours and smooth short spikes, short enough that a sync's effect shows within a day.

### Idle cost

Idle cost is the part of a CPU or memory cost paid for requested capacity that went unused: `cost x (request - usage) / request` per pod, nothing for a pod using at least its request, weighted by the minutes each pod ran and projected like every other cost (see *Monthly figures*). It is measured from the per-pod allocation for both scopes: an application's from its own pods (`ForApplication`), a namespace's from all of them (`PodAllocations.Efficiency`), so the applications in a namespace add up to it when they own all its pods. A namespace whose pods are missing from the answer while it has CPU or memory cost has no idle cost measured. OpenCost's own `cpuCostIdle` / `ramCostIdle` are never used: up to 1.121.3 OpenCost only copies and sums them, so they are always zero.

### Namespace scope and application scope

OpenCost is queried by namespace, so **a record's own columns hold the cost of the application's whole deployment namespace**, not its share. `app_json` (`CostActual.App`) holds the application's own measurement: the pods of the controllers Argo CD listed in the webhook's `resources`, matched by `models.ManagedResources.OwnsPod` (a CronJob's Jobs and a Rollout's ReplicaSets count under their parent), with cost, breakdown and efficiency computed from those pods.

- Aggregate once per namespace (`ListNamespaces`): summing records across apps that share a namespace double-counts.
- Application routes serve `CostActual.ForApplication()`, which marks `scope: "application"`, or `"namespace"` for records without `app_json` (stored before migration 007, or synced by a notifications template that does not send `resources`). Namespace routes serve the namespace columns.
- Never compare figures of different scopes (see `comparableHistory` in `api.go`; the recorder stores a scope with each day).
- The UI attributes cost per app on the Namespaces page by matching pods to controllers (`ui/src/utils/namespaceApps.ts`). For old `namespace` records the app page narrows the figures itself (`asApplication` / `scopeToApp` in `ui/src/utils/costInsights.ts`) and labels the rest as the namespace's.
- The notifications template must send `{{.app.spec.destination.namespace}}` (where the app deploys, not `.app.metadata.namespace`, where the Application object lives) and the app's `resources`, plus the destination cluster as `destinationServer` and `destinationName` (each wrapped in `{{with}}`, so an unset one is empty rather than `<no value>`). See `deploy/argocd-notifications-patch.yaml`. The Helm chart reads the same template from `helm/files/argocd/` unrendered, so the braces need no escaping.

### Application identity

An application is its `application_namespace` plus its name. With apps-in-any-namespace, `team-a/web` and `team-b/web` are different Applications. Every per-application query filters on both, and anything grouped "per app" groups by both.

### Authorization

Argo CD's extension proxy checks that the caller may `get` the Application named in `Argocd-Application-Name`, but not that the request path refers to it. `pkg/server/auth.go` closes that gap:

- `/{appName}/...` routes require the header to name `{appName}`. `requireApp` passes the header's full `models.AppRef` to the handler through the request context; mount handlers with `appScoped(...)` and look data up by that `AppRef`, never by `chi.URLParam(r, "appName")`, which cannot tell same-named apps apart.
- Cluster-wide routes require the header to name the gate Application. It is `CLUSTER_SCOPE_APP` when set (`off` disables the routes); otherwise `pkg/clusterscope` resolves it, cached for a minute: the Application named in the tracking annotation of the service's own Deployment, if that Application exists here, deploys to this cluster and lists the Deployment among its resources; else `CLUSTER_ACCESS_APP`, if it exists and carries the `cost-intelligence.octopus.com/cluster-access` label; else none, and the routes are off. A lookup that fails keeps the last answer and is retried after 5 seconds; while one request looks the gate up, the others are served the last answer. The chart creates the access Application (an empty chart the service serves at `/charts/`, in an AppProject that permits no resources) only when Helm renders it against a cluster with Argo CD's CRD, which `lookup` detects; Argo CD's own rendering sees no cluster, so an Argo CD install never gets one. It is never measured: the exporter, the backfill and the history recorder skip Applications with the label, and the sync webhook answers a notification for `CLUSTER_ACCESS_APP` with 200 and measures nothing. `CLUSTER_ACCESS_APP` is only set when the chart created that Application, so nobody can become the gate by creating one of that name first; someone who may also delete Applications in Argo CD's namespace could still replace it, which already lets them do far more. Helm creates it only if it can read the CRD: without permission to read CustomResourceDefinitions, or with Argo CD installed after the chart until the next `helm upgrade`, there is none. `/config` and `/data-quality` need any header; `/charts/` needs none, as it serves only a chart with no templates.
- `Argocd-Username` and `Argocd-User-Groups` are never trusted for authorization: the proxy only sets them when non-empty, so a client-supplied value passes through.
- `/on-sync` is not proxied, so no header vouches for it. It requires `Authorization: Bearer <WEBHOOK_SECRET>`, compared in constant time (`requireWebhookToken`). The chart generates the token (or reads `webhook.existingSecret`) and its post-install hook copies it into `argocd-notifications-secret` as `cost-intel-webhook-token`, which the notifications webhook sends. The Deployment carries a checksum of the token so pods restart when it changes.
- The headers can be trusted only because nothing else can reach the pods: the chart's NetworkPolicy admits just `argocd-server` and the notifications controller (Prometheus only to the metrics port).

[SECURITY.md](../SECURITY.md) has the trust model for deployers and how to report a problem. Other hardening, as implemented: namespaces and label keys are validated against Kubernetes name patterns before use in OpenCost filters (`pkg/validation`); `OPENCOST_BASE_URL` must be http(s) with a host; the ClusterRole is read-only (pods, namespaces, Applications, and `verticalpodautoscalers` get/list when VPA is enabled); the VPA namespace comes from the stored record, never from the caller; `/metrics` carries every Application's cost, so whoever can query Prometheus sees it regardless of Argo CD RBAC (`metrics.exporter.enabled=false` exports only service health).

## Extension points

- **A storage backend:** implement `storage.CostStorage`.
- **Another IaC cost tool (e.g. Terracost):** add a parser in `pkg/cloudcost/` that returns `models.CloudServiceEstimate`, and a `case` for its source name in the `switch source` of `handlePostCloudEstimate` (`pkg/server/api.go`), which also sets `validCloudSources`. Storage and UI need no change: `source` already flows through.
- **A new route:** put it in the authorization group matching its scope. An app-scoped route must never take a namespace from the caller.
- **A new migration:** add the next numbered `.up.sql` / `.down.sql` pair in `pkg/storage/migrations/`.

Kubecost serves the same allocation API as OpenCost, so pointing `OPENCOST_BASE_URL` at it may work for the shared endpoints; this is untested.

## Deployment

The Helm chart ([helm/README.md](../helm/README.md), [configuration.md](configuration.md)) deploys the API Deployment (HPA 2 to 5 replicas by default), the exporter Deployment (1 replica), a UI plugin Deployment whose files an init container on `argocd-server` installs as the extension, a bundled PostgreSQL StatefulSet (replaceable with an external database), and a NetworkPolicy. OpenCost and Prometheus are prerequisites. `deploy/` holds the same thing as raw manifests for `make deploy-k8s`.

## Known limitations

1. **Records are per namespace.** An application's stored namespace figures cover everything in its namespace; per-app figures exist only when the webhook carried `resources`.
2. **IaC estimates are pushed, not pulled.** Cloud-service costs need a CI step that posts the Infracost output. `pushed_at` is the only freshness signal; the UI warns after 7 days.
3. **24-hour window.** A sync's effect shows fully only after a day. A namespace younger than that is projected from the hours it has existed, which is noisier the shorter it is: a sync measured minutes after a first deploy finds little or no data.
4. **A missed webhook is a missed measurement.** There is no periodic re-measure of `cost_actuals`, so an app that does not sync is not measured again (daily history and the exporter still read OpenCost, so cost over time keeps flowing). Measuring at install fills in only applications with no record at all.
5. **Only this cluster is measured.** OpenCost measures the cluster it runs in, so an application that deploys to another cluster has no cost records and no cost series: the sync webhook, the exporter and the measurement at install all skip it rather than measure a same-named namespace here. Measuring those needs this service and OpenCost in that cluster. The daily history recorder never records one either. Cost stored for such an application before this was enforced stays unless `PRUNE_REMOTE_COST` is on, which deletes it at startup and on each history run; both read the live Application list, never the webhook, so a webhook caller cannot have data deleted, though anyone who can retarget an Application to another cluster can. The application's tab says it deploys to another cluster instead of showing its views (`otherCluster` in `ui/src/utils/destination.ts` applies the same rule to the Application that Argo CD hands the tab), and the Namespaces page no longer lists it once its cost is deleted. The rule is by name: an application that targets this cluster by its API server URL, or by an in-cluster entry renamed in Argo CD, is taken to be on another cluster, so it is not measured, and with `PRUNE_REMOTE_COST` its stored cost is deleted.
6. **Re-measuring is not replaying.** Calling the webhook again measures the last 24 hours as of now, not as of the original sync, and OpenCost history cannot be rebuilt after its retention window.
7. **No backup.** Nothing in the chart backs up PostgreSQL; use your platform's backup tooling.
