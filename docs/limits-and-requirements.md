# Limits and requirements

What ArgoCD Cost Intelligence runs on and what it doesn't do, to check before you install it. To install it, see [Installation](installation.md); for how it works inside, see [Architecture](architecture.md).

## Compatibility

| Component | Supported | Last tested with |
|---|---|---|
| Argo CD | 3.4, 3.5 and 3.6 | 3.6.0-rc4 |
| OpenCost | 1.121.1+ (the label picker on Chargeback needs 1.120.3+) | 1.121.3 |
| Kubernetes | Any version Argo CD supports (Argo CD tests 3.4 on 1.32 to 1.35, 3.5 on 1.33 to 1.36, and 3.6 on 1.34 to 1.37) | 1.31 (k3s) |
| Helm | 3.8+ | 4.2.0 |
| PostgreSQL | The bundled `postgres:15-alpine`, or an external instance | 15 |
| Vertical Pod Autoscaler (optional) | A recommender serving `autoscaling.k8s.io/v1` | VPA 1.7.1, Goldilocks 4.16.2 |

"Supported" means the versions the plugin is built for. "Last tested with" is the version we last ran it against. The UI runs on the React that Argo CD provides (16.14 on Argo CD 3.4, 19 on 3.5 and 3.6), so it only uses APIs that React 16.14 and 19 both have. Other versions may work too. If you try one, please tell us how it went.

What the cluster needs besides these (permissions, a StorageClass, metrics-server, network access) is in [Prerequisites](installation.md#prerequisites).

## Limitations

### Only this cluster is measured

OpenCost measures only the cluster it runs in, so only the applications Argo CD deploys to that cluster are measured. An application deployed to another cluster has no costs, and its tab says it deploys to another cluster. Measuring it needs OpenCost and Cost Intelligence in that cluster.

The cluster is matched by name: an application that targets this cluster by its API server URL, or by a renamed in-cluster entry, is treated as being on another cluster.

Cost already stored for such an application, from before it moved or from an earlier version, stays unless you turn on [`costIntelligence.pruneRemoteCost`](configuration.md#backend), which deletes it and can't be undone.

### Costs cover the last 24 hours

Each measurement covers the 24 hours before it, so a sync shows in full a day later. An application younger than a day is projected from the hours it has run, which is less reliable the shorter that is, and it isn't measured until OpenCost has 5 minutes of data for it. See [Measurements](how-costs-are-measured.md#measurements).

### An application is measured when it syncs

Cost is measured after each successful sync, not on a schedule. When OpenCost has no data yet, the service tries again for 15 minutes. A sync whose webhook is lost, or that OpenCost fails to answer, isn't measured: the application keeps its previous measurement until it syncs again. One that has never been measured is measured when the service starts, unless [`costIntelligence.backfillConcurrency`](configuration.md#backend) is `0`.

Daily history and the Prometheus metrics, when they are on, read OpenCost on their own schedule, so cost over time keeps being recorded.

### Past costs can't be measured again

Sending the webhook again measures the 24 hours up to now, not the ones after the original sync. Daily history starts with the days OpenCost still has when the service first records an application, so history from before the install goes back only as far as OpenCost's Prometheus retention.

### Some measurements cover the whole namespace

Figures cover the application's own pods only when the notifications template sends the application's resources, as the chart's template does. A measurement stored by an earlier version, or sent by a template without them, covers the whole namespace. See [The application's own cost](how-costs-are-measured.md#the-applications-own-cost).

### Cloud service costs come from CI

OpenCost can't see cloud services such as RDS or S3. Their cost appears only when your CI pipeline pushes an [Infracost estimate](installation.md#optional-cloud-service-costs), and the service can't tell whether it is current: the tab warns once an estimate is 7 days old.

### Nothing backs up the database

The chart doesn't back up PostgreSQL. Use your platform's backup tooling or `pg_dump`, with the names in [Database](operations.md#database).
