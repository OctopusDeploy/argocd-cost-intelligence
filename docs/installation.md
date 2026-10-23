# Installation

This page covers installing ArgoCD Cost Intelligence with Helm, checking that it works, upgrading and removing it. Chart settings are in [Configuration](configuration.md). If something goes wrong, see [Troubleshooting](operations.md#troubleshooting).

## What the chart installs

Everything goes into the `argocd-cost-intel` namespace, which the chart creates:

| Component | Kind | What it does |
|---|---|---|
| `argocd-cost-intelligence` | Deployment (HPA, 2 to 5 replicas), Service on 8080 | REST API and sync webhook |
| `argocd-cost-intelligence-exporter` | Deployment, 1 replica | Publishes application cost as Prometheus metrics |
| `argocd-cost-intelligence-ui` | Deployment, Service on 3000 | Serves the React bundle that `argocd-server` loads as an extension |
| `postgres` | StatefulSet, headless Service, PVC `postgres-pvc` | Cost history. You can use an external database instead, see [External PostgreSQL](configuration.md#external-postgresql) |

These names assume the release is called `argocd-cost-intelligence`. With another release name, the Deployments and Services are named after it.

The chart also runs a post-install/post-upgrade hook Job that changes Argo CD itself. It runs on every `helm install` and `helm upgrade`:

| Where | What changes |
|---|---|
| `argocd-cm` | `extension.config`: the extension proxy, pointing at the backend Service |
| `argocd-cmd-params-cm` | `server.enable.proxy.extension: "true"` |
| `argocd-notifications-cm` | `service.webhook.cost-intel`, `trigger.cost-intel-on-sync-succeeded` and `template.cost-intel-sync-succeeded`, and one entry in `subscriptions` |
| `argocd-rbac-cm` | `policy.cost-intel.csv`, which lets `role:admin` open the extension (see [Grant access in Argo CD](#grant-access-in-argo-cd)) |
| `argocd-notifications-secret` | The key `cost-intel-webhook-token`, the bearer token the webhook sends |
| `argocd-server` Deployment | An init container, `cost-intel-extension`, and a volume that fetch the UI bundle from the UI Service |

The hook works with Argo CD's `install.yaml` and with its Helm chart, whatever the release is called. It only writes keys of its own, named after `cost-intel`, and removes the trigger and template 0.1.0 wrote if you haven't changed them, so your triggers, templates and `policy.csv` stay as they are. `subscriptions` is one key shared by every subscription: the hook changes it only when its entry is missing or 0.1.0's is still there, and keeps your entries and comments, but the list's formatting may change. If `argocd-notifications-cm` or `argocd-rbac-cm` doesn't exist, the hook skips it and logs that.

The hook then restarts `argocd-notifications-controller`, the UI Deployment and `argocd-server`, so the Argo CD UI reloads during an install or upgrade. When the hook succeeds it deletes its own ServiceAccount, ClusterRole and Job.

The hook doesn't change Kubernetes RBAC for Argo CD. The webhook is sent only for Applications the notifications controller can read. For Applications outside the Argo CD namespace ([apps in any namespace](https://argo-cd.readthedocs.io/en/stable/operator-manual/app-any-namespace/)), the Argo CD Helm chart already gives it a ClusterRole, unless `createClusterRoles` is false. With Argo CD's `install.yaml`, or with that value off, apply the notifications ClusterRole and binding from Argo CD's `examples/k8s-rbac/argocd-server-applications`; its binding names the `argocd` namespace, so change that if Argo CD runs elsewhere.

## Prerequisites

| You need | Notes |
|---|---|
| A Kubernetes cluster | Any cluster Argo CD supports. OpenCost measures only the cluster it runs in, so Cost Intelligence measures only the applications Argo CD deploys to this cluster. If Argo CD also manages other clusters, applications deployed there aren't measured, and their tab says so; see [Limits and requirements](limits-and-requirements.md#only-this-cluster-is-measured). |
| Argo CD 3.4 or 3.5 | The plugin uses the proxy extension feature, which the hook turns on. `argocd-notifications-controller` must be running in `argoCd.namespace`. |
| OpenCost 1.121.1 or later | The backend and exporter call it on port 9003. The default URL is `http://opencost.opencost:9003`. See [opencost.io/docs/install](https://www.opencost.io/docs/install). |
| Helm 3.8 or later | The chart is published as an OCI artifact. |
| Cluster-admin | The chart creates ClusterRoles, and the hook patches resources in the Argo CD namespace. |
| A default StorageClass | The bundled PostgreSQL claims a 20 GiB PVC. Without a default class, set `postgres.persistence.storageClassName`. |
| metrics-server | The backend HPA needs it. Without it, set `costIntelligence.autoscaling.enabled=false`. |

Supported and tested versions are in [Limits and requirements](limits-and-requirements.md#compatibility).

### Network

The backend and exporter must reach OpenCost on port 9003. The exporter and the hook also need the Kubernetes API.

The chart's NetworkPolicies only limit incoming traffic. The backend accepts `argocd-server` and `argocd-notifications-controller`, the UI pod accepts `argocd-server`, PostgreSQL accepts the backend and exporter, and Prometheus can reach the metrics port (9102). Outgoing traffic is not restricted by the chart, but it may be restricted in your cluster:

- If the OpenCost namespace limits incoming traffic, let pods from `argocd-cost-intel` reach port 9003.
- If the Argo CD namespace limits outgoing traffic, let `argocd-server` reach `argocd-cost-intel` on ports 8080 and 3000, and `argocd-notifications-controller` reach it on 8080.

[Operations](operations.md#networkpolicy) explains how the policies work and what to do if your CNI blocks kubelet probes.

> **Warning:** Clusters that use Flannel on its own don't enforce NetworkPolicy. Install with `--set networkPolicy.enabled=false` there. Otherwise the policies do nothing, and you'd think you were protected when you aren't. k3s and k3d use Flannel too, but k3s has its own policy controller, so they enforce NetworkPolicy unless k3s was started with `--disable-network-policy`.

### Images

The images are public on GHCR, so you don't need a pull secret: `ghcr.io/octopusdeploy/argocd-cost-intelligence:v0.1.0` and `ghcr.io/octopusdeploy/argocd-cost-intelligence-ui:v0.1.0`. The hook Job (`postInstallHook.image`, `alpine/k8s`) and the `argocd-server` init container (`postInstallHook.extensionInitImage`, `busybox:1.37.0`) pull images too. Mirror those if your cluster can't reach public registries. A replacement for the hook image needs `kubectl`, `jq` and `yq` (mikefarah/yq v4); one for the init container needs `sh` and `wget`.

## Install

### Try it without OpenCost

To look at the plugin before you have an OpenCost, the repository has a mock. It prices the pods that run in each namespace with made-up but stable prices, scaled up so small pods show larger amounts. Use it to try things out; the amounts mean nothing.

```bash
kubectl apply -f https://raw.githubusercontent.com/OctopusDeploy/argocd-cost-intelligence/main/deploy/mock-opencost.yaml
```

It runs as `opencost` in the `opencost` namespace, so the default OpenCost URL works. Remove it with `kubectl delete -f` on the same file, and install a real OpenCost before you rely on the numbers.

### Choose your settings

**PostgreSQL password.** The chart stores it in a Secret called `postgres-credentials`. Don't commit it. To manage that Secret yourself, see [PostgreSQL credentials](configuration.md#postgresql-credentials).

**OpenCost URL**, if it isn't `http://opencost.opencost:9003`: `--set openCost.baseURL=http://<service>.<namespace>.svc.cluster.local:9003`.

**Argo CD namespace**, if it isn't `argocd`: `--set argoCd.namespace=<namespace>`.

**Who sees cluster-wide costs.** The Namespaces, Idle spend and Chargeback pages show what every team spends. Argo CD's extension proxy can only check whether a viewer may `get` one Application, so one Application acts as the gate: anyone who can view it sees cluster-wide costs. You don't have to pick one:

- If an Argo CD Application installs Cost Intelligence, that Application is the gate. The service finds it from the tracking annotation Argo CD puts on its Deployment.
- If you install with `helm install`, as below, the chart creates an Application called `cost-intelligence-access` in the Argo CD namespace, in an AppProject of the same name that permits no resources. It deploys nothing and is never measured; it exists only to be the gate. Its chart is an empty one the service itself serves, so it needs no repository.

Who sees cluster-wide costs is then whoever your Argo CD RBAC lets `get` that Application. `role:admin` can, and so can `role:readonly` if it is your `policy.default`. To let a team in:

```csv
p, role:platform, applications, get, cost-intelligence-access/cost-intelligence-access, allow
```

To choose the gate yourself, pass any existing Application as `<namespace>:<name>`, where the namespace is the one the Application resource lives in (usually `argocd`); the chart then creates no access Application. To keep cluster-wide costs off for everyone, pass `off`:

```bash
--set costIntelligence.clusterScopeApp=argocd:<app-name>
```

Per-application costs work either way. You can change it later with `helm upgrade --reuse-values --set ...`.

### Run the install

The chart creates its own namespace (`namespace.create=true`), so tell Helm not to create it, or the two collide. Replace `<chart-version>` with the newest version on the [Releases page](https://github.com/OctopusDeploy/argocd-cost-intelligence/releases):

```bash
helm install argocd-cost-intelligence \
  oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence \
  --version <chart-version> \
  --namespace argocd-cost-intel --create-namespace \
  --set namespace.create=false \
  --set postgres.auth.password=<your-secure-password> \
  --wait --timeout 10m
```

`--wait` also waits for the hook, and the hook waits for the Argo CD rollouts it triggers. On a slow cluster that takes several minutes, so give it a generous timeout. `--namespace` must equal `namespace.name`.

Before you run it, know what the hook does to your existing configuration:

> **Warning:** If something reconciles `argocd-cm`, `argocd-notifications-cm`, `argocd-rbac-cm` or the `argocd-server` Deployment (Argo CD managing itself, for example), it will revert the hook's patches. In that case set `features.autoPatchArgoCd=false` and make the changes in your own source of truth. [Install when Git manages Argo CD](gitops-install.md) has every change, for Argo CD's Helm chart and for plain manifests or Kustomize.

### Grant access in Argo CD

Viewers need permission to invoke the extension, and `get` on an Application to see its tab (they usually have that already). On Argo CD 3.5 the extension is denied to everyone until a policy allows it, `admin` included. Without a rule the tab shows "401: Unauthorized extension request".

The hook allows the built-in `admin` role. It writes this line to `policy.cost-intel.csv`, a key of its own in `argocd-rbac-cm`:

```
p, role:admin, extensions, invoke, cost-intel, allow
```

Argo CD appends every `policy.<name>.csv` key to `policy.csv`, so your own rules aren't touched. To manage the rule yourself, install with `--set argoCd.rbac.grantAdmin=false`. The hook then removes its key, unless you've changed it.

Every other role needs its own rule. Don't add it to `policy.cost-intel.csv`, because the hook rewrites that key on every upgrade. Use a key of your own instead, such as `policy.cost-intel-viewers.csv`, or `policy.csv`. A merge patch that sets a new key leaves the others alone:

```bash
kubectl patch configmap argocd-rbac-cm -n argocd --type merge -p '{"data":{"policy.cost-intel-viewers.csv":"p, role:cost-viewer, extensions, invoke, cost-intel, allow\n"}}'
```

The people who may see cluster-wide costs also need `get` on the gate Application from above:

```
p, role:platform, applications, get, <project>/<app-name>, allow
```

If Argo CD's own Helm chart or GitOps manages `argocd-rbac-cm`, add your rules there.

Then assign the roles to your groups as you normally do.

### Install with Argo CD or helm template

If Git also manages Argo CD itself, follow [Install when Git manages Argo CD](gitops-install.md) instead. It covers this section too.

For an example Application, see [Step 2 of that guide](gitops-install.md#step-2-add-the-cost-intelligence-application). It sets `features.autoPatchArgoCd: false`, because in that guide Git holds Argo CD's settings and Step 3 changes them there. If you apply it with `kubectl`, Kubernetes may print a warning that `resources-finalizer.argocd.argoproj.io` should be "a domain-qualified finalizer name". It is harmless: the Application is created and the finalizer still works. That guide shows the full warning.

The chart generates the sync webhook's token and reads it back on `helm upgrade`, so it stays the same. Argo CD and `helm template` render without cluster access, so they'd generate a new token on every render. That restarts the backend each time and leaves the Application permanently out of sync. Create the Secret yourself instead:

```bash
kubectl create secret generic cost-intel-webhook -n argocd-cost-intel \
  --from-literal=token=$(openssl rand -hex 24)
```

Then set `webhook.existingSecret=cost-intel-webhook`. Give the database password the same treatment: use `postgres.auth.existingSecret`, or a values file that isn't in version control, because a render can't read the generated password back.

To rotate the webhook token, update the Secret and run the hook again (`helm upgrade`, or sync the chart). Then restart the backend, which reads the token only at startup:

```bash
kubectl rollout restart deployment/argocd-cost-intelligence -n argocd-cost-intel
```

## Check that it works

The hook Job is deleted when it succeeds, and most of its patch steps tolerate failure. So check the result rather than the Job:

```bash
kubectl get pods -n argocd-cost-intel      # backend, exporter, ui, postgres-0: all Running

# Argo CD was wired up
kubectl get cm argocd-cm -n argocd -o jsonpath='{.data.extension\.config}'
kubectl get cm argocd-cmd-params-cm -n argocd -o jsonpath='{.data.server\.enable\.proxy\.extension}'
kubectl get cm argocd-notifications-cm -n argocd -o jsonpath='{.data.service\.webhook\.cost-intel}'
kubectl get cm argocd-rbac-cm -n argocd -o jsonpath='{.data.policy\.cost-intel\.csv}'
kubectl get deploy -n argocd -l app.kubernetes.io/name=argocd-server \
  -o jsonpath='{.items[*].spec.template.spec.initContainers[*].name}'      # includes cost-intel-extension

# Backend health
kubectl port-forward svc/argocd-cost-intelligence 18080:8080 -n argocd-cost-intel &
curl -s http://localhost:18080/health      # {"status":"ok"}
```

Then open any Application in Argo CD. It has a **Cost Intelligence** tab. When the backend starts, it measures every application that has no cost yet, so most tabs show a cost within a few minutes, marked "Measured at install". If OpenCost has no data yet for an application's namespace, or doesn't answer, the backend tries again for about a quarter of an hour; after that the tab stays empty until the application's next successful sync.

Each sync measures the application again: Argo CD Notifications calls the webhook, the backend waits 5 seconds, asks OpenCost and stores the result, and the tab shows it within the UI's 30-second refresh. A new application takes a few minutes longer: its first sync creates its namespace, which OpenCost has no data for yet, so the backend measures it again after 1, 2, 4 and 8 minutes.

## Optional: rightsizing

Cost Intelligence can tell you which requests to set for each container and what that would save per month. It doesn't work this out itself. It reads the recommendations of the [Vertical Pod Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) (VPA) and prices them with OpenCost.

Without a VPA the rest of the plugin works as usual and rightsizing stays hidden. The chart doesn't install a VPA, and the service never creates or changes one. It only reads them.

You need three things:

1. **The VPA recommender in the cluster**, plus metrics-server. Follow the [VPA installation guide](https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/docs/installation.md) or use your provider's add-on (GKE Autopilot has it on by default). To check:
   ```bash
   kubectl get crd verticalpodautoscalers.autoscaling.k8s.io
   ```
2. **A `VerticalPodAutoscaler` in `Off` mode for each workload you want advice on.** In `Off` mode the VPA only recommends. In any other mode (`Initial`, `Recreate`, `Auto`, `InPlaceOrRecreate`) it sets requests itself, so the workload shows as managed by the VPA and gets no advice. You can let [Goldilocks](https://goldilocks.docs.fairwinds.com/installation/) (from Fairwinds, needs the VPA) create one for every workload in a labelled namespace:
   ```bash
   kubectl label namespace <namespace> goldilocks.fairwinds.com/enabled=true
   ```
   or write one yourself:
   ```yaml
   apiVersion: autoscaling.k8s.io/v1
   kind: VerticalPodAutoscaler
   metadata:
     name: web
     namespace: <namespace>
   spec:
     targetRef:
       apiVersion: apps/v1
       kind: Deployment
       name: web
     updatePolicy:
       updateMode: "Off"
   ```
3. **`costIntelligence.vpa.enabled`** left at its default of `true`. That lets the backend `get` and `list` `verticalpodautoscalers`.

What to expect:

- The VPA needs some usage history. Until it has a recommendation, the **Resources** tab says so. For its first day the recommendation is shown as **Not enough data yet**, with no requests to set and no saving, because minutes or hours of use miss the daily peak. After that, while the upper bound is still far above the target, the recommendation is marked low confidence, the upper bound is hidden and there's no memory-limit warning. A container that uses almost nothing gets the VPA's minimum, not a measured figure.
- Once there is a recommendation, **Actual costs** names the workloads to change and the monthly saving or cost, when it's at least $1. **Resources** shows each container's requests next to the VPA's range, and the requests to paste into your manifest.
- Only requests are suggested. Limits are left alone, except for a warning when a memory limit is below the most the VPA expects the container to need.
- The application must have synced since you installed or upgraded Cost Intelligence, so that its cost record lists the workloads it manages.

## Optional: cloud service costs

OpenCost measures what runs in Kubernetes. It can't see cloud services such as RDS, ElastiCache or S3. To cover those, the service accepts cost estimates from [Infracost](https://www.infracost.io/) (or a compatible tool) that your CI pipeline pushes to it through Argo CD.

Add one step to your CI workflow after `terraform plan`:

```bash
infracost breakdown --format json --path ./terraform > /tmp/infracost.json

curl -s --fail-with-body -X POST \
  "https://<argocd-host>/extensions/cost-intel/api/cost-analysis/<app-name>/cloud-estimate?source=infracost" \
  -H "Authorization: Bearer $ARGOCD_TOKEN" \
  -H "Argocd-Application-Name: <app-namespace>:<app-name>" \
  -H "Argocd-Project-Name: <project>" \
  -H "Content-Type: application/json" \
  --data @/tmp/infracost.json
```

`ARGOCD_TOKEN` is an Argo CD token (for example a project role token) allowed to `get` the application and to `invoke` the `cost-intel` extension. `<app-namespace>` is the namespace of the Application resource, usually `argocd`.

The service answers `200` with the estimate it stored. Each push replaces the application's previous estimate, and resources without a monthly cost are left out:

```json
{
  "source": "infracost",
  "totalMonthlyCost": 412.5,
  "currency": "USD",
  "resources": [
    { "name": "aws_db_instance.payments", "resourceType": "aws_db_instance", "monthlyCost": 380.1 }
  ],
  "pushedAt": "2026-10-23T09:14:02.381552Z"
}
```

`--fail-with-body` fails the CI step on any other answer and prints why:

| Status | Cause |
|---|---|
| `400` | The body isn't Infracost JSON or is over 10 MB, or `source` isn't `infracost`. Argo CD also answers `400` when a header is missing |
| `401` | Argo CD refused the call: the token may not `get` the Application or `invoke` `cost-intel`, or `Argocd-Project-Name` isn't the Application's project. The `argocd-server` log has the reason |
| `403` | `Argocd-Application-Name` names a different application from the URL |
| `500` | The estimate couldn't be saved. The service log has the reason |

The **Actual costs** tab then shows a full-stack monthly cost card: Kubernetes compute, cloud services and the total. The card warns when the estimate is more than 7 days old, and it's hidden when nothing has been pushed.

## Optional: Prometheus metrics

By default the chart serves metrics on port 9102: each Application's cost per hour (from the exporter) and the health of each API replica. If your Prometheus scrapes by `prometheus.io/*` pod annotations (the `prometheus-community/prometheus` chart does), you don't need to do anything else, as long as it scrapes with `honor_labels: true`. Two settings are worth checking:

- **Who may scrape.** The NetworkPolicy admits only `metrics.allowedFrom`, by default the `monitoring` namespace. If your Prometheus runs somewhere else, its scrapes time out until you set it; see [Prometheus metrics](metrics.md#security).
- **The Prometheus Operator.** Use `metrics.serviceMonitor.enabled`, `metrics.prometheusRule.enabled`, their `labels`, and `metrics.podAnnotations=false`. See [Prometheus metrics](metrics.md#prometheus-operator).

Anyone who can query that Prometheus sees every Application's cost, whatever Argo CD's RBAC says. To export only the service's own health, set `metrics.exporter.enabled=false`. To export nothing, set `metrics.enabled=false`. [Prometheus metrics](metrics.md) lists the metrics, how to join them with `argocd_app_info`, and example queries and alerts.

## Upgrade

Database migrations are built into the backend and run when it starts, so an upgrade needs no manual database work. The PostgreSQL password is read back from the existing Secret, so you don't pass it again.

```bash
helm upgrade argocd-cost-intelligence \
  oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence \
  --version <new-version> \
  --namespace argocd-cost-intel \
  --set namespace.create=false \
  <the same --set and -f flags as at install, minus postgres.auth.password> \
  --wait --timeout 10m
```

Keep your settings in a values file and pass it with `-f` every time. `--reuse-values` works too, but it pins every chart default to the old release, so values that a newer chart adds aren't picked up. Read the [changelog](../CHANGELOG.md) for anything that needs action on your side.

The hook runs again on every upgrade. That keeps Argo CD's configuration in step with the chart (including the webhook token) and restarts `argocd-server`.

## Uninstall

```bash
helm uninstall argocd-cost-intelligence -n argocd-cost-intel
```

> **Warning:** This deletes the PostgreSQL PVC, and with it all stored cost history. The chart doesn't mark the PVC to be kept. [Back up first](operations.md#backup-restore-and-pruning) if you want the data. If the chart created the namespace, `helm uninstall` removes that too.

The hook's changes to Argo CD stay behind. Remove them in this order. A `remove` fails if its key is already gone, so skip that command if so.

```bash
# 1. Extension proxy config and feature flag
kubectl patch cm argocd-cm -n argocd \
  --type json -p '[{"op":"remove","path":"/data/extension.config"}]'
kubectl patch cm argocd-cmd-params-cm -n argocd \
  --type json -p '[{"op":"remove","path":"/data/server.enable.proxy.extension"}]'

# 2. Notifications entries. Then delete the cost-intel entry from `subscriptions`
#    with `kubectl edit cm argocd-notifications-cm -n argocd`, and the key itself
#    if that was its only entry.
kubectl patch cm argocd-notifications-cm -n argocd \
  --type json -p '[
    {"op":"remove","path":"/data/service.webhook.cost-intel"},
    {"op":"remove","path":"/data/trigger.cost-intel-on-sync-succeeded"},
    {"op":"remove","path":"/data/template.cost-intel-sync-succeeded"}
  ]'

# 3. The webhook token
kubectl patch secret argocd-notifications-secret -n argocd \
  --type json -p '[{"op":"remove","path":"/data/cost-intel-webhook-token"}]'

# 4. The admin rule
kubectl patch cm argocd-rbac-cm -n argocd \
  --type json -p '[{"op":"remove","path":"/data/policy.cost-intel.csv"}]'

# 5. The UI init container and volume on argocd-server
#    (one patch: the volume mount must go together with the volume).
#    The server container is argocd-server with install.yaml and server with
#    Argo CD's Helm chart; both list it first.
SERVER=$(kubectl get deploy -n argocd -l app.kubernetes.io/name=argocd-server -o name)
CONTAINER=$(kubectl get "$SERVER" -n argocd -o jsonpath='{.spec.template.spec.containers[0].name}')
kubectl patch "$SERVER" -n argocd --type strategic \
  -p '{"spec":{"template":{"spec":{"initContainers":[{"name":"cost-intel-extension","$patch":"delete"}],"containers":[{"name":"'"$CONTAINER"'","volumeMounts":[{"mountPath":"/tmp/extensions","$patch":"delete"}]}],"volumes":[{"name":"cost-intel-ext-js","$patch":"delete"}]}}}}'

# 6. Restart so the changes apply
kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-server
kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-notifications-controller
```
