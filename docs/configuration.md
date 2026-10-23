# Configuration

This page lists the Helm values, how to manage the secrets, the ready-made profiles, and the environment variables for running without Helm. To install, see [Installation](installation.md).

The defaults are in [`helm/values.yaml`](../helm/values.yaml). To print them for a released version, with `<chart-version>` taken from the [Releases page](https://github.com/OctopusDeploy/argocd-cost-intelligence/releases):

```bash
helm show values oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence --version <chart-version>
```

## Helm values

Set values with `--set key=value` or with a values file (`-f`). If the file holds a password, keep it out of version control. The chart checks the type of each value: a value of the wrong type, such as a word for a port, stops `helm install` and `helm template` (which Argo CD runs) with an error naming the key. Keys it doesn't know are ignored.

### Common settings

| Key | Default | Description |
|---|---|---|
| `postgres.auth.password` | `""` | Database password. Needed on first install unless `postgres.auth.existingSecret` is set. Later upgrades read it back from the Secret. |
| `openCost.baseURL` | `http://opencost.opencost:9003` | OpenCost API URL. |
| `costIntelligence.clusterScopeApp` | `""` | The Argo CD Application (`<namespace>:<name>`) whose viewers see the Namespaces, Idle spend and Chargeback pages and the control-plane fee. Empty uses the Application that installed the chart, or else the access Application below. `off` turns them off. See [Choose your settings](installation.md#choose-your-settings). |
| `costIntelligence.accessApplication.create` | `true` | When Helm itself installs the chart and `clusterScopeApp` is empty, create an Argo CD Application to gate cluster-wide costs, in an AppProject that permits no resources. It deploys nothing. It is not created when Argo CD renders the chart, since Argo CD's own Application is the gate then. |
| `costIntelligence.accessApplication.name` / `.project` | `cost-intelligence-access` | Names of that Application and its AppProject, in `argoCd.namespace`. |
| `argoCd.namespace` | `argocd` | The namespace Argo CD runs in. The hook patches resources there. |
| `namespace.name` | `argocd-cost-intel` | The namespace for every chart resource. `helm --namespace` must match it. |
| `namespace.create` | `true` | Create that namespace. Set it to `false` when Helm (`--create-namespace`) or you create it first. With `true`, an existing namespace makes the install fail. |
| `networkPolicy.enabled` | `true` | Deploy the NetworkPolicies. Set it to `false` on CNIs that don't enforce them, such as Flannel on its own. k3s and k3d do enforce them, unless k3s was started with `--disable-network-policy`. |

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
| `postgres.image.repository` / `.tag` / `.pullPolicy` | `postgres` / `15-alpine` / `IfNotPresent` | |
| `postgres.replicaCount` | `1` | Leave it at 1: the bundled PostgreSQL is one instance on one volume. |
| `postgres.sslMode` | `disable` | Connection mode (`DB_SSL_MODE`): `disable`, `require` or `verify-full`. Plaintext is fine only for the bundled database inside the cluster. |
| `postgres.service.port` | `5432` | |
| `postgres.persistence.size` | `20Gi` | Size of the PVC `postgres-pvc`. A PVC can only grow, and only if its StorageClass allows expansion. A smaller value makes the upgrade fail. |
| `postgres.persistence.storageClassName` | `""` | Empty means the cluster default. |
| `postgres.resources` | 100m / 256Mi requested, 500m / 1Gi limit | |
| `postgres.podSecurityContext` / `.containerSecurityContext` | `fsGroup` 999, no privilege escalation | PostgreSQL starts as root and switches to uid 999, and writes to its root filesystem, so it can't use the backend's contexts. |

The chart can't keep the PVC on uninstall, and it has no switch to run without one.

### Backend

| Key | Default | Description |
|---|---|---|
| `costIntelligence.replicaCount` | `2` | Used only when autoscaling is off. |
| `costIntelligence.autoscaling.*` | enabled, 2 to 5 replicas, CPU 70%, memory 80% | The HorizontalPodAutoscaler. Needs metrics-server. |
| `costIntelligence.port` | `8080` | Listen port (`SERVER_PORT`), also the Service and NetworkPolicy port. |
| `costIntelligence.dailyHistoryDays` | `400` | How many days of each application's daily cost to keep in PostgreSQL (`DAILY_HISTORY_DAYS`), about one row per application per day. OpenCost only answers for the days its Prometheus keeps, often 15, so this history is what lets the Trends tab show months. `0` turns recording off. |
| `costIntelligence.backfillConcurrency` | `4` | How many namespaces to measure at once when the service starts and finds Applications without a cost record (`BACKFILL_CONCURRENCY`). Replicas take turns, so OpenCost never sees more than this many queries from the backfill. `0` turns it off, and applications then show costs only after their next sync. |
| `costIntelligence.pruneRemoteCost` | `false` | Delete the cost records and daily history stored for Applications that deploy to another cluster (`PRUNE_REMOTE_COST`). It can't be undone, and an Application that targets this cluster by its API server URL counts as remote; see [Limits and requirements](limits-and-requirements.md#only-this-cluster-is-measured). |
| `costIntelligence.vpa.enabled` | `true` | Read VPA recommendations for rightsizing (`VPA_ENABLED`) and give the ClusterRole read access to `verticalpodautoscalers`. See [Optional: rightsizing](installation.md#optional-rightsizing). |
| `costIntelligence.logLevel` | `info` | `debug` also logs each OpenCost request URL and response body, in the API and the exporter (`LOG_LEVEL`). These name every namespace and its cost; turn it on only while troubleshooting. |
| `costIntelligence.database.maxOpenConns` / `maxIdleConns` | `25` / `5` | Connection pool size per replica. Keep replicas times `maxOpenConns` below PostgreSQL's `max_connections`. |
| `costIntelligence.server.readTimeout` / `writeTimeout` / `idleTimeout` | `10s` / `10s` / `120s` | HTTP server timeouts. Raise `writeTimeout` if large Trends queries get cut off. |
| `costIntelligence.image.repository` / `.tag` / `.pullPolicy` | `ghcr.io/octopusdeploy/argocd-cost-intelligence` / chart `appVersion` / `IfNotPresent` | The backend image, which the exporter runs too. Set `tag` to pin a release. |
| `costIntelligence.resources` | 100m / 128Mi requested, 500m / 512Mi limit | |
| `openCost.timeout` | `10s` | How long to wait for OpenCost. |

### Argo CD integration and UI

| Key | Default | Description |
|---|---|---|
| `features.autoPatchArgoCd` | `true` | Run the post-install/post-upgrade hook that registers the extension, the webhook and the token with Argo CD. With `false` you do all of that yourself. |
| `ui.enabled` and `features.deployUI` | `true` | Deploy the UI plugin. Both must be true. |
| `ui.autoRegister` | `true` | Have the hook add the init container that loads the bundle into `argocd-server`. Needs `features.autoPatchArgoCd`. |
| `ui.port` / `ui.replicas` | `3000` / `1` | |
| `ui.image.repository` / `.tag` / `.pullPolicy` | `ghcr.io/octopusdeploy/argocd-cost-intelligence-ui` / chart `appVersion` / `IfNotPresent` | |
| `ui.resources` | 50m / 64Mi requested, 200m / 256Mi limit | |
| `postInstallHook.image` | `alpine/k8s:1.32.3` | An image with `kubectl`, `jq` and `yq` (mikefarah/yq v4), for the hook Job. Pick a tag close to your cluster's Kubernetes minor version. |
| `postInstallHook.extensionInitImage` | `busybox:1.37.0` | The image of the `argocd-server` init container that downloads the UI bundle. It needs `sh` and `wget`. |
| `argoCd.rbac.grantAdmin` | `true` | Have the hook write `policy.cost-intel.csv` in `argocd-rbac-cm`, so `role:admin` can open the extension. With `false` the hook removes that key, unless you changed it. It never touches `policy.csv`. |

### Metrics

| Key | Default | Description |
|---|---|---|
| `metrics.enabled` | `true` | Serve `/metrics` from the API replicas (`METRICS_PORT`) and deploy the exporter. `false` turns both off. |
| `metrics.port` | `9102` | The metrics port. It's separate from the API port, which Prometheus must never reach, and must differ from `costIntelligence.port`. |
| `metrics.podAnnotations` | `true` | Add `prometheus.io/*` scrape annotations to the API and exporter pods. |
| `metrics.allowedFrom` | the `monitoring` namespace | NetworkPolicy `from` entries that may reach the metrics port, and only that port. Setting it replaces the default, so list every namespace a Prometheus scrapes from; any other Prometheus sees the targets as down. An empty list admits nobody. Anyone who can query that Prometheus sees every Application's cost. See [Security](metrics.md#security). |
| `metrics.exporter.enabled` | `true` | Deploy the exporter: one replica, `Recreate`, the backend image with `MODE=exporter`. |
| `metrics.exporter.interval` | `5m` | How often every Application is measured (`EXPORTER_INTERVAL`), at least `1m`. It costs one OpenCost query per namespace. |
| `metrics.exporter.resources` | 20m / 64Mi requested, 200m / 256Mi limit | |
| `metrics.serviceMonitor.enabled` / `.interval` / `.labels` | `false` / `1m` / `{}` | ServiceMonitors for the Prometheus Operator, one for the API and one for the exporter. `labels` are the labels your Prometheus selects by, such as `release: kube-prometheus-stack`. |
| `metrics.prometheusRule.enabled` / `.labels` | `false` / `{}` | A PrometheusRule built from `helm/files/prometheus-rules.yaml`. |

The metrics themselves, with queries and alerts, are in [Prometheus metrics](metrics.md).

### Networking and scheduling

| Key | Default | Description |
|---|---|---|
| `networkPolicy.probeCIDRs` | `[]` | Node address ranges to admit, for CNIs that block kubelet probes. See [NetworkPolicy](operations.md#networkpolicy). |
| `ingress.enabled` | `false` | Create an Ingress for the backend. This bypasses Argo CD's authorization and the NetworkPolicy blocks it, so leave it off. |
| `ingress.className` / `.annotations` / `.hosts` / `.tls` | none, one example host | The Ingress class, its annotations, the hosts with their paths and path types, and TLS entries with `hosts` and `secretName`. |
| `nodeSelector`, `affinity`, `tolerations` | empty | Applied to the backend, exporter and PostgreSQL pods. |
| `security.podSecurityContext` / `security.containerSecurityContext` | non-root uid 1000, read-only root filesystem, all capabilities dropped | For the backend, exporter and UI pods. PostgreSQL has its own under `postgres.`. |
| `healthChecks.startupProbe` | enabled, 5s period, 30 failures | For the backend and the exporter. The liveness probe waits for it, so a database that is slow to come up doesn't restart the pod. |
| `healthChecks.livenessProbe` / `.readinessProbe` | enabled, 10s / 5s period | For the backend and PostgreSQL probes. |
| `commonLabels`, `commonAnnotations` | a `part-of` label, no annotations | Added to every resource (the annotations go on the pods). |
| `rbac.create` / `.roleName` / `.roleBindingName` | `true` / `argocd-cost-intelligence` | Create the ClusterRole and ClusterRoleBinding with these names, and a Role to read the service's own Deployment. |
| `serviceAccount.create` / `.name` | `true` / `argocd-cost-intelligence` | The backend's and the exporter's ServiceAccount. With `create: false`, the chart uses the named account, or `default` when the name is empty. |

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
  --version <chart-version> --namespace argocd-cost-intel \
  --set namespace.create=false \
  --set postgres.auth.existingSecret=postgres-credentials \
  --wait --timeout 10m
```

The bundled PostgreSQL only applies the password when it first initialises its volume. If you rotate the Secret later, the database user's password doesn't change, and the backend logs `password authentication failed`. Change both (`ALTER USER` inside `postgres-0`), or set the Secret back to the original value. Starting over with an empty database means uninstalling, which deletes the PVC (see [Uninstall](installation.md#uninstall)).

### Webhook token

Argo CD Notifications has to send a bearer token to `POST /api/cost-analysis/on-sync`, or the backend answers 401. The chart generates the token and keeps it in the Secret `argocd-cost-intelligence-webhook` (key `token`). The hook copies it into `argocd-notifications-secret` as `cost-intel-webhook-token`. The backend reads it only at startup. If Argo CD or `helm template` renders the chart, use `webhook.existingSecret`; see [Install with Argo CD or helm template](installation.md#install-with-argo-cd-or-helm-template), which also covers rotating the token.

## Profiles

The profile files ship in the chart but aren't part of the OCI install path. To get them:

```bash
helm pull oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence --version <chart-version> --untar
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

## Running without Helm

The chart sets these variables from its values, so you only need them when you run the image without the chart. [`.env.example`](../.env.example) is a template to start from, with the ones you are most likely to set.

| Variable | Default | Description |
|---|---|---|
| `MODE` | `api` | `api` serves the REST API. `exporter` exports every Argo CD Application's cost to Prometheus (see [Prometheus metrics](metrics.md)). |
| `SERVER_PORT` | `8080` | The API port. |
| `SERVER_READ_TIMEOUT` / `SERVER_WRITE_TIMEOUT` / `SERVER_IDLE_TIMEOUT` | `10s` / `10s` / `120s` | HTTP server timeouts. |
| `METRICS_PORT` | `9102` | The port that serves Prometheus `/metrics`, apart from the API port, so in API mode it must differ from `SERVER_PORT` and `0` turns metrics off. The exporter needs it. |
| `EXPORTER_INTERVAL` | `5m` | How often the exporter measures every Application, at least `1m`. Exporter mode only. |
| `WEBHOOK_SECRET` | required | The bearer token Argo CD Notifications sends to `/on-sync`, at least 16 characters. Not needed in exporter mode. |
| `DB_HOST` | `localhost` | PostgreSQL host. The chart has no value for it. |
| `DB_PORT` | `5432` | PostgreSQL port. |
| `DB_USER` | `cost_user` | Database user. |
| `DB_PASSWORD` | required | Database password. |
| `DB_NAME` | `cost_intelligence` | Database name. |
| `DB_SSL_MODE` | `disable` | PostgreSQL SSL mode: `disable`, `require` or `verify-full`. |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | `25` / `5` | Connection pool size of each process. |
| `OPENCOST_BASE_URL` | `http://opencost:9003` | OpenCost API endpoint, `http` or `https`. The chart's default is `http://opencost.opencost:9003`. |
| `OPENCOST_TIMEOUT` | `10s` | How long to wait for each OpenCost request. |
| `CLUSTER_SCOPE_APP` | empty | The Argo CD Application (`<namespace>:<name>`) whose viewers may see cluster-wide cost data. Empty finds one and `off` turns it off; see [Choose your settings](installation.md#choose-your-settings). |
| `CLUSTER_ACCESS_APP` | empty | The fallback Application (`<namespace>:<name>`) when none installed the service; see [Authorization](architecture.md#authorization). |
| `POD_NAMESPACE` / `DEPLOYMENT_NAME` | empty | The service's own Deployment, whose tracking annotation names the Application that installed it. |
| `ARGOCD_NAMESPACE` | `argocd` | Where Argo CD runs. A tracking annotation that names an Application without a namespace means one here. |
| `DAILY_HISTORY_DAYS` | `400` | How many days of each application's daily cost to keep; `0` turns recording off. |
| `BACKFILL_CONCURRENCY` | `4` | How many namespaces to measure at once when the service starts; `0` turns it off. See [Measuring at install](architecture.md#when-the-service-starts). |
| `PRUNE_REMOTE_COST` | `false` | Delete stored cost for Applications that deploy to another cluster, which can't be undone; see [Limits and requirements](limits-and-requirements.md#only-this-cluster-is-measured). |
| `VPA_ENABLED` | `true` | Read Vertical Pod Autoscaler recommendations for rightsizing; see [Optional: rightsizing](installation.md#optional-rightsizing). |
| `LOG_LEVEL` | `info` | `info` or `debug`, in any case. `debug` also logs each OpenCost request URL and response body, which name every namespace and its cost. |

When a `SERVER_`, `DB_` or `OPENCOST_` variable isn't set, the service reads the same name without the prefix, so a `USER` or `HOST` already in the environment takes the place of `DB_USER` or `DB_HOST`, and a `PORT` takes the place of both `SERVER_PORT` and `DB_PORT`. Set the ones you rely on.
