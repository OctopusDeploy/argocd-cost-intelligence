# Changelog

All notable changes to this project are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until 1.0, a minor version may include breaking changes. They are listed under **Breaking**.

## [Unreleased]

### Breaking

- Building from source needs Go 1.27 or newer. The images and the Helm chart are unaffected.

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
