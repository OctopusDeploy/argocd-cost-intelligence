# Changelog

All notable changes to this project are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until 1.0, a minor version may include breaking changes. They are listed under **Breaking**.

## [Unreleased]

## [0.1.0] - 2026-11-03

The first public release.

### Added

- A sync webhook (`POST /api/cost-analysis/on-sync`). Argo CD Notifications calls it after each successful sync. The service then asks OpenCost what the application's namespace and its own pods cost, projects that onto a 30-day month from the hours OpenCost has data for, and stores it in PostgreSQL. The webhook needs a bearer token (`WEBHOOK_SECRET`). When OpenCost has less than 5 minutes of data for the namespace, nothing is stored and the namespace is measured again, waiting 1, 2, 4 and then 8 minutes between tries.
- Costs right after install. When the service starts, it measures each Application that has no cost yet, instead of waiting for its next sync. These records are marked "Measured at install" and are never compared with a later sync. `BACKFILL_CONCURRENCY` (Helm `costIntelligence.backfillConcurrency`, 4 by default, `0` to turn it off) bounds how many namespaces are measured at once.
- A REST API behind Argo CD's extension proxy. Application routes only answer for the application named in the `Argocd-Application-Name` header. Cluster-wide routes answer for a gate Application: the one in `CLUSTER_SCOPE_APP`, otherwise the Argo CD Application that installed Cost Intelligence, otherwise the chart's `cost-intelligence-access` Application. Anyone who can `get` the gate sees every namespace's cost. `CLUSTER_SCOPE_APP=off` turns cluster-wide routes off.
- A UI plugin for Argo CD 3.4, 3.5 and 3.6, in Octopus Deploy's look:
  - On each application, a Cost Intelligence tab with actual costs (split by CPU, memory, network and storage), idle spend, efficiency, what changed since the last sync, a resources view and a trends view.
  - A system-level page with cost per namespace, idle spend per application with the saving from lowering its CPU request, chargeback by Kubernetes label and the control-plane fee.
- Trends with a date range, a month-end forecast and anomaly notices. The service records each application's daily cost itself (`DAILY_HISTORY_DAYS`, 400 by default), so the history reaches back further than OpenCost's Prometheus.
- Rightsizing from Vertical Pod Autoscaler recommendations in `Off` mode (including those Goldilocks creates), priced with OpenCost, once a recommendation has a day of history. It only suggests requests, never limits.
- Cloud service costs: Infracost estimates pushed from CI, shown next to the Kubernetes cost.
- Prometheus metrics on a port of their own (`METRICS_PORT`, 9102). A single exporter publishes each Application's cost per hour, and every API replica reports its own health. The chart can add ServiceMonitors and a PrometheusRule with a recording rule and starter alerts. See [docs/metrics.md](docs/metrics.md).
- A Helm chart, published to `oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence`. It deploys the backend, the exporter, the UI plugin and a bundled PostgreSQL, with NetworkPolicies that only let Argo CD's server, notifications controller and repo server reach the backend's API. It checks the type of each value, so a wrongly typed value stops `helm install` with an error naming the key. A post-install hook registers the extension and the webhook in Argo CD, installed with `install.yaml` or its Helm chart, and lets `role:admin` open the extension (`argoCd.rbac.grantAdmin`). It only writes keys of its own, plus its entry in `subscriptions`. `postInstallHook.extensionInitImage` sets the image that downloads the plugin, for clusters that mirror images. Multi-arch images (amd64 and arm64) are on GHCR.
- A PostgreSQL you already run, with `postgres.enabled=false`: `postgres.host` and `postgres.port` point the chart at it, or an ExternalName Service called `postgres` when `postgres.host` is empty.
- A guide for installing when Git manages Argo CD's own configuration, with the hook turned off. See [docs/gitops-install.md](docs/gitops-install.md). Each release attaches the UI plugin as `extension.tar.gz`, with its checksum in `extension_checksums.txt`, for the Argo CD extension installer.
- Applications that deploy to another cluster are never measured, since OpenCost only sees the cluster it runs in, and their tab shows which cluster they deploy to. `PRUNE_REMOTE_COST` (Helm `costIntelligence.pruneRemoteCost`, off by default) deletes cost already stored for them, and can't be undone.
- `LOG_LEVEL` (Helm `costIntelligence.logLevel`, `info` by default). OpenCost request URLs and responses, which name each namespace and its cost, are only logged at `debug`.

### Security

- Application data is scoped to the application in `Argocd-Application-Name`. Argo CD's proxy checks that header against the viewer's RBAC but not the request path, so the service checks the path itself.
- Records are keyed by the Application's namespace and name, so `team-a/web` and `team-b/web` stay separate with apps-in-any-namespace.
- `/on-sync` needs `Authorization: Bearer <WEBHOOK_SECRET>`. The backend won't start with a secret shorter than 16 characters.
- The NetworkPolicies are what make the headers trustworthy, so they need a CNI that enforces NetworkPolicy. k3s and k3d do, unless k3s was started with `--disable-network-policy`.

[Unreleased]: https://github.com/OctopusDeploy/argocd-cost-intelligence/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/tag/v0.1.0
