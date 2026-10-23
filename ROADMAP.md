# Roadmap

What we plan to build next, grouped by theme. There are no dates. The order changes as we learn what the people who use the project need, and nothing here is a promise until it is in a release. The [changelog](CHANGELOG.md) lists what has shipped.

## Next up

1. Budgets per application: set a monthly budget for an application and see on its cost tab how much of it is spent, with a warning as it gets close and a banner once it is over.
2. Storage cost per volume: each PersistentVolumeClaim with its storage class, how much of it is used and what it costs, so oversized or expensive volumes stand out.
3. Cost impact of a sync: how much a sync changed the estimated cost of an application's cloud services, from the Infracost estimate for the commit it deployed.

## Insights without a model

Answers worked out from the cost the service already stores, with no model and nothing new to install, so you can check how each one was reached:

- a period compared with the one before it
- a lasting change in cost, not only a spike
- cost at night and over weekends, for workloads that could scale down
- anomalies that weigh efficiency as well as cost
- how much an application's cost varies, its cost per replica, and when one controller accounts for most of it
- the kind of waste an application has, such as requests far above use, or a workload that sits idle
- how long until an application passes its budget
- patterns worth a security look, such as CPU and network cost rising together the way crypto mining does, or a workload never seen before
- what-if estimates for more or fewer replicas and for spot nodes
- an application compared with similar ones

## More from OpenCost

Data OpenCost already offers that the plugin doesn't use yet:

- applications that deploy to more than one namespace, and CronJob runs grouped under their CronJob
- GPU efficiency and idle GPUs
- AI inference cost per model
- node costs grouped by pool, node prices and spot nodes
- pods that ran recently and are gone
- cloud billing and external service costs, where OpenCost is set up to read them

## Cloud service estimates

More from the [Infracost](https://www.infracost.io/) estimates your CI pushes:

- estimates compared with measured cost over time
- rightsizing savings priced with the instance types in your infrastructure code
- the estimated and measured cost change of each commit an application syncs
- cloud resources missing the tags chargeback depends on

## ApplicationSets

- which ApplicationSet an application belongs to
- cost per ApplicationSet, with its environments side by side
- anomalies across an ApplicationSet
- cost per rollout step and by generator type

## Other cost sources

- confirm that the service works with Kubecost, which serves the same allocation API as OpenCost
- per-application cost from Datadog Cloud Cost Management and from Vantage, for teams that already use them

## Summaries written by a model

Opt-in and off by default. The plugin stays fully useful without a model, and a model never replaces an answer the rules above can give. A self-hosted model is the expected setup, and only summaries are sent to it, never raw metrics.

- a short plain-English summary after each sync
- why an application's cost changed
- questions about cost asked in plain English
- a weekly digest per namespace or team
- a cost comment on pull requests
- cost spikes linked to cluster events

## Platform

- client-certificate TLS to PostgreSQL
- measuring again, on a schedule, applications that an OpenCost outage left without a cost

## Ideas and feedback

Share an idea, or upvote one you need too, in the [Ideas](https://github.com/OctopusDeploy/argocd-cost-intelligence/discussions/categories/ideas) category of GitHub Discussions. If you'd like to build something on this list, open an issue first so we can agree on the approach, as [CONTRIBUTING.md](CONTRIBUTING.md) describes.
