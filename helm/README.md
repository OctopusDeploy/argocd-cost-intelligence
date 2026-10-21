# ArgoCD Cost Intelligence Helm Chart

Measures what each Argo CD application costs: after every sync it queries [OpenCost](https://www.opencost.io), stores the result in PostgreSQL and shows it in a **Cost Intelligence** tab in the Argo CD UI.

The chart deploys the backend API, the UI plugin, a bundled PostgreSQL StatefulSet and, optionally, a Prometheus exporter. A post-install hook registers the extension proxy, the UI bundle and the notification webhook in Argo CD.

> Early release (v0.x): chart values may change between minor versions. See the [CHANGELOG](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/CHANGELOG.md).

## Requirements

Argo CD 3.4 or 3.5, OpenCost 1.121.1+ reachable from the release namespace, Helm 3.8+. Details: [installation guide](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/docs/installation.md).

## Install

```bash
helm install argocd-cost-intelligence \
  oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence \
  --version 0.1.0 \
  --namespace argocd-cost-intel --create-namespace \
  --set namespace.create=false \
  --set postgres.auth.password=<your-secure-password> \
  --wait --timeout 10m
```

From a checkout, replace the OCI reference with `./helm`. Ready-made overlays: `values-minimal.yaml` (one replica, small storage) and `values-production.yaml` (autoscaling, resource limits, anti-affinity).

## Values you will probably set

| Key | Default | Purpose |
|---|---|---|
| `postgres.auth.password` | none | Database password; required on first install (or use `postgres.auth.existingSecret`) |
| `openCost.baseURL` | `http://opencost.opencost:9003` | OpenCost API URL |
| `costIntelligence.clusterScopeApp` | `""` | Argo CD Application (`<namespace>:<name>`) whose viewers see the cluster-wide Namespaces and Chargeback pages; empty turns them off |
| `argoCd.namespace` | `argocd` | Where Argo CD runs |
| `webhook.existingSecret` | `""` | Set when Argo CD or `helm template` renders the chart, so the webhook token stays stable |
| `networkPolicy.enabled` | `true` | Set `false` on CNIs that do not enforce NetworkPolicy (Flannel, k3d) |
| `metrics.enabled` | `true` | Prometheus metrics and exporter |

Every value, with defaults, is in [`values.yaml`](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/helm/values.yaml) and the [configuration reference](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/docs/configuration.md#helm-values).

## Documentation

- [Installation](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/docs/installation.md): prerequisites, install, upgrade, uninstall
- [Configuration](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/docs/configuration.md): every value, secrets, profiles
- [Operations](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/docs/operations.md): troubleshooting, NetworkPolicy, database backups
- [Metrics](https://github.com/OctopusDeploy/argocd-cost-intelligence/blob/main/docs/metrics.md): Prometheus metrics, rules and alerts

## License

Apache-2.0
