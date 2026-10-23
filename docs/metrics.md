# Prometheus metrics

ArgoCD Cost Intelligence exports two kinds of metrics:

- What each Argo CD Application costs, per hour. A single exporter process serves these ([why](architecture.md#the-exporter-runs-as-a-single-replica)). Use them for Grafana dashboards, chargeback queries and cost alerts, next to Argo CD's own `argocd_app_info`.
- How the service itself is doing. Each API replica serves its own: requests, sync webhooks and measurements, calls to OpenCost and PostgreSQL, and the daily history recorder. Use them to tell whether costs are being measured at all.

Both are served on a port of their own (`9102` by default), never on the API port. The [Helm chart](#scraping) adds scrape annotations and, if you want them, ServiceMonitors and a PrometheusRule with [recording rules and starter alerts](#recording-rules-and-alerts).

## Application cost

Every 5 minutes (`metrics.exporter.interval`), the exporter:

1. lists the Applications Argo CD has now, in every namespace, from the Kubernetes API;
2. asks OpenCost for the cost of each namespace their workloads run in, **over the last hour**, one query per namespace;
3. attributes the pods to Applications by the workloads that own them (the same rule as the Cost Intelligence tab: a CronJob's Jobs and a Rollout's ReplicaSets count under their parent).

Scrapes are served from the last refresh, so scraping is cheap and puts no load on OpenCost.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `argocd_cost_intelligence_application_hourly_cost` | gauge | `namespace`, `name`, `resource` | Cost per hour of the pods the Application's workloads own, averaged over the last hour. `resource` is `cpu`, `memory`, `gpu`, `storage`, `network` or `load_balancer`; together they are the Application's cost. |
| `argocd_cost_intelligence_application_idle_hourly_cost` | gauge | `namespace`, `name`, `resource` | The part of the hourly cost paid for requested CPU or memory the pods did not use. `resource` is `cpu` or `memory`; a subset of `application_hourly_cost` for the same resource, never added to it. |
| `argocd_cost_intelligence_application_estimated_hourly_cost` | gauge | `namespace`, `name`, `source` | Cost per hour of the Application's cloud services, as estimated by an IaC tool (`source="infracost"`) and [pushed from CI](installation.md#optional-cloud-service-costs). Infracost's monthly figure divided by 730. |
| `argocd_cost_intelligence_application_workload_info` | gauge, always 1 | `namespace`, `name`, `workload_namespace`, `workload_kind`, `workload_name` | One series per pod-owning workload the Application manages, for [joining per-workload metrics](#an-applications-workloads-with-kube-state-metrics) to Applications. |

The `namespace` and `name` labels identify the Application exactly as Argo CD's `argocd_app_info` does: `namespace` is where the Application resource lives (usually `argocd`), not where it deploys. Join on both: with apps-in-any-namespace, `team-a/web` and `team-b/web` are different Applications. Where an Application deploys is in `argocd_app_info`'s `dest_namespace`, or in `workload_namespace`.

Costs are in OpenCost's currency, usually USD. Multiply by 730 for a month (the month Infracost and the cloud providers use), by 24 for a day.

Each refresh replaces the series. An Application that has no workloads (only ConfigMaps, say) has no series. An Application that could not be measured, because OpenCost failed for one of its namespaces, has no cost series until it can be, rather than an old value; its `workload_info` stays. `exporter_refresh_errors_total` counts the failures. A deleted Application's series disappear at the next refresh.

An Application that deploys to another cluster has no cost or `workload_info` series, because OpenCost measures only the cluster it runs in ([why](limits-and-requirements.md#only-this-cluster-is-measured)). Its cloud estimates come from CI, not OpenCost, so `application_estimated_hourly_cost` still has them. `exporter_remote_applications` counts these Applications.

These metrics won't match the Cost Intelligence tab. The tab projects a 30-day month (720 hours) from the last 24 hours at each sync, while these are always the last hour, so they follow scaling, restarts and spot price changes as they happen.

The metrics only cover the Application's own pods. Namespace and cluster totals, including pods no Application manages, idle node capacity and shared costs, are for OpenCost to report. Adding the metrics up gives the cost Argo CD Applications are responsible for, not your bill.

### The exporter's own health

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `argocd_cost_intelligence_exporter_last_refresh_timestamp_seconds` | gauge | | When the exporter last refreshed with the current list of Applications. Not updated when listing Applications fails. |
| `argocd_cost_intelligence_exporter_last_refresh_duration_seconds` | gauge | | How long the last refresh took. |
| `argocd_cost_intelligence_exporter_remote_applications` | gauge | | Applications in the last refresh that deploy to another cluster, which OpenCost here does not measure. They are expected, not errors. |
| `argocd_cost_intelligence_exporter_refresh_errors_total` | counter | `stage` | Failed refresh steps: `applications` (listing Applications; every cost series is dropped), `opencost` (one per namespace that failed), `estimates` (reading cloud estimates from PostgreSQL; on a fresh install, before the API has created its tables, there are no estimates and no error). |

The exporter also reports `opencost_request_duration_seconds`, `database_operation_duration_seconds`, and Go and process metrics, as below.

### Cardinality

About 8 series per Application with workloads, plus one per workload and one per cloud estimate: some 12,000 series for 1,000 Applications. Pods are never a label.

## Service health

Each API replica reports its own; Prometheus tells replicas apart by target.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `argocd_cost_intelligence_http_request_duration_seconds` | histogram | `route`, `method`, `code` | API requests. `route` is the route pattern, such as `/api/cost-analysis/{appName}/actual`, never the actual path; `unmatched` for paths no route serves. |
| `argocd_cost_intelligence_sync_webhooks_total` | counter | `result` | Sync notifications from Argo CD: `accepted`, `unauthorized` (wrong or missing token), `invalid` (unreadable, or no application namespace), `overloaded` (refused because too many measurements are running), `remote` (the application deploys to another cluster, so nothing is measured) or `ignored` (the chart's access Application, which deploys nothing, so nothing is measured). |
| `argocd_cost_intelligence_sync_measurements_total` | counter | `result` | Cost measurements after accepted notifications: `success`, `error`, or `no_data` (OpenCost had no data for the namespace yet, or less than 5 minutes of it, as for one the sync just created, so nothing was recorded). A `no_data` measurement is retried after 1, 2, 4 and 8 minutes, and each retry counts here too. |
| `argocd_cost_intelligence_sync_measurement_duration_seconds` | histogram | | Duration of a measurement, including the 5 s wait for metrics to settle. Retries are not timed. |
| `argocd_cost_intelligence_opencost_request_duration_seconds` | histogram | `endpoint`, `code` | Requests to OpenCost, by API path (`/allocation`, `/allocation/summary`, `/assets`, ...) and HTTP status; `code="error"` when no response arrived. |
| `argocd_cost_intelligence_database_operation_duration_seconds` | histogram | `operation`, `result` | Storage operations against PostgreSQL, such as `SaveActual`. A record that does not exist yet is a `success`. |
| `argocd_cost_intelligence_history_runs_total` | counter | `result` | Runs of the daily cost history recorder: `success`, or `error` when any Application failed. |
| `argocd_cost_intelligence_history_last_run_timestamp_seconds` | gauge | | When the recorder last finished a run; 0 until the first one, and always with `DAILY_HISTORY_DAYS=0`. |
| `go_*`, `process_*` | | | Go runtime and process metrics. |

Every `result` starts at zero, so rates and alerts work before the first event.

## Example queries

`namespace_name:argocd_cost_intelligence_application_hourly_cost:sum`, each Application's cost per hour, is the [recording rule](#recording-rules-and-alerts); without it, use `sum by (namespace, name) (argocd_cost_intelligence_application_hourly_cost)`.

Monthly cost by Argo CD project (or `dest_namespace`, `dest_server`, ...):

```promql
sum by (project) (
  namespace_name:argocd_cost_intelligence_application_hourly_cost:sum
  * on (namespace, name) group_left (project) argocd_app_info
) * 730
```

This needs `argocd_app_info` scraped with its own `namespace` label. If yours shows `exported_namespace` instead, scrape Argo CD's metrics with `honorLabels: true` (in the `argo/argo-cd` chart, `controller.metrics.serviceMonitor.honorLabels`), or rename it with `label_replace`.

What share of each Application's cost pays for requests it does not use:

```promql
sum by (namespace, name) (argocd_cost_intelligence_application_idle_hourly_cost)
  / namespace_name:argocd_cost_intelligence_application_hourly_cost:sum
```

Full-stack cost per hour, Kubernetes plus cloud services:

```promql
namespace_name:argocd_cost_intelligence_application_hourly_cost:sum
  + on (namespace, name) group_left
    sum by (namespace, name) (argocd_cost_intelligence_application_estimated_hourly_cost)
```

### An Application's workloads with kube-state-metrics

`application_workload_info` maps Applications to workloads. Keep only the labels you join on with `group by`, rename them to the other metric's with `label_replace`, then join. Without the `group by`, the join fails with duplicate series whenever an old exporter pod's series are still visible beside the new pod's (see [Labels the scrape adds](#labels-the-scrape-adds)). For example, the replicas each Application's Deployments want:

```promql
sum by (app_namespace, app_name) (
  kube_deployment_spec_replicas
  * on (namespace, deployment) group_left (app_namespace, app_name)
    label_replace(label_replace(label_replace(label_replace(
      group by (namespace, name, workload_namespace, workload_name) (
        argocd_cost_intelligence_application_workload_info{workload_kind="Deployment"}
      ),
      "app_namespace", "$1", "namespace", "(.*)"),
      "app_name", "$1", "name", "(.*)"),
      "namespace", "$1", "workload_namespace", "(.*)"),
      "deployment", "$1", "workload_name", "(.*)")
)
```

## Recording rules and alerts

[`helm/files/prometheus-rules.yaml`](../helm/files/prometheus-rules.yaml) holds one recording rule and some starter alerts. With the Prometheus Operator, `metrics.prometheusRule.enabled=true` installs them as a PrometheusRule. Otherwise add the file to your Prometheus `rule_files`. They're tested with `promtool` ([`test/prometheus/`](../test/prometheus/)).

| Rule | When |
|---|---|
| `namespace_name:argocd_cost_intelligence_application_hourly_cost:sum` | Each Application's cost per hour, every resource together. |
| `ArgoCDApplicationCostIncrease` | An Application costs more than 1.5× its average over the past week, and more than 0.01 an hour, for 2 hours. |
| `ArgoCDCostIntelligenceExporterStale` | The exporter has not refreshed for 15 minutes. |
| `ArgoCDCostIntelligenceExporterAbsent` | No exporter is scraped, for 15 minutes. |
| `ArgoCDCostIntelligenceExporterErrors` | A refresh step keeps failing for 30 minutes. |
| `ArgoCDCostIntelligenceMeasurementsFailing` | More than half of the measurements after a sync fail, for 15 minutes. Measurements OpenCost had no data for (`no_data`) are left out. |
| `ArgoCDCostIntelligenceWebhooksRefused` | Webhooks were refused in the last 30 minutes and none got through (accepted, or for an application on another cluster): the token Argo CD sends no longer matches. |
| `ArgoCDCostIntelligenceOpenCostErrors` | More than half of the requests to OpenCost fail, for 15 minutes. |
| `ArgoCDCostIntelligenceHistoryStale` | No replica has recorded daily cost history for 3 hours. |
| `ArgoCDCostIntelligenceHistoryFailing` | Every history run in the last 3 hours failed. |

The thresholds are starting points. The cost alert compares with the recording rule's own history, so it needs the rule to have run for a week before it is reliable. To tune them, copy the file and set `metrics.prometheusRule.enabled=false`.

There's no metric for "cost change since the last sync" or "anomaly". Prometheus works them out from the cost series it already has, as `ArgoCDApplicationCostIncrease` does.

## Scraping

### Pod annotations (default)

The chart adds `prometheus.io/scrape`, `prometheus.io/port` and `prometheus.io/path` to the API and exporter pods (`metrics.podAnnotations`), which the `kubernetes-pods` job of the `prometheus-community/prometheus` chart discovers. That job sets `honor_labels: true` (in the chart since version 15.8.1); your own job must too, or Prometheus renames the exporter's `namespace` label to `exported_namespace` and the joins with `argocd_app_info` stop matching.

### Prometheus Operator

```bash
helm upgrade argocd-cost-intelligence \
  oci://ghcr.io/octopusdeploy/charts/argocd-cost-intelligence --version <chart-version> \
  -n argocd-cost-intel --reuse-values --set namespace.create=false \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.prometheusRule.enabled=true \
  --set metrics.serviceMonitor.labels.release=kube-prometheus-stack \
  --set metrics.prometheusRule.labels.release=kube-prometheus-stack \
  --set metrics.podAnnotations=false
```

The labels are the ones your Prometheus selects ServiceMonitors and PrometheusRules by. This creates one ServiceMonitor for the API replicas and one for the exporter, which sets `honorLabels: true`. Turn the annotations off if your Prometheus also scrapes by annotation, or every series is scraped twice.

### Labels the scrape adds

Both ways of scraping add labels the exporter doesn't set: `job`, `instance` and `pod`, plus the pod's own labels with the `kubernetes-pods` job (and `node` in recent versions of its chart), or `service`, `endpoint` and `container` with a ServiceMonitor. The recording rule and the example queries aggregate them away, and the cost alert reads only the recording rule. Do the same in your own queries and dashboards rather than keeping `pod` or `instance`: each new exporter pod, after an upgrade or when Kubernetes moves it, starts new series.

When the exporter pod goes away, Prometheus marks its series stale, so they aren't added to the new pod's. If Prometheus misses that, for example while it is restarting itself, the old series stay visible for the query lookback (five minutes by default) and a sum counts each Application twice in that window. `ArgoCDApplicationCostIncrease` waits two hours before it fires, so it isn't affected.

## Security

Anyone who can query your Prometheus sees every Application's cost. Argo CD's RBAC doesn't apply there. If that matters, restrict who may query Prometheus or Grafana, or turn the exporter off (`metrics.exporter.enabled=false`).

The metrics port is the only port Prometheus may reach. The API trusts the `Argocd-Application-Name` header Argo CD's proxy sets, so anything that can reach the API port can read any application's costs ([why](operations.md#networkpolicy)). The chart's NetworkPolicy therefore admits Prometheus to the metrics port only, from `metrics.allowedFrom`, by default any pod in the `monitoring` namespace. Set it to your Prometheus's namespace or pods:

```yaml
metrics:
  allowedFrom:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: prometheus-system
      podSelector:
        matchLabels:
          app.kubernetes.io/name: prometheus
```

The list replaces the default, so it must name every Prometheus that scrapes these pods. One it doesn't name gets no answer on the metrics port: its scrapes usually time out (`context deadline exceeded`), every Cost Intelligence target shows as down, and, if you load the chart's alert rules, `ArgoCDCostIntelligenceExporterAbsent` fires. Kubernetes sets the `kubernetes.io/metadata.name` label on every namespace, so a namespace selector on it needs no labels of your own. Leave out `podSelector` to admit every pod in the namespace.

With an empty list, nothing may reach the metrics port. `/metrics` holds no secrets and needs no authentication; it carries no pod names, labels or annotations.

The exporter lists Argo CD Applications with the chart's ServiceAccount, which already may (`argoproj.io` `applications`, `get`/`list`), and reads cloud estimates from PostgreSQL. It runs no migrations and serves no API, so until the API has created its tables it exports no cloud estimates and logs that it is waiting for them.

## Configuration

The `metrics.*` Helm values (port, pod annotations, `allowedFrom`, exporter interval and resources, ServiceMonitor, PrometheusRule) are listed in [Configuration](configuration.md#metrics), and the environment variables in [Running without Helm](configuration.md#running-without-helm).

`MODE=exporter` lists Applications through the in-cluster configuration, so it cannot run outside Kubernetes. In exporter mode `/health` and `/ready` are served on the metrics port; `/ready` answers once the first refresh has finished.
