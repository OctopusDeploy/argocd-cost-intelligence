# How costs are measured

What the figures on an application's **Cost Intelligence** tab mean, where they come from and how fresh they are. For how the service works inside, see [Architecture](architecture.md).

## Measurements

After each successful sync, the service asks OpenCost what the application cost over the previous 24 hours and stores the result. The tab shows the latest measurement, and its header says when it was taken.

- **Monthly figures** are projected onto a 30-day month from the hours OpenCost has data for. With less than a day of data, as for a new application, the header says "Projected from N hours of data". Less than 5 minutes of data is too little to project from, so a new application is first measured once OpenCost has 5 minutes of data for its namespace.
- **A sync shows in full a day later.** A measurement covers the 24 hours before it, so the one taken right after a sync mostly reflects the previous version. The Trends tab shows the change from the next day on.
- **Measured at install.** An application without a measurement when the service starts is measured then, without waiting for a sync. Its next sync measures it again.
- **Stale** marks a measurement 7 days old or more.

An application shows no costs until its first measurement.

### The application's own cost

Figures cover only the pods of the workloads Argo CD manages for the application. A CronJob's Jobs and a Rollout's ReplicaSets count under their parent. Other workloads in the same namespace are left out, so applications that share a namespace each see their own cost.

A measurement stored by an earlier version, or sent by a notifications template without the application's resources, covers the whole namespace. On a shared namespace the tab then narrows the monthly cost to the application's pods and says which other figures still cover the whole namespace.

## Actual costs

![Actual costs: monthly cost, change since the last sync, rightsizing advice, cost breakdown and resource efficiency](images/actual-costs.webp)

| Figure | Meaning |
|---|---|
| Monthly cost | The application's Kubernetes cost per month, plus its [cloud services estimate](installation.md#optional-cloud-service-costs) when CI pushes one. |
| Since last sync | The change in Kubernetes cost from the measurement at the previous sync. Empty for a measurement at install, which has no sync to compare with. |
| Idle spend | The part of the monthly CPU and memory cost paid for requested capacity the pods didn't use: per pod, `cost x (request - usage) / request`, and nothing for a pod using at least its request. |
| Pricing | Whether OpenCost prices resources with your cloud provider's prices (Real) or public list prices (Estimated). Unknown when the service can't ask OpenCost. |

**Resource efficiency** compares the average and peak CPU and memory use with the requests. Average use below 50% of the request is over-provisioned, 50% to 80% moderate, 80% to 100% healthy, and above 100% is shown as above request. When peak use is above the request, pods may be throttled.

**Cost breakdown** splits the monthly cost into CPU, memory, storage, network, cloud services, load balancer and GPU. Only resources with a cost are shown. Shared costs and the carbon footprint are listed below it and aren't part of the total.

**What changed** explains the change since the previous sync per workload. It lists up to five changes of at least $1 a month or 2% of the cost. "Not tied to a pod" covers unmounted volumes and other costs OpenCost doesn't assign to a pod.

A **CPU request suggestion** appears when pods use less than half of their CPU request over a full day of data. It suggests a request 25% above the higher of average and peak use, rounded up to 50m, when that saves at least $1 a month. Where a Vertical Pod Autoscaler gives advice (see [Resources](#resources)), its advice is shown instead.

**Cost warning** appears when the monthly Kubernetes cost is more than 30% above the average of the earlier measurements, up to 30 of them, and becomes a **Cost alert** above 100%. It needs at least 3 earlier measurements and flags increases only.

## Resources

![Resources: cost per controller and Vertical Pod Autoscaler rightsizing suggestions](images/resources.webp)

The monthly cost per workload, with its CPU, memory, total and share of the total. The total also counts the costs that have no column of their own, such as storage and network. Each workload expands to its pods.

**Rightsizing** shows when a workload has a Vertical Pod Autoscaler ([setup](installation.md#optional-rightsizing)). Only a VPA in `Off` mode gives advice; one in any other mode sets requests itself and is shown as managed. A recommendation counts once the VPA has about a day of history; before that the workload shows "Not enough data yet". Suggestions are per container and priced with OpenCost's figures. They cover requests only, never limits.

**Orphaned pods**, listed when there are any, are pods that report no CPU or memory use, such as evicted pods, pods stuck in `Pending`, or finished Jobs still holding volumes or load balancers.

## Trends

![Trends: daily cost for this month with a month-end forecast](images/trends.webp)

Daily cost, queried from OpenCost for each day whether or not the application synced. The service stores each finished day, so the history outlives OpenCost's own retention, for 400 days by default ([`costIntelligence.dailyHistoryDays`](configuration.md#backend); `0` turns recording off). Today is still being measured, so it is left out of the averages and the trend.

Ranges run from 30 days to a year, this month or last month, or a custom range of up to 365 days. Ranges up to 92 days show daily cost, up to 200 days weekly, and longer ones monthly.

**Month-end forecast** is what the month has cost so far plus a straight-line trend of the last 30 days to the end of the month. It needs 7 complete days of data and shows when the range includes today.

A day with no cost means OpenCost reported nothing for it: the application's pods weren't running, or OpenCost no longer had that day when it was first recorded.

<!-- The plugin links to this heading. Don't rename or move it without a redirect. -->
## Applications on another cluster

OpenCost measures only the cluster it runs in, so an application that Argo CD deploys to another cluster has no costs. Its tab says "Deploys to another cluster" instead. Measuring it needs OpenCost and Cost Intelligence in that cluster.

The cluster is matched by name: an application is on this cluster only when its destination server is `https://kubernetes.default.svc` or empty and its destination name is `in-cluster` or empty. One that targets this cluster by its API server URL, or by a renamed in-cluster entry, is treated as being on another cluster. See [Limits and requirements](limits-and-requirements.md#only-this-cluster-is-measured).
