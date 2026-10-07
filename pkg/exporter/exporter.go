// Package exporter exports each Argo CD Application's cost to Prometheus. Cost is global state, the
// same whichever replica reads it, so it is exported by one singleton process (the service started
// with MODE=exporter), as kube-state-metrics and OpenCost export theirs, rather than by every API
// replica, whose identical series would add up to several times the cost.
//
// Every interval the exporter lists the Applications Argo CD has now, measures each namespace their
// workloads run in over the last window, and attributes the pods to Applications by the workloads that
// own them. Scrapes are served from the last refresh, so they cost nothing and put no load on OpenCost.
//
// The exporter publishes only what its last refresh measured. An Application it could not measure is
// absent, not shown with an older value, and refresh_errors_total says which step failed.
package exporter

import (
	"context"
	"log"
	"sort"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/telemetry"
)

// Window is the period each refresh measures, and over which cost per hour is averaged.
const Window = time.Hour

// infracostMonthHours is the month Infracost's monthly figures assume, so an estimate converts to
// the same cost per hour Infracost started from.
const infracostMonthHours = 730

// Refresh steps, the values of refresh_errors_total's stage label.
const (
	StageApplications = "applications"
	StageOpenCost     = "opencost"
	StageEstimates    = "estimates"
)

// Costs are a namespace's pods over a window, from which each Application's share is taken.
type Costs interface {
	HourlyCostOf(resources models.ManagedResources) opencost.HourlyCost
}

// CostSource measures a namespace's pods over the window up to now.
type CostSource interface {
	RecentCosts(ctx context.Context, namespace string, window time.Duration) (Costs, error)
}

// EstimateSource lists the cloud estimates CI pushed for each Application.
type EstimateSource interface {
	ListCloudEstimates(ctx context.Context) ([]models.AppCloudEstimate, error)
}

// openCostSource reads costs from OpenCost.
type openCostSource struct{ client *opencost.Client }

// NewOpenCostSource measures namespaces with OpenCost's allocation API.
func NewOpenCostSource(client *opencost.Client) CostSource { return openCostSource{client: client} }

func (s openCostSource) RecentCosts(ctx context.Context, namespace string, window time.Duration) (Costs, error) {
	return s.client.GetRecentPodAllocations(ctx, namespace, window)
}

var (
	appLabels = []string{"namespace", "name"}

	hourlyCostDesc = prometheus.NewDesc(telemetry.Namespace+"_application_hourly_cost",
		"Cost per hour of the pods the Argo CD Application's workloads own, averaged over the last hour, in OpenCost's currency. "+
			"resource is cpu, memory, gpu, storage, network or load_balancer; together they are the Application's cost.",
		append(appLabels, "resource"), nil)
	idleHourlyCostDesc = prometheus.NewDesc(telemetry.Namespace+"_application_idle_hourly_cost",
		"Part of the Application's hourly cost paid for requested CPU or memory its pods did not use, in OpenCost's currency. "+
			"A subset of application_hourly_cost for the same resource.",
		append(appLabels, "resource"), nil)
	estimatedHourlyCostDesc = prometheus.NewDesc(telemetry.Namespace+"_application_estimated_hourly_cost",
		"Cost per hour of the Application's cloud services as estimated by an IaC cost tool and pushed from CI, "+
			"converted from its monthly figure with 730 hours a month.",
		append(appLabels, "source"), nil)
	workloadInfoDesc = prometheus.NewDesc(telemetry.Namespace+"_application_workload_info",
		"A pod-owning workload the Argo CD Application manages, always 1, for joining per-workload metrics to Applications.",
		append(appLabels, "workload_namespace", "workload_kind", "workload_name"), nil)
)

// sample is one exported value with its label values, in the order of its Desc.
type sample struct {
	desc   *prometheus.Desc
	value  float64
	labels []string
}

// Exporter refreshes Application costs on an interval and serves them as Prometheus metrics.
type Exporter struct {
	apps      ApplicationLister
	costs     CostSource
	estimates EstimateSource
	interval  time.Duration
	logger    *log.Logger
	now       func() time.Time

	samples  atomic.Pointer[[]sample]
	ran      atomic.Bool
	errors   *prometheus.CounterVec
	last     prometheus.Gauge
	duration prometheus.Gauge
}

// New returns an exporter refreshing every interval, registered on reg.
func New(apps ApplicationLister, costs CostSource, estimates EstimateSource, interval time.Duration, logger *log.Logger, reg prometheus.Registerer) *Exporter {
	e := &Exporter{
		apps:      apps,
		costs:     costs,
		estimates: estimates,
		interval:  interval,
		logger:    logger,
		now:       time.Now,
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: telemetry.Namespace,
			Name:      "exporter_refresh_errors_total",
			Help:      "Failed steps of the exporter's refreshes, by stage: applications, opencost (one per namespace) or estimates.",
		}, []string{"stage"}),
		last: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: telemetry.Namespace,
			Name:      "exporter_last_refresh_timestamp_seconds",
			Help:      "When the exporter last refreshed Application costs with the current Application list, as a Unix timestamp.",
		}),
		duration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: telemetry.Namespace,
			Name:      "exporter_last_refresh_duration_seconds",
			Help:      "How long the exporter's last refresh took.",
		}),
	}
	for _, stage := range []string{StageApplications, StageOpenCost, StageEstimates} {
		e.errors.WithLabelValues(stage)
	}
	e.samples.Store(&[]sample{})
	reg.MustRegister(e, e.errors, e.last, e.duration)
	return e
}

