# Operations

Running the service day to day: fixing problems, how the NetworkPolicies work, and looking after the database. For installing, see [installation.md](installation.md).

## Troubleshooting

### The Cost Intelligence tab doesn't appear

If you turned the hook off and configured Argo CD from Git, go through [If the tab doesn't appear](gitops-install.md#if-the-tab-doesnt-appear) in the GitOps guide instead.

Run the checks in [Check that it works](installation.md#check-that-it-works). These are the usual causes:

- The `cost-intel-extension` init container is missing from `argocd-server`. Either the hook failed, or something that manages the Deployment reverted it. A failed hook Job is kept, so you can read its log: `kubectl logs -n argocd-cost-intel job/argocd-cost-intelligence-post-install`.
- The viewer lacks `extensions, invoke, cost-intel` (see [Grant access in Argo CD](installation.md#grant-access-in-argo-cd)).
- The init container couldn't download the bundle. It retries 5 times and then gives up quietly. Check `kubectl logs -n argocd <argocd-server-pod> -c cost-intel-extension`, the UI pod, and any NetworkPolicy or egress rules between the two namespaces.

### The post-install hook fails

```bash
kubectl logs -n argocd-cost-intel job/argocd-cost-intelligence-post-install --tail=50
```

Common causes:

- With `webhook.existingSecret`, the Secret is missing or has no `token` key, so the Job's pod can't start.
- `argoCd.namespace` doesn't match where Argo CD runs.
- The ServiceAccount or ClusterRole that the hook creates was rejected. Check the Job's events.
- A rollout in the Argo CD namespace didn't finish in time.

### "Cluster-wide costs are turned off"

No Application gates them. The service logs why when it starts, in a line beginning `Cluster-wide cost data`. Either `costIntelligence.clusterScopeApp` is `off`, or the service found neither the Argo CD Application that installed it nor the chart's `cost-intelligence-access` Application. The second happens when:

- Cost Intelligence was installed by something other than Argo CD or Helm (Flux, Terraform, `kubectl apply` of the raw manifests), or Helm couldn't see the Argo CD Application CRD at install time.
- An Argo CD on another cluster manages it, or Argo CD tracks resources by label only (`application.resourceTrackingMethod: label`), so the Deployment carries no tracking annotation.
- The service may not read its own Deployment (the chart's `-self` Role) or Argo CD Applications.
- Someone deleted the `cost-intelligence-access` Application. `helm upgrade` creates it again.

Set the gate yourself in any of these cases: see [Choose your settings](installation.md#choose-your-settings), then run `helm upgrade --reuse-values --set costIntelligence.clusterScopeApp=<namespace>:<name>`. The service looks again every minute, so a new access Application is picked up without a restart. If the page tells you to ask for access to an application, the gate is set but the viewer's role can't `get` that Application.

### Costs show $0 or "no data"

- **No sync yet.** Costs are captured when an application syncs successfully, so sync one.
- **OpenCost.** Try it from the backend pod:
  ```bash
  kubectl exec -n argocd-cost-intel deploy/argocd-cost-intelligence -- \
    curl -s 'http://opencost.opencost:9003/allocation?window=24h' | head -c 200
  ```
  Use your `openCost.baseURL` if it differs. A new OpenCost needs time to collect metrics and reports $0 until it has a full window.
- **The tab shows "401: Unauthorized extension request".** Argo CD refused the call before it reached the backend. The `argocd-server` log says `permission denied: extensions, invoke, cost-intel`. The hook allows `role:admin` through `policy.cost-intel.csv` in `argocd-rbac-cm`, unless `argoCd.rbac.grantAdmin` is off. Every other role needs the rule from [Grant access in Argo CD](installation.md#grant-access-in-argo-cd).
- **The webhook is refused.** The backend logs `Refused POST /on-sync` and answers 401. Argo CD Notifications sent no token or the wrong one. It sends the `cost-intel-webhook-token` key of `argocd-notifications-secret`, which has to equal the backend's token. `helm upgrade` copies it again. If something else manages that Secret (Argo CD's own Helm chart, say) and replaces it, add the key there.
- **The webhook is accepted but nothing is stored.** `/on-sync` answers 202 as soon as it queues the work, and the OpenCost query and database write happen afterwards. Follow the backend log while you sync: `kubectl logs -f -n argocd-cost-intel deploy/argocd-cost-intelligence`.
- **The webhook never arrives.** Run `kubectl logs -n argocd -l app.kubernetes.io/name=argocd-notifications-controller --tail=-1 | grep cost-intel`, and check that `subscriptions` in the notifications ConfigMap still has the entry that sends `cost-intel-on-sync-succeeded` to `cost-intel`. Restart the controller after you change the ConfigMap.

### Rightsizing doesn't show

Check these in order:

- The VPA is installed: `kubectl get crd verticalpodautoscalers.autoscaling.k8s.io`.
- The workload has a VPA in `Off` mode with a recommendation. Run `kubectl get vpa -n <namespace>`, then `kubectl describe vpa -n <namespace> <name>` to see the update mode and, under `Status`, the recommendation.
- The backend may read VPAs. `kubectl auth can-i list verticalpodautoscalers.autoscaling.k8s.io -n <namespace> --as=system:serviceaccount:argocd-cost-intel:argocd-cost-intelligence` should say `yes`. If it doesn't, check that `costIntelligence.vpa.enabled` is `true`.
- The application has synced since the install or upgrade.
- The change is worth at least $1 a month. Smaller ones only show on the Resources tab.

### Pods don't start or stay unready

```bash
kubectl describe pod <pod> -n argocd-cost-intel
kubectl get events -n argocd-cost-intel --sort-by=.lastTimestamp
```

- `ImagePullBackOff`: the image tag doesn't exist, or the registry can't be reached.
- The backend restarts at first start: PostgreSQL wasn't ready yet. It recovers by itself.
- Pods only become unready after the NetworkPolicy is applied: your CNI blocks kubelet probes. See [NetworkPolicy](#networkpolicy).

### Database connection errors

```bash
kubectl logs -n argocd-cost-intel deploy/argocd-cost-intelligence
```

- **`password authentication failed`**: PostgreSQL sets its user's password only when it first initialises its volume, so changing the Secret afterwards doesn't change the database. Either set the Secret back to the original value, or run `ALTER USER` inside `postgres-0` to match it. Starting over with an empty database means uninstalling, which deletes the PVC (see [Uninstall](installation.md#uninstall)).
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

These commands use the bundled PostgreSQL (pod `postgres-0`) with the default user and database. Adjust them if you changed `postgres.auth.*`.

### Backup, restore and pruning

```bash
# Backup
kubectl exec postgres-0 -n argocd-cost-intel -- \
  pg_dump -U cost_user cost_intelligence > backup.sql

# Restore (into an empty database, with the backend scaled down)
kubectl exec -i postgres-0 -n argocd-cost-intel -- \
  psql -U cost_user cost_intelligence < backup.sql
```

Nothing in the chart backs up PostgreSQL, so set up your own schedule.

`cost_actuals` gets a row per sync and is never pruned automatically. To keep, say, the last 90 days:

```bash
kubectl exec postgres-0 -n argocd-cost-intel -- \
  psql -U cost_user -d cost_intelligence \
  -c "DELETE FROM cost_actuals WHERE measured_at < NOW() - INTERVAL '90 days';"
```

The service prunes `cost_daily` itself, down to `costIntelligence.dailyHistoryDays`. Pruning `cost_actuals` also drops the latest record of any application that hasn't synced in that period.

### Connectivity

```bash
kubectl exec postgres-0 -n argocd-cost-intel -- \
  psql -U cost_user -d cost_intelligence -c "SELECT 1;"
```

The backend image has no `psql`. To see connection errors, read its log (`kubectl logs deploy/argocd-cost-intelligence -n argocd-cost-intel`).
