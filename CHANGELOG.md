# Changelog

All notable changes to this project are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until 1.0, a minor version may include breaking changes. They are listed under **Breaking**.

## [Unreleased]

### Breaking

- Building from source needs Go 1.27 or newer. The images and the Helm chart are unaffected.
- The hook's notification trigger and template are now `trigger.cost-intel-on-sync-succeeded` and `template.cost-intel-sync-succeeded`. 0.1.0 wrote them as `trigger.on-sync-succeeded` and `template.app-sync-succeeded`, names that Argo CD's notifications catalog uses too, and replaced `subscriptions` with its own entry. On upgrade the hook removes the two old keys if they still hold exactly what 0.1.0 wrote, and replaces its old entry in `subscriptions` with the new one. It keeps anything you changed, and keeps the old trigger while a subscription or `defaultTriggers` still names it. Argo CD Notifications records what it has sent per trigger name, so after the upgrade each application whose last sync succeeded sends the webhook once more, all at once, and gets one extra cost record for that sync. If 0.1.0 overwrote your own trigger, template or subscriptions, restore them from your backup. If you applied `deploy/argocd-notifications-patch.yaml` by hand instead, apply the new version, add the new entry to `subscriptions` as its comment shows, and remove the old keys if nothing else uses them.
- The hook image needs `jq` and `yq` (v4, mikefarah/yq, not the Python `yq`) as well as `kubectl`. The default `alpine/k8s` has them. If you set `postInstallHook.image`, check that yours does.
- The hook now fails if it can't update `argocd-notifications-cm`, for example when `subscriptions` isn't a YAML list, where 0.1.0 carried on. It registers the UI first, so the tab still appears. A missing `argocd-notifications-cm` or `argocd-rbac-cm` is skipped, not an error.
- `idleCost` in the namespaces API can now be `null`, for a namespace whose idle cost hasn't been measured yet.
- Cluster-wide costs (the Namespaces and Chargeback pages, and the control-plane fee) are now on by default. When `costIntelligence.clusterScopeApp` (`CLUSTER_SCOPE_APP`) is empty, the service no longer turns them off: it uses the Argo CD Application that installed Cost Intelligence as the gate, and when Helm installed it, the chart creates an Application and an AppProject called `cost-intelligence-access` in the Argo CD namespace to be the gate. Anyone your Argo CD RBAC lets `get` that Application sees every namespace's cost, which includes `role:readonly` if it is your `policy.default`. On upgrade, check who can view the gate, or set `costIntelligence.clusterScopeApp=off` to keep them off. A gate you set yourself is unchanged.

### Added

