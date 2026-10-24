# Operations

Running the service day to day: fixing problems, how the NetworkPolicies work, and looking after the database. For installing, see [Installation](installation.md).

## Troubleshooting

### The Cost Intelligence tab doesn't appear

Run the checks in [Check that it works](installation.md#check-that-it-works). These are the usual causes:

- The `cost-intel-extension` init container is missing from `argocd-server`. Either the hook failed or [never ran](#helm-install-times-out-before-the-hook-runs), or something that manages the Deployment reverted it. A failed hook Job is kept, so you can read its log: `kubectl logs -n argocd-cost-intel job/argocd-cost-intelligence-post-install`.
- The init container couldn't download the bundle, so `/tmp/extensions` is empty. It retries 5 times and then gives up quietly. Its log has the reason: `kubectl logs -n argocd <argocd-server-pod> -c cost-intel-extension`. Usually the UI pod wasn't running yet, or the chart's NetworkPolicy blocks the download because `argoCd.namespace` doesn't match the namespace Argo CD runs in. Check egress rules between the two namespaces too. Restart `argocd-server` once it's fixed.
- The tab shows "401: Unauthorized extension request". Argo CD refused the call before it reached the backend. The `argocd-server` log has `permission denied: extensions, invoke, cost-intel`. The hook allows `role:admin` through `policy.cost-intel.csv` in `argocd-rbac-cm`, unless `argoCd.rbac.grantAdmin` is off. Every other role needs the rule from [Grant access in Argo CD](installation.md#grant-access-in-argo-cd).
- The tab shows an error instead of costs. Check that `server.enable.proxy.extension` is `"true"` in `argocd-cmd-params-cm` and that `argocd-server` restarted after it changed, because it reads that setting only when it starts. Then check that `extension.config` in `argocd-cm` points at the backend Service on port 8080, not the UI Service.
- The tab never fills in. See [Costs show $0 or "no data"](#costs-show-0-or-no-data).

If you turned the hook off and configured Argo CD from Git, also go through [If the tab doesn't appear](gitops-install.md#if-the-tab-doesnt-appear) in the GitOps guide, which covers the changes you made by hand.

### The post-install hook fails

```bash
kubectl logs -n argocd-cost-intel job/argocd-cost-intelligence-post-install --tail=50
```

Common causes:

- With `webhook.existingSecret`, the Secret is missing or has no `token` key, so the Job's pod can't start.
- `argoCd.namespace` doesn't match where Argo CD runs.
- The ServiceAccount or ClusterRole that the hook creates was rejected. Check the Job's events.
- A rollout in the Argo CD namespace didn't finish in time.

### Helm install times out before the hook runs

Helm runs the post-install hook only once everything the chart deploys is ready. If `helm install --wait` times out first, the release is marked `failed` and the hook never runs, so there is no hook Job to read and Argo CD has none of the plugin's configuration: `argocd-cm` has no `extension.config`.

Find the pod that wasn't ready, for example the UI pod failing its probes on a cluster short of CPU, and fix it (see [Pods don't start or stay unready](#pods-dont-start-or-stay-unready)):

```bash
kubectl get pods -n argocd-cost-intel
```

Then run the command from [Upgrade](installation.md#upgrade) with the version you installed. Helm accepts an upgrade of a release whose install failed, and the hook runs again after it. Uninstalling and installing again also works.

With `--atomic` (`--rollback-on-failure` in Helm 4), Helm uninstalls the release instead, so install it again once the pods can start.

### "Cluster-wide costs are turned off"

No Application gates them. The service logs why when it starts, in a line beginning `Cluster-wide cost data`. Either `costIntelligence.clusterScopeApp` is `off`, or the service found neither the Argo CD Application that installed it nor the chart's `cost-intelligence-access` Application. The second happens when:

- Cost Intelligence was installed by something other than Argo CD or Helm (Flux, Terraform, `kubectl apply` of the raw manifests), or Helm couldn't see the Argo CD Application CRD at install time.
- An Argo CD on another cluster manages it, or Argo CD tracks resources by label only (`application.resourceTrackingMethod: label`), so the Deployment carries no tracking annotation.
- The service may not read its own Deployment (the chart's `-self` Role) or Argo CD Applications.
- Someone deleted the `cost-intelligence-access` Application. `helm upgrade` creates it again.

Set the gate yourself in any of these cases: see [Choose your settings](installation.md#choose-your-settings), then run `helm upgrade --reuse-values --set costIntelligence.clusterScopeApp=<namespace>:<name>`. The service looks again every minute, so a new access Application is picked up without a restart. If the page tells you to ask for access to an application, the gate is set but the viewer's role can't `get` that Application.

### Costs show $0 or "no data"

- **Not measured yet.** When the service starts, it measures every application that has no cost, and tries again for about 15 minutes while OpenCost has no data for it or doesn't answer. After that, an application is measured at its next successful sync, so sync it. With `costIntelligence.backfillConcurrency` at `0`, only syncs measure.
- **OpenCost.** Try it from the backend pod:
  ```bash
  kubectl exec -n argocd-cost-intel deploy/argocd-cost-intelligence -- \
    curl -s 'http://opencost.opencost:9003/allocation?window=24h' | head -c 200
  ```
  Use your `openCost.baseURL` if it differs. A new OpenCost needs time to collect metrics and reports $0 until it has a full window.
- **The webhook is refused.** The backend logs `Refused POST /on-sync` and answers 401. Argo CD Notifications sent no token or the wrong one. It sends the `cost-intel-webhook-token` key of `argocd-notifications-secret`, which has to equal the backend's token. `helm upgrade` copies it again. If something else manages that Secret (Argo CD's own Helm chart, say) and replaces it, add the key there.
- **The webhook is accepted but nothing is stored.** `/on-sync` answers 202 as soon as it queues the work, and the OpenCost query and database write happen afterwards. Follow the backend log while you sync: `kubectl logs -f -n argocd-cost-intel deploy/argocd-cost-intelligence`.
- **The webhook never arrives.** Run `kubectl logs -n argocd -l app.kubernetes.io/name=argocd-notifications-controller --tail=-1 | grep cost-intel`, and check that `subscriptions` in the notifications ConfigMap still has the entry that sends `cost-intel-on-sync-succeeded` to `cost-intel`. Restart the controller after you change the ConfigMap: `kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-notifications-controller`.

### Rightsizing doesn't show

Check these in order:

- The VPA is installed: `kubectl get crd verticalpodautoscalers.autoscaling.k8s.io`.
- The workload has a VPA in `Off` mode with a recommendation. Run `kubectl get vpa -n <namespace>`, then `kubectl describe vpa -n <namespace> <name>` to see the update mode and, under `Status`, the recommendation.
- The backend may read VPAs. `kubectl auth can-i list verticalpodautoscalers.autoscaling.k8s.io -n <namespace> --as=system:serviceaccount:argocd-cost-intel:argocd-cost-intelligence` should say `yes`. If it doesn't, check that `costIntelligence.vpa.enabled` is `true`.
- The application has synced since the install or upgrade.
- The change is worth at least $1 a month. Smaller ones only show on the Resources tab.

### Pods don't start or stay unready

- The backend restarts at first start: PostgreSQL wasn't ready yet. It recovers by itself.
- Pods only become unready after the NetworkPolicy is applied: your CNI blocks kubelet probes. See [NetworkPolicy](#networkpolicy).

### Database connection errors

The backend image has no `psql`, so its log is where connection errors show:

```bash
kubectl logs -n argocd-cost-intel deploy/argocd-cost-intelligence
```

- **`password authentication failed`**: the Secret no longer matches the database user's password, usually because the Secret was changed after the first install. See [PostgreSQL credentials](configuration.md#postgresql-credentials).
- **`SSL is not enabled on the server`**: set `postgres.sslMode=disable` for the bundled database, or the mode your external database needs.

## NetworkPolicy

The backend decides who may see what from the `Argocd-Application-Name` header, which Argo CD's extension proxy checks against the viewer's RBAC. Any other client that can reach the pod could set that header itself and read any application's or namespace's costs. So every workload has its own policy, selected by its `component` label (or `app: postgres`), and each one admits only what it needs:

| Pod | Allowed source | Why |
|---|---|---|
| Backend (`costIntelligence.port`) | `argocd-server` in `argoCd.namespace` | The extension proxy: every UI request |
| Backend (`costIntelligence.port`) | `argocd-notifications-controller` in `argoCd.namespace` | `POST /api/cost-analysis/on-sync` |
| UI (`ui.port`) | `argocd-server` in `argoCd.namespace` | The init container downloads the bundle |
| Backend and exporter (`metrics.port`) | `metrics.allowedFrom` (default: the `monitoring` namespace) | Prometheus scrapes `/metrics`, and never reaches the API port |
| PostgreSQL (5432) | The backend and exporter pods | Only when `postgres.enabled`. Its probes run `pg_isready` inside the pod. |

Sources are matched by the `app.kubernetes.io/name` label, which both the Argo CD manifests and the `argo/argo-cd` chart set. If your installation labels them differently, the UI and webhooks stop working once the policy applies. The policies only restrict incoming traffic.

Kubelet probes don't need a rule. The NetworkPolicy spec always allows traffic between a pod and its own node, and the kubelet's probes come from there. If your CNI doesn't implement that exception, pods stay unready once the policy applies, even though the probes would pass without it. In that case list the node address ranges:

```bash
--set "networkPolicy.probeCIDRs={10.0.0.0/16}"
```

This admits those ranges on the API port (and the exporter's metrics port), not just the probe paths, and that includes every `hostNetwork` pod on those nodes. Use the narrowest ranges that work, and never a pod CIDR.

An Ingress sends traffic around the extension proxy, so it can't be authorized. The policy blocks the ingress controller and the chart adds no exception. Users reach cost data through the Argo CD UI.

Anything else that calls the service directly, such as a CI job or a debugging pod, is blocked too. `kubectl port-forward` still works, because the kubelet delivers it from inside the pod's network namespace. CI pushes Infracost estimates through the Argo CD proxy (see [Optional: cloud service costs](installation.md#optional-cloud-service-costs)).

To check, call the service from a pod that should be refused:

```bash
kubectl run np-check --rm -it --restart=Never --image=busybox -n default -- \
  wget -qO- -T 5 http://argocd-cost-intelligence.argocd-cost-intel:8080/health
# expected: "download timed out"
```

## Database

The bundled PostgreSQL runs as pod `postgres-0`, with the user `cost_user` and the database `cost_intelligence`, unless you changed `postgres.auth.*`.

### Backup, restore and pruning

Nothing in the chart backs up PostgreSQL, and `helm uninstall` deletes its PVC, so back it up with your platform's tooling or `pg_dump`. Restore into an empty database, with the backend scaled down, because it creates the schema when it starts.

`cost_actuals` gets a row per sync and is never pruned automatically. To keep, say, the last 90 days:

```bash
kubectl exec postgres-0 -n argocd-cost-intel -- \
  psql -U cost_user -d cost_intelligence \
  -c "DELETE FROM cost_actuals WHERE measured_at < NOW() - INTERVAL '90 days';"
```

The service prunes `cost_daily` itself, down to `costIntelligence.dailyHistoryDays`. Pruning `cost_actuals` also drops the latest record of any application that hasn't synced in that period.
