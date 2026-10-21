# ArgoCD Cost Intelligence

[![CI](https://github.com/OctopusDeploy/argocd-cost-intelligence/actions/workflows/ci.yml/badge.svg)](https://github.com/OctopusDeploy/argocd-cost-intelligence/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/OctopusDeploy/argocd-cost-intelligence?include_prereleases)](https://github.com/OctopusDeploy/argocd-cost-intelligence/releases)

A Kubernetes-native service that measures what each Argo CD application actually costs. After every successful sync it asks [OpenCost](https://www.opencost.io) what the application's namespace and pods cost, stores the result in PostgreSQL, and shows it in a **Cost Intelligence** tab inside the Argo CD UI.

> **Status: early release (v0.x).** APIs, Helm values and the database schema may still change between minor versions. Breaking changes are listed in the [CHANGELOG](CHANGELOG.md).
>
> This is an independent community project. It is not affiliated with or endorsed by the Argo project, the CNCF, or OpenCost.

## What you get

- Actual cost per application: the monthly cost split into CPU, memory, network and storage, limited to the controllers Argo CD manages for that application, plus efficiency (requested against used) and idle spend.
- Trends: a daily cost history of up to a year, a forecast and anomaly notices. The history is kept in PostgreSQL, so it outlives OpenCost's Prometheus retention.
- Namespaces, Chargeback and cluster overhead: a system-level page with the cost per namespace, per Kubernetes label, and the control-plane fee. It's opt-in; see [the API section](#api).
- Rightsizing, optional: Vertical Pod Autoscaler recommendations, priced with OpenCost.
- Cloud service costs, optional: Infracost estimates pushed from CI and shown next to the Kubernetes cost.
- Prometheus metrics, optional: the cost of each Application per hour, and the service's own health.

## Screenshots

The tabs on an application, in the Argo CD dark theme:

![Actual costs: monthly cost, cost breakdown, resource efficiency and what changed since the last sync](docs/images/actual-costs.webp)

![Resources: cost per controller and Vertical Pod Autoscaler rightsizing suggestions](docs/images/resources.webp)

![Trends: daily cost for the last 30 days with a month-end forecast](docs/images/trends.webp)

The system page, for people who can see cluster-wide costs:

![Namespaces: cost, idle spend and efficiency per namespace](docs/images/namespaces.webp)

![Chargeback: cost grouped by a Kubernetes label](docs/images/chargeback.webp)

These come from a small test cluster, so the amounts are small and the data is a few days old.

## Quick start

Requires Argo CD 3.4+, OpenCost 1.121.1+ and Helm 3.8+.

```bash
helm install argocd-cost-intelligence \
  oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence \
  --version 0.1.0 \
  --namespace argocd-cost-intel --create-namespace \
  --set namespace.create=false \
  --set postgres.auth.password=<your-secure-password> \
  --wait --timeout 10m
```

A post-install hook registers the extension and the notification webhook in Argo CD.

On Argo CD 3.5 the extension is denied to every user until a policy allows it. The hook allows the built-in `admin` role in a key of its own, `policy.cost-intel.csv` in `argocd-rbac-cm`, and leaves your `policy.csv` alone. To give other roles access, see [Grant access in Argo CD](docs/installation.md#grant-access-in-argo-cd).

Sync an application, wait a few seconds, and open its **Cost Intelligence** tab. Settings are in [docs/configuration.md](docs/configuration.md). The [installation guide](docs/installation.md) covers the prerequisites, the OpenCost URL, who sees cluster-wide costs, upgrading and troubleshooting.

## How it works

```
Argo CD sync-succeeded event
  → Argo CD Notifications Controller
  → POST /api/cost-analysis/on-sync  (returns 202 immediately)
  → Background goroutine:
      1. Wait 5 s for Kubernetes metrics to propagate
      2. GET OpenCost /allocation?window=24h for the namespace and its pods
      3. Keep the pods of the controllers Argo CD lists for the app, and project both onto
         a 30-day month from the hours OpenCost has data for (24 h → × 30)
      4. Upsert into PostgreSQL cost_actuals table
  → REST API serves stored results to the UI plugin
```

## Cloud service costs (Infracost integration)

OpenCost measures what runs in Kubernetes: CPU, memory and storage. It can't see cloud services such as RDS, ElastiCache or S3. To cover those, the plugin accepts cost estimates from [Infracost](https://www.infracost.io/) (or a compatible tool) that your CI pipeline pushes to it.

Add one step to your CI workflow after `terraform plan`:

```bash
infracost breakdown --format json --path ./terraform > /tmp/infracost.json

curl -s -X POST \
  "https://<argocd-host>/extensions/cost-intel/api/cost-analysis/<app-name>/cloud-estimate?source=infracost" \
  -H "Authorization: Bearer $ARGOCD_TOKEN" \
  -H "Argocd-Application-Name: <app-namespace>:<app-name>" \
  -H "Argocd-Project-Name: <project>" \
  -H "Content-Type: application/json" \
  --data @/tmp/infracost.json
```

`ARGOCD_TOKEN` is an Argo CD token (for example a project role token) allowed to `get` the application and to `invoke` the `cost-intel` extension. `<app-namespace>` is the namespace of the Application resource, usually `argocd`.

The estimate is stored next to the OpenCost data and shown as a Full-Stack Monthly Cost card: Kubernetes compute, cloud services and the total. The card warns when the estimate is more than 7 days old, and it's hidden when nothing has been pushed. Estimates are keyed by `source`, so another tool can be added later without changing storage.

## Rightsizing (Vertical Pod Autoscaler and Goldilocks)

If your cluster runs the [Vertical Pod Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler), alone or through [Goldilocks](https://goldilocks.docs.fairwinds.com/), the plugin turns its recommendations into advice: which requests to set for each container, and what that saves or costs per month, priced with OpenCost. It only reads recommendation-only VPAs (`updateMode: "Off"`) and never creates or changes one. You install both tools separately. Without them, rightsizing stays hidden. See [Optional: rightsizing](docs/installation.md#optional-rightsizing).

## Prometheus metrics

An exporter publishes what each Application costs per hour. It labels the metrics like Argo CD's `argocd_app_info`, so the two join on `namespace` and `name`. Each API replica also reports its own health. The chart serves both on a separate port that only Prometheus can reach, and it ships recording rules and alerts. See [docs/metrics.md](docs/metrics.md).

## API

The API is called through Argo CD's extension proxy (`/extensions/cost-intel/...`), which forwards a request only when the caller may `get` the Application named in its `Argocd-Application-Name` (`<namespace>:<name>`) and `Argocd-Project-Name` headers. The backend then decides what that Application lets the caller see:

- **Application endpoints** (`/{appName}/...`) answer only when `{appName}` is the application in the header, and return that application's data: the header's namespace tells apart same-named Applications in different namespaces (apps-in-any-namespace).
- **Cluster-wide endpoints** (`/namespaces`, `/namespace/{ns}/...`, `/chargeback`, `/chargeback/labels`, `/cluster/overhead`, `/waste`) answer only requests naming the gate application: `CLUSTER_SCOPE_APP` when set, otherwise the Argo CD Application that installed Cost Intelligence, or, when Helm installed it, the `cost-intelligence-access` Application the chart creates. `CLUSTER_SCOPE_APP=off` turns them off. Grant `applications, get` on the gate application to the users who should see every namespace.
- `/config` and `/data-quality` reveal nothing about a particular application and answer any proxied request.

Anything else without the header gets `403`. `/health`, `/ready` (Kubernetes probes) and `/on-sync` (Argo CD Notifications) are not proxied and need no header. `/on-sync` instead requires `Authorization: Bearer <WEBHOOK_SECRET>` and answers `401` without it; the chart generates the secret and gives it to Argo CD Notifications. `/on-sync` also rejects a notification without `applicationNamespace` (`{{.app.metadata.namespace}}`) with `400`, because its costs could never be read back. The headers can only be trusted if nothing but `argocd-server` can reach the service, and the chart's NetworkPolicy takes care of that.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness probe |
| GET | `/ready` | Readiness probe |
| POST | `/api/cost-analysis/on-sync` | Webhook from Argo CD Notifications; returns 202 |
| GET | `/api/cost-analysis/{appName}/actual` | Latest cost record; includes `cloudEstimate` when available |
| POST | `/api/cost-analysis/{appName}/cloud-estimate?source=infracost` | Push IaC cost estimate from CI (raw `infracost breakdown --format json` body) |
| GET | `/api/cost-analysis/{appName}/trend?days=N` | Cost history + statistics (max 365 days) |
| GET | `/api/cost-analysis/{appName}/daily?days=N` or `?from=YYYY-MM-DD&to=YYYY-MM-DD` | Day-by-day cost time series |
| GET | `/api/cost-analysis/{appName}/orphaned-pods` | Orphaned pods in the application's deployment namespace |
| GET | `/api/cost-analysis/{appName}/rightsizing` | Vertical Pod Autoscaler recommendations for the application's workloads, per container, priced from OpenCost |
| GET | `/api/cost-analysis/namespace/{ns}/summary?days=N` | Average daily cost of a deployment namespace (each day counted once) |
| GET | `/api/cost-analysis/namespace/{ns}/apps` | Latest cost record per application in a namespace |
| GET | `/api/cost-analysis/namespace/{ns}/orphaned-pods` | Pods with zero CPU/RAM usage (waste candidates) |
| GET | `/api/cost-analysis/namespaces` | All deployment namespaces with aggregate cost |
| GET | `/api/cost-analysis/chargeback?label=<key>` | Cost grouped by a Kubernetes label key |
| GET | `/api/cost-analysis/chargeback/labels?namespace=<ns>` | Label keys pods carry, for the Chargeback picker (OpenCost 1.120.3+) |
| GET | `/api/cost-analysis/cluster/overhead` | Control-plane fee (EKS/GKE/AKS) from OpenCost |
| GET | `/api/cost-analysis/waste` | Applications on this cluster ranked by the idle cost of their own pods |
| GET | `/api/cost-analysis/data-quality` | OpenCost pricing source: `real` / `estimated` / `unknown` |
| GET | `/api/cost-analysis/config` | Which application gates cluster-wide data (`clusterScopeApp`, or `null`) |

## Documentation

| I want to... | Read |
|---|---|
| Install the plugin, upgrade it or remove it | [docs/installation.md](docs/installation.md) |
| Look up a Helm value or environment variable, or manage secrets | [docs/configuration.md](docs/configuration.md) |
| Fix a problem, understand the NetworkPolicy, back up the database | [docs/operations.md](docs/operations.md) |
| Scrape cost and health metrics, query them, alert on them | [docs/metrics.md](docs/metrics.md) |
| Build it, run the tests, try it locally | [docs/development.md](docs/development.md) |
| Understand how it works inside | [docs/architecture.md](docs/architecture.md) |
| Work on the Argo CD UI plugin | [ui/README.md](ui/README.md) |
| Contribute a change | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Report a security issue | [SECURITY.md](SECURITY.md) |

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first; commits need a DCO sign-off (`git commit -s`). Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md), and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