- `PRUNE_REMOTE_COST` (Helm: `costIntelligence.pruneRemoteCost`, off by default) deletes the cost stored for applications that deploy to another cluster: their cost records and daily history, which priced a namespace of the same name in this cluster rather than the application. It runs when the service starts and on each hourly daily history run, both of which read the Argo CD Applications from the cluster, never a webhook. Cloud estimates pushed from CI are kept. The deletion can't be undone, and an application counts as being on another cluster unless its destination is `https://kubernetes.default.svc` or `in-cluster`, so check that no application targets this cluster by its API server URL or by a renamed in-cluster entry before you turn it on, and back up the database if you need its history.
- The hook lets `role:admin` open the extension, so a new install on Argo CD 3.5 no longer shows "401: Unauthorized extension request" until someone adds a rule. It writes the rule to a key of its own, `policy.cost-intel.csv` in `argocd-rbac-cm`, and never changes `policy.csv`. Turn it off with `argoCd.rbac.grantAdmin=false`. If you added the admin line to `policy.csv` after installing 0.1.0, you can keep it or remove it.
- `postInstallHook.extensionInitImage` sets the image of the `argocd-server` init container that downloads the UI bundle, for clusters that mirror images.
- Costs show up right after install. When the service starts, it measures each Argo CD Application that deploys to this cluster and has no cost record yet, instead of waiting for the application's next sync. If OpenCost has no data for a namespace yet, it tries again a few times over the next quarter of an hour. These records say "Measured at install" in the UI and are never compared with a later sync in "Since last sync". Replicas take turns, and `BACKFILL_CONCURRENCY` (Helm `costIntelligence.backfillConcurrency`, 4 by default, `0` to turn it off) bounds how many namespaces are measured at once. It uses the existing permission to read Applications; no RBAC change is needed. In the API, these records have `measuredOn: "install"` and no `syncTimestamp`; every other record has `measuredOn: "sync"`.
- Cluster-wide costs work without configuration. The gate is the Argo CD Application that installed Cost Intelligence, found from the tracking annotation on the service's Deployment, or the chart's `cost-intelligence-access` Application when Helm installed it. It deploys nothing, in an AppProject that permits no resources, from an empty chart the service serves, and is never measured. The service looks the gate up again every minute and logs which one it uses. The chart adds a Role to read the service's own Deployment, `costIntelligence.accessApplication` to name or turn off the access Application, and, when it creates one, a NetworkPolicy rule admitting Argo CD's repo server to fetch the empty chart.
- A new Idle spend tab, between Namespaces and Chargeback, ranks applications by idle spend: the CPU and memory their own pods requested and didn't use, per month. Each one shows what lowering its CPU request would save, the same suggestion as on its tab, and the ranking can be narrowed to one namespace. Above it are the apps' idle spend, the total saving, how many apps aren't ranked yet and the cluster's idle spend. The idle spend figure on the Namespaces page links to it, and an application's deploy namespace opens that namespace on the Namespaces tab. Applications on another cluster or deleted from Argo CD are left out, using the existing permission to read Applications. Applications whose last record measured only their whole namespace, because the notifications template didn't send their resources yet, are counted but not ranked until their next sync. The ranking is served at `GET /api/cost-analysis/waste`, a cluster-wide route answering the same gate Application as the others.
- `argocd_cost_intelligence_exporter_remote_applications`, the number of Applications the exporter leaves out because they deploy to another cluster.
- The `remote` result on `argocd_cost_intelligence_sync_webhooks_total`, for a sync notification about an application on another cluster. The service answers it with 200 and measures nothing.
- The `ignored` result on `argocd_cost_intelligence_sync_webhooks_total`, for a sync notification about the chart's `cost-intelligence-access` Application, which deploys nothing. The service answers it with 200 and measures nothing.

### Changed

- The UI plugin has a new look. Both the application tab and the Namespaces / Chargeback page sit in a panel headed "Cost Intelligence", with "by Octopus Deploy" under it linking to the Octopus Deploy website. They use Octopus Deploy's colours and the Inter font, which is bundled into the plugin so it works without internet access. Labels are in sentence case and the Reload button is labelled.
- The `argocd-server` init container uses `busybox:1.37.0` instead of `busybox:latest`.

### Fixed

- Idle spend on the Namespaces page was always $0 with a real OpenCost, which leaves its idle fields at zero. The service now measures a namespace's idle cost from its pods' requests and usage, as it already did for an application, so its idle cost matches the sum of its applications' when they own all its pods. After an upgrade, existing namespaces show idle spend as not measured until their next sync, since their records were stored before it was measured; the cluster's idle spend then covers only the namespaces measured so far and says how many of them that is. In the API their `idleCost` is `null`. The upgrade adds a column to `cost_actuals`.
- An application that deploys to another cluster is no longer measured. OpenCost only sees the cluster it runs in, so the service used to price a namespace of the same name in its own cluster and store that as the application's cost. The exporter now leaves these applications out of the cost series, though their cloud estimates are still exported, and the sync webhook skips them. The webhook learns the cluster from two new fields in the notifications template, `destinationServer` and `destinationName`. The Helm chart adds them when it patches Argo CD. If you maintain the template yourself, add them as in `deploy/argocd-notifications-patch.yaml`. Until you do, every application is taken to deploy to this cluster, as before. An application counts as deploying here when its destination server is `https://kubernetes.default.svc` or its destination name is `in-cluster`; if you renamed the in-cluster entry in Argo CD, applications that target it by the new name are now taken to be on another cluster. Its daily history is no longer recorded, though cost already stored for it stays until you turn on `PRUNE_REMOTE_COST`.
- The application tab of an application that deploys to another cluster now says so, with the cluster's name or server, instead of showing empty cost views or the cost of a namespace of the same name in this cluster. It links to the single-cluster limit.
- The alert for refused webhooks no longer fires on a cluster whose syncs are all for other clusters.
- The hook no longer overwrites Argo CD Notifications settings you already had. It adds its entry to `subscriptions` and keeps the others, and running it again on upgrade adds nothing twice.

