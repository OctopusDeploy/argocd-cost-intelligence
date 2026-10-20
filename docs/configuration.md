# Configuration

This page lists the Helm values, how to manage the secrets, the ready-made profiles, and the environment variables the backend reads. To install, see [installation.md](installation.md).

The defaults are in [`helm/values.yaml`](../helm/values.yaml). To print them for a released version:

```bash
helm show values oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence --version 0.1.0
```

## Helm values

Set values with `--set key=value` or with a values file (`-f`). If the file holds a password, keep it out of version control.

### Common settings

| Key | Default | Description |
|---|---|---|
| `postgres.auth.password` | `""` | Database password. Needed on first install unless `postgres.auth.existingSecret` is set. Later upgrades read it back from the Secret. |
| `openCost.baseURL` | `http://opencost.opencost:9003` | OpenCost API URL. |
| `costIntelligence.clusterScopeApp` | `""` | The Argo CD Application (`<namespace>:<name>`) whose viewers see the Namespaces and Chargeback pages and the control-plane fee. Empty turns them off. See [installation.md](installation.md#choose-your-settings). |
| `argoCd.namespace` | `argocd` | The namespace Argo CD runs in. The hook patches resources there. |
| `namespace.name` | `argocd-cost-intel` | The namespace for every chart resource. `helm --namespace` must match it. |
| `namespace.create` | `true` | Create that namespace. Set it to `false` when Helm (`--create-namespace`) or you create it first. With `true`, an existing namespace makes the install fail. |
| `networkPolicy.enabled` | `true` | Deploy the NetworkPolicies. Set it to `false` on CNIs that don't enforce them (Flannel, k3d). |

### Credentials

| Key | Default | Description |
|---|---|---|
| `postgres.auth.existingSecret` | `""` | A Secret in `namespace.name` with `username` and `password` keys. When set, the chart creates no PostgreSQL Secret and ignores `postgres.auth.password` and `username`. |
| `postgres.auth.username` | `cost_user` | Database user. |
| `postgres.auth.database` | `cost_intelligence` | Database name. |
| `webhook.existingSecret` | `""` | A Secret holding the sync webhook token under the key `token`, at least 16 characters. When empty, the chart generates `argocd-cost-intelligence-webhook` and keeps its token across upgrades. Set it when Argo CD or `helm template` renders the chart. |

### PostgreSQL

| Key | Default | Description |
|---|---|---|
| `postgres.enabled` | `true` | Deploy the bundled StatefulSet. For `false`, see [External PostgreSQL](#external-postgresql). |
| `postgres.image.repository` / `.tag` | `postgres` / `15-alpine` | |
| `postgres.sslMode` | `disable` | Connection mode (`DB_SSL_MODE`): `disable`, `require` or `verify-full`. Plaintext is fine only for the bundled database inside the cluster. |
| `postgres.service.port` | `5432` | |
| `postgres.persistence.size` | `20Gi` | Size of the PVC `postgres-pvc`. A PVC can only grow, and only if its StorageClass allows expansion. A smaller value makes the upgrade fail. |
| `postgres.persistence.storageClassName` | `""` | Empty means the cluster default. |
| `postgres.resources` | 100m / 256Mi requested, 500m / 1Gi limit | |

The chart can't keep the PVC on uninstall, and it has no switch to run without one.

### Backend

| Key | Default | Description |
|---|---|---|
| `costIntelligence.replicaCount` | `2` | Used only when autoscaling is off. |
| `costIntelligence.autoscaling.*` | enabled, 2 to 5 replicas, CPU 70%, memory 80% | The HorizontalPodAutoscaler. Needs metrics-server. |
| `costIntelligence.port` | `8080` | Listen port (`SERVER_PORT`), also the Service and NetworkPolicy port. |
| `costIntelligence.dailyHistoryDays` | `400` | How many days of each application's daily cost to keep in PostgreSQL (`DAILY_HISTORY_DAYS`), about one row per application per day. OpenCost only answers for the days its Prometheus keeps, often 15, so this history is what lets the Trends tab show months. `0` turns recording off. |
| `costIntelligence.vpa.enabled` | `true` | Read VPA recommendations for rightsizing (`VPA_ENABLED`) and give the ClusterRole read access to `verticalpodautoscalers`. See [installation.md](installation.md#optional-rightsizing). |
| `costIntelligence.database.maxOpenConns` / `maxIdleConns` | `25` / `5` | Connection pool size per replica. Keep replicas times `maxOpenConns` below PostgreSQL's `max_connections`. |
| `costIntelligence.server.readTimeout` / `writeTimeout` / `idleTimeout` | `10s` / `10s` / `120s` | HTTP server timeouts. Raise `writeTimeout` if large Trends queries get cut off. |
| `costIntelligence.image.tag` | chart `appVersion` | Pin a release. |
| `costIntelligence.resources` | 100m / 128Mi requested, 500m / 512Mi limit | |
| `openCost.timeout` | `10s` | How long to wait for OpenCost. |

### Argo CD integration and UI

| Key | Default | Description |
|---|---|---|
| `features.autoPatchArgoCd` | `true` | Run the post-install/post-upgrade hook that registers the extension, the webhook and the token with Argo CD. With `false` you do all of that yourself. |
| `ui.enabled` and `features.deployUI` | `true` | Deploy the UI plugin. Both must be true. |
| `ui.autoRegister` | `true` | Have the hook add the init container that loads the bundle into `argocd-server`. Needs `features.autoPatchArgoCd`. |
| `ui.port` / `ui.replicas` | `3000` / `1` | |
| `ui.image.tag` | chart `appVersion` | |
| `postInstallHook.image` | `alpine/k8s:1.32.3` | An image with `kubectl`, `jq` and `yq` (mikefarah/yq v4), for the hook Job. Pick a tag close to your cluster's Kubernetes minor version. |
| `postInstallHook.extensionInitImage` | `busybox:1.37.0` | The image of the `argocd-server` init container that downloads the UI bundle. It needs `sh` and `wget`. |
| `argoCd.rbac.grantAdmin` | `true` | Have the hook write `policy.cost-intel.csv` in `argocd-rbac-cm`, so `role:admin` can open the extension. With `false` the hook removes that key, unless you changed it. It never touches `policy.csv`. |

### Metrics

| Key | Default | Description |
|---|---|---|
| `metrics.enabled` | `true` | Serve `/metrics` from the API replicas (`METRICS_PORT`) and deploy the exporter. `false` turns both off. |
| `metrics.port` | `9102` | The metrics port. It's separate from the API port, which Prometheus must never reach, and must differ from `costIntelligence.port`. |
| `metrics.podAnnotations` | `true` | Add `prometheus.io/*` scrape annotations to the API and exporter pods. |
| `metrics.allowedFrom` | the `monitoring` namespace | NetworkPolicy `from` entries that may reach the metrics port, and only that port. An empty list admits nobody. Anyone who can query that Prometheus sees every Application's cost. |
| `metrics.exporter.enabled` | `true` | Deploy the exporter: one replica, `Recreate`, the backend image with `MODE=exporter`. |
| `metrics.exporter.interval` | `5m` | How often every Application is measured (`EXPORTER_INTERVAL`), at least `1m`. It costs one OpenCost query per namespace. |
| `metrics.exporter.resources` | 20m / 64Mi requested, 200m / 256Mi limit | |
| `metrics.serviceMonitor.enabled` / `.interval` / `.labels` | `false` / `1m` / `{}` | ServiceMonitors for the Prometheus Operator, one for the API and one for the exporter. `labels` are the labels your Prometheus selects by, such as `release: kube-prometheus-stack`. |
| `metrics.prometheusRule.enabled` / `.labels` | `false` / `{}` | A PrometheusRule built from `helm/files/prometheus-rules.yaml`. |

The metrics themselves, with queries and alerts, are in [metrics.md](metrics.md).

### Networking and scheduling

| Key | Default | Description |
|---|---|---|
| `networkPolicy.probeCIDRs` | `[]` | Node address ranges to admit, for CNIs that block kubelet probes. See [NetworkPolicy](operations.md#networkpolicy). |
| `ingress.enabled` | `false` | Create an Ingress for the backend. This bypasses Argo CD's authorization and the NetworkPolicy blocks it, so leave it off. |
| `nodeSelector`, `affinity`, `tolerations` | empty | Applied to the backend, exporter and PostgreSQL pods. |
| `security.podSecurityContext` / `security.containerSecurityContext` | non-root uid 1000, read-only root filesystem, all capabilities dropped | For the backend, exporter and UI pods. PostgreSQL has its own under `postgres.`. |
| `healthChecks.livenessProbe` / `.readinessProbe` | enabled, 10s / 5s period | For the backend and PostgreSQL probes. |
| `commonLabels`, `commonAnnotations` | a `part-of` label, no annotations | Added to every resource (the annotations go on the pods). |
| `rbac.*`, `serviceAccount.*` | created, named `argocd-cost-intelligence` | |

## Secrets

### PostgreSQL credentials

The simplest way is to let the chart manage them. Pass the password on first install (`--set postgres.auth.password="$(openssl rand -base64 32)"`). The chart writes it to the Secret `postgres-credentials` and reuses that Secret on every upgrade, so later `helm upgrade` runs don't need the password.

For production and GitOps, create the Secret yourself, outside Helm:

```bash
kubectl create namespace argocd-cost-intel

kubectl create secret generic postgres-credentials \
  --namespace argocd-cost-intel \
  --from-literal=username=cost_user \
  --from-literal=password="$(openssl rand -base64 32)"

helm install argocd-cost-intelligence \
  oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence \
  --version 0.1.0 --namespace argocd-cost-intel \
  --set namespace.create=false \
  --set postgres.auth.existingSecret=postgres-credentials \
  --wait --timeout 10m
```

The bundled PostgreSQL only applies the password when it first initialises its volume. If you rotate the Secret later, the database user's password doesn't change. Change both (`ALTER USER` inside `postgres-0`).

### Webhook token

Argo CD Notifications has to send a bearer token to `POST /api/cost-analysis/on-sync`, or the backend answers 401. The chart generates the token and keeps it in the Secret `argocd-cost-intelligence-webhook` (key `token`). The hook copies it into `argocd-notifications-secret` as `cost-intel-webhook-token`. The backend reads it only at startup. If Argo CD or `helm template` renders the chart, use `webhook.existingSecret`; see [installation.md](installation.md#install-with-argo-cd-or-helm-template), which also covers rotating the token.

## Profiles

The profile files ship in the chart but aren't part of the OCI install path. To get them:

```bash
helm pull oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence --version 0.1.0 --untar
# then: -f argocd-cost-intelligence/values-minimal.yaml
```

| Profile | Backend | PostgreSQL | Notes |
|---|---|---|---|
| Default (`values.yaml`) | 2 to 5 replicas (HPA) | 20 GiB | |
| `values-minimal.yaml` | 1 replica, HPA off, smaller requests | 10 GiB | For evaluation and small clusters. |
| `values-production.yaml` | 3 to 10 replicas (HPA), larger requests, pod anti-affinity | 100 GiB on StorageClass `fast-ssd` | A starting point. It also sets a `nodeSelector` (`workload: platform-services`) and a placeholder Ingress block, so read it and change the StorageClass before you use it. |

Every profile still needs the same `postgres.auth.*` and `namespace.create=false` settings as a default install.

## External PostgreSQL

Set `postgres.enabled=false` to skip the bundled StatefulSet, PVC, Service and NetworkPolicy. Then provide `postgres.auth.existingSecret` (with `username` and `password`), `postgres.auth.database`, and `postgres.sslMode=require` or `verify-full`.

The chart has no value for the database host. The backend and exporter always connect to `postgres.<namespace.name>.svc.cluster.local` on `postgres.service.port`, so make that name resolve to your database. An ExternalName Service called `postgres` in the release namespace does it. With `verify-full`, the server certificate has to match that name.

## Environment variables

The backend reads its settings from environment variables. With Helm you set chart values instead, and the chart sets the variables. The last column names the chart value where there is one. `DB_PASSWORD` and `WEBHOOK_SECRET` are required, and the chart fills in both. [`.env.example`](../.env.example) is a template for running the backend without Helm.

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP server port. |
| `MODE` | `api` | `api` serves the REST API. `exporter` exports every Argo CD Application's cost to Prometheus (see [metrics.md](metrics.md)). |
| `METRICS_PORT` | `9102` | The port that serves Prometheus `/metrics`, apart from the API port. `0` turns metrics off (API mode only). Helm: `metrics.port`, `metrics.enabled`. |
| `EXPORTER_INTERVAL` | `5m` | How often the exporter measures every Application, at least `1m`. Helm: `metrics.exporter.interval`. |
| `DB_HOST` | `localhost` | PostgreSQL host. |
| `DB_PORT` | `5432` | PostgreSQL port. |
| `DB_USER` | `cost_user` | Database user. |
| `DB_PASSWORD` | required | Database password. |
| `DB_NAME` | `cost_intelligence` | Database name. |
| `DB_SSL_MODE` | `disable` | PostgreSQL SSL mode (`disable`, `require` or `verify-full`). |
| `DB_MAX_OPEN_CONNS` | `25` | Maximum open connections per replica. Helm: `costIntelligence.database.maxOpenConns`. |
| `DB_MAX_IDLE_CONNS` | `5` | Maximum idle connections per replica. Helm: `costIntelligence.database.maxIdleConns`. |
| `SERVER_READ_TIMEOUT` / `SERVER_WRITE_TIMEOUT` / `SERVER_IDLE_TIMEOUT` | `10s` / `10s` / `120s` | HTTP server timeouts. Helm: `costIntelligence.server.*`. |
| `OPENCOST_BASE_URL` | `http://opencost:9003` | OpenCost API endpoint. |
| `OPENCOST_TIMEOUT` | `10s` | How long to wait for OpenCost. |
| `CLUSTER_SCOPE_APP` | empty (off) | The Argo CD Application (`<namespace>:<name>`) whose viewers may see cluster-wide cost data. Helm: `costIntelligence.clusterScopeApp`. |
| `DAILY_HISTORY_DAYS` | `400` | Days of each application's daily cost to keep in PostgreSQL, so the Trends tab can show 6 months or a year when OpenCost's Prometheus keeps less. `0` turns recording off. Helm: `costIntelligence.dailyHistoryDays`. |
| `VPA_ENABLED` | `true` | Read Vertical Pod Autoscaler recommendations (from Goldilocks or any VPA) for rightsizing advice. The service has to run in the cluster with permission to list `verticalpodautoscalers`. Without that, rightsizing stays hidden. Helm: `costIntelligence.vpa.enabled`. |
| `WEBHOOK_SECRET` | required | The bearer token Argo CD Notifications sends to `/on-sync`, at least 16 characters. The chart generates it (or reads `webhook.existingSecret`) and copies it into `argocd-notifications-secret`. |