// Describe implements prometheus.Collector for the Application series.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{hourlyCostDesc, idleHourlyCostDesc, estimatedHourlyCostDesc, workloadInfoDesc} {
		ch <- d
	}
}

// Collect implements prometheus.Collector, serving the last refresh.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	for _, s := range *e.samples.Load() {
		ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, s.value, s.labels...)
	}
}

// Ready reports whether the exporter has finished its first refresh.
func (e *Exporter) Ready() bool { return e.ran.Load() }

// Run refreshes at once and then every interval until ctx is done.
func (e *Exporter) Run(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		e.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Refresh measures every Application and replaces what is served.
func (e *Exporter) Refresh(ctx context.Context) {
	start := e.now()
	defer func() {
		e.duration.Set(e.now().Sub(start).Seconds())
		e.ran.Store(true)
	}()

	apps, err := e.apps.List(ctx)
	if err != nil {
		e.fail(StageApplications, err)
		e.samples.Store(&[]sample{})
		return
	}

	samples := e.costSamples(ctx, apps)
	samples = append(samples, e.estimateSamples(ctx, apps)...)
	e.samples.Store(&samples)
	e.last.Set(float64(start.Unix()))
}

func (e *Exporter) fail(stage string, err error) {
	e.errors.WithLabelValues(stage).Inc()
	e.logger.Printf("Exporter: %s: %v", stage, err)
}

// costSamples measures each namespace once and attributes its pods to the Applications whose
// workloads own them. An Application with a workload in a namespace that could not be measured is left
// out, rather than exported with part of its cost.
func (e *Exporter) costSamples(ctx context.Context, apps []Application) []sample {
	namespaces := map[string]bool{}
	for _, app := range apps {
		for _, w := range app.Workloads {
			namespaces[w.Namespace] = true
		}
	}
	measured := make(map[string]Costs, len(namespaces))
	for _, ns := range sortedKeys(namespaces) {
		costs, err := e.costs.RecentCosts(ctx, ns, Window)
		if err != nil {
			e.fail(StageOpenCost, err)
			continue
		}
		measured[ns] = costs
	}

	var samples []sample
	for _, app := range apps {
		if len(app.Workloads) == 0 {
			continue
		}
		labels := []string{app.Ref.Namespace, app.Ref.Name}
		for _, w := range app.Workloads {
			samples = append(samples, sample{workloadInfoDesc, 1, append(labels[:2:2], w.Namespace, w.Kind, w.Name)})
		}

		var total opencost.HourlyCost
		complete := true
		for ns := range appNamespaces(app) {
			costs, ok := measured[ns]
			if !ok {
				complete = false
				break
			}
			total = add(total, costs.HourlyCostOf(app.Workloads))
		}
		if !complete {
			continue
		}
		for _, r := range []struct {
			resource string
			value    float64
		}{
			{"cpu", total.CPU},
			{"memory", total.Memory},
			{"gpu", total.GPU},
			{"storage", total.Storage},
			{"network", total.Network},
			{"load_balancer", total.LoadBalancer},
		} {
			samples = append(samples, sample{hourlyCostDesc, r.value, append(labels[:2:2], r.resource)})
		}
		samples = append(samples,
			sample{idleHourlyCostDesc, total.CPUIdle, append(labels[:2:2], "cpu")},
			sample{idleHourlyCostDesc, total.MemoryIdle, append(labels[:2:2], "memory")},
		)
	}
	return samples
}

// estimateSamples converts the cloud estimates of Applications that still exist to cost per hour.
func (e *Exporter) estimateSamples(ctx context.Context, apps []Application) []sample {
	estimates, err := e.estimates.ListCloudEstimates(ctx)
	if err != nil {
		e.fail(StageEstimates, err)
		return nil
	}
	exists := make(map[models.AppRef]bool, len(apps))
	for _, app := range apps {
		exists[app.Ref] = true
	}
	var samples []sample
	for _, est := range estimates {
		if !exists[est.App] {
			continue
		}
		samples = append(samples, sample{
			estimatedHourlyCostDesc,
			est.Estimate.TotalMonthlyCost / infracostMonthHours,
			[]string{est.App.Namespace, est.App.Name, est.Estimate.Source},
		})
	}
	return samples
}

// appNamespaces are the namespaces an Application's workloads run in.
func appNamespaces(app Application) map[string]bool {
	namespaces := map[string]bool{}
	for _, w := range app.Workloads {
		namespaces[w.Namespace] = true
	}
	return namespaces
}

func add(a, b opencost.HourlyCost) opencost.HourlyCost {
	return opencost.HourlyCost{
		CPU: a.CPU + b.CPU, Memory: a.Memory + b.Memory, GPU: a.GPU + b.GPU, Storage: a.Storage + b.Storage,
		Network: a.Network + b.Network, LoadBalancer: a.LoadBalancer + b.LoadBalancer,
		CPUIdle: a.CPUIdle + b.CPUIdle, MemoryIdle: a.MemoryIdle + b.MemoryIdle,
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