### Security

- `golang.org/x/net` is updated from 0.57.0 to 0.60.0 to fix GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617 in its HTTP/2 transport, which the Kubernetes client (rightsizing and the exporter) uses.

## [0.1.0] - 2026-10-04

The first public release. There's nothing to upgrade from.

### Added

- A sync webhook (`POST /api/cost-analysis/on-sync`). Argo CD Notifications calls it after each successful sync. The service then asks OpenCost what the application's namespace and pods cost, projects it onto a 30-day month from the hours OpenCost actually has data for, and stores the result in PostgreSQL. The webhook needs a bearer token (`WEBHOOK_SECRET`).
- A REST API behind Argo CD's extension proxy. Application routes only answer for the application named in the `Argocd-Application-Name` header. Cluster-wide routes only answer for the Application named in `CLUSTER_SCOPE_APP`, and are off when it's unset.
- A UI plugin for Argo CD 3.4 and 3.5:
  - On each application, a Cost Intelligence tab with actual costs (split by CPU, memory, network and storage), idle spend, efficiency, what changed since the last sync, a resources view and a trends view.
  - A system-level page with cost per namespace, chargeback by Kubernetes label and the control-plane fee.
- Trends with a date range, a month-end forecast and anomaly notices. The service records each application's daily cost itself (`DAILY_HISTORY_DAYS`, 400 by default), so the history reaches back further than OpenCost's Prometheus.
- Rightsizing from Vertical Pod Autoscaler recommendations (including those Goldilocks creates), priced with OpenCost. It only suggests requests, never limits.
- Cloud service costs: Infracost estimates pushed from CI, shown next to the Kubernetes cost.
- Prometheus metrics on a port of their own (`METRICS_PORT`, 9102). A single exporter publishes each Application's cost per hour, and every API replica reports its own health. The chart can add ServiceMonitors and a PrometheusRule with a recording rule and starter alerts. See [docs/metrics.md](docs/metrics.md).
- A Helm chart, published to `oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence`. It deploys the backend, the exporter, the UI plugin and a bundled PostgreSQL, with NetworkPolicies that only let `argocd-server` and `argocd-notifications-controller` reach the backend. A post-install hook registers the extension and the webhook in Argo CD. Multi-arch images (amd64 and arm64) are on GHCR.

### Security

- Application data is scoped to the application in `Argocd-Application-Name`. Argo CD's proxy checks that header against the viewer's RBAC but not the request path, so the service checks the path itself.
- Records are keyed by the Application's namespace and name, so `team-a/web` and `team-b/web` stay separate with apps-in-any-namespace.
- `/on-sync` needs `Authorization: Bearer <WEBHOOK_SECRET>`. The backend won't start with a secret shorter than 16 characters.
- The NetworkPolicies are what make the headers trustworthy, and they only work on a CNI that enforces NetworkPolicy. k3d and Flannel don't.

[Unreleased]: https://github.com/OctopusDeploy/argocd-cost-intelligence/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/tag/v0.1.0
