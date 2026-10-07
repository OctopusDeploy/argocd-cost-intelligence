package exporter

import (
	"context"
	"errors"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/models"
	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/opencost"
)

var refreshedAt = time.Date(2026, 10, 4, 7, 0, 0, 0, time.UTC)

type fakeApps struct {
	apps []Application
	err  error
}

func (f *fakeApps) List(context.Context) ([]Application, error) { return f.apps, f.err }

// fakeCosts is a namespace measured by OpenCost: each workload's pods cost what workloads says.
type fakeCosts struct {
	namespace string
	workloads map[string]opencost.HourlyCost
}

func (c fakeCosts) HourlyCostOf(resources models.ManagedResources) opencost.HourlyCost {
	var total opencost.HourlyCost
	for _, r := range resources {
		if r.Namespace == c.namespace {
			total = add(total, c.workloads[r.Name])
		}
	}
	return total
}

type fakeSource struct {
	mu        sync.Mutex
	workloads map[string]map[string]opencost.HourlyCost // by namespace, then workload name
	failing   map[string]bool
	asked     []string
	window    time.Duration
}

func (s *fakeSource) RecentCosts(_ context.Context, namespace string, window time.Duration) (Costs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, namespace)
	s.window = window
	if s.failing[namespace] {
		return nil, errors.New("opencost returned status 500")
	}
	return fakeCosts{namespace: namespace, workloads: s.workloads[namespace]}, nil
}

type fakeEstimates struct {
	estimates []models.AppCloudEstimate
	err       error
}

func (f *fakeEstimates) ListCloudEstimates(context.Context) ([]models.AppCloudEstimate, error) {
	return f.estimates, f.err
}

func workload(kind, name, namespace string) models.ManagedResource {
	return models.ManagedResource{Kind: kind, Name: name, Namespace: namespace}
}

var (
	shop  = Application{Ref: models.AppRef{Namespace: "argocd", Name: "shop"}, Workloads: models.ManagedResources{workload("Deployment", "web", "shop"), workload("StatefulSet", "db", "shop-data")}}
	redis = Application{Ref: models.AppRef{Namespace: "argocd", Name: "redis"}, Workloads: models.ManagedResources{workload("Deployment", "redis", "argocd")}}
	// sameNameElsewhere is another team's Application with redis's name.
	sameNameElsewhere = Application{Ref: models.AppRef{Namespace: "team-a", Name: "redis"}, Workloads: models.ManagedResources{workload("Deployment", "redis", "team-a")}}
	// configOnly manages no pods.
	configOnly = Application{Ref: models.AppRef{Namespace: "argocd", Name: "config"}, Workloads: models.ManagedResources{}}
)

func cluster() *fakeSource {
	return &fakeSource{workloads: map[string]map[string]opencost.HourlyCost{
		"shop":      {"web": {CPU: 0.04, Memory: 0.01, Network: 0.002, CPUIdle: 0.03, MemoryIdle: 0.004}},
		"shop-data": {"db": {CPU: 0.02, Memory: 0.03, Storage: 0.005, LoadBalancer: 0.001}},
		"argocd":    {"redis": {CPU: 0.001, Memory: 0.002}},
		"team-a":    {"redis": {CPU: 0.5, GPU: 0.25}},
	}}
}

func newExporter(apps *fakeApps, costs *fakeSource, estimates *fakeEstimates) (*Exporter, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	e := New(apps, costs, estimates, time.Minute, log.New(io.Discard, "", 0), reg)
	e.now = func() time.Time { return refreshedAt }
	return e, reg
}

const shopSeries = `
argocd_cost_intelligence_application_hourly_cost{name="shop",namespace="argocd",resource="cpu"} 0.06
argocd_cost_intelligence_application_hourly_cost{name="shop",namespace="argocd",resource="gpu"} 0
argocd_cost_intelligence_application_hourly_cost{name="shop",namespace="argocd",resource="load_balancer"} 0.001
argocd_cost_intelligence_application_hourly_cost{name="shop",namespace="argocd",resource="memory"} 0.04
argocd_cost_intelligence_application_hourly_cost{name="shop",namespace="argocd",resource="network"} 0.002
argocd_cost_intelligence_application_hourly_cost{name="shop",namespace="argocd",resource="storage"} 0.005
`

const (
	hourlyHeader = `
# HELP argocd_cost_intelligence_application_hourly_cost Cost per hour of the pods the Argo CD Application's workloads own, averaged over the last hour, in OpenCost's currency. resource is cpu, memory, gpu, storage, network or load_balancer; together they are the Application's cost.
# TYPE argocd_cost_intelligence_application_hourly_cost gauge
`
	idleHeader = `
# HELP argocd_cost_intelligence_application_idle_hourly_cost Part of the Application's hourly cost paid for requested CPU or memory its pods did not use, in OpenCost's currency. A subset of application_hourly_cost for the same resource.
# TYPE argocd_cost_intelligence_application_idle_hourly_cost gauge
`
	estimateHeader = `
# HELP argocd_cost_intelligence_application_estimated_hourly_cost Cost per hour of the Application's cloud services as estimated by an IaC cost tool and pushed from CI, converted from its monthly figure with 730 hours a month.
# TYPE argocd_cost_intelligence_application_estimated_hourly_cost gauge
`
	infoHeader = `
# HELP argocd_cost_intelligence_application_workload_info A pod-owning workload the Argo CD Application manages, always 1, for joining per-workload metrics to Applications.
# TYPE argocd_cost_intelligence_application_workload_info gauge
`
)

func TestRefresh_ExportsEachApplicationsCostByResource(t *testing.T) {
	t.Parallel()

	costs := cluster()
	e, _ := newExporter(&fakeApps{apps: []Application{redis, shop, sameNameElsewhere, configOnly}}, costs, &fakeEstimates{})

	e.Refresh(context.Background())

	assert.Equal(t, []string{"argocd", "shop", "shop-data", "team-a"}, costs.asked, "each namespace once")
	assert.Equal(t, Window, costs.window)
	require.NoError(t, testutil.CollectAndCompare(e, strings.NewReader(hourlyHeader+`
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="cpu"} 0.001
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="gpu"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="load_balancer"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="memory"} 0.002
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="network"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="storage"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="team-a",resource="cpu"} 0.5
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="team-a",resource="gpu"} 0.25
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="team-a",resource="load_balancer"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="team-a",resource="memory"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="team-a",resource="network"} 0
argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="team-a",resource="storage"} 0
`+shopSeries+idleHeader+`
argocd_cost_intelligence_application_idle_hourly_cost{name="redis",namespace="argocd",resource="cpu"} 0
argocd_cost_intelligence_application_idle_hourly_cost{name="redis",namespace="argocd",resource="memory"} 0
argocd_cost_intelligence_application_idle_hourly_cost{name="redis",namespace="team-a",resource="cpu"} 0
argocd_cost_intelligence_application_idle_hourly_cost{name="redis",namespace="team-a",resource="memory"} 0
argocd_cost_intelligence_application_idle_hourly_cost{name="shop",namespace="argocd",resource="cpu"} 0.03
argocd_cost_intelligence_application_idle_hourly_cost{name="shop",namespace="argocd",resource="memory"} 0.004
`+infoHeader+`
argocd_cost_intelligence_application_workload_info{name="redis",namespace="argocd",workload_kind="Deployment",workload_name="redis",workload_namespace="argocd"} 1
argocd_cost_intelligence_application_workload_info{name="redis",namespace="team-a",workload_kind="Deployment",workload_name="redis",workload_namespace="team-a"} 1
argocd_cost_intelligence_application_workload_info{name="shop",namespace="argocd",workload_kind="Deployment",workload_name="web",workload_namespace="shop"} 1
argocd_cost_intelligence_application_workload_info{name="shop",namespace="argocd",workload_kind="StatefulSet",workload_name="db",workload_namespace="shop-data"} 1
`)))
}

func TestRefresh_LeavesOutAnApplicationWithANamespaceItCouldNotMeasure(t *testing.T) {
	t.Parallel()

	costs := cluster()
	costs.failing = map[string]bool{"shop-data": true}
	e, _ := newExporter(&fakeApps{apps: []Application{redis, shop}}, costs, &fakeEstimates{})

	e.Refresh(context.Background())

	got := gatherText(t, e)
	assert.NotContains(t, got, `argocd_cost_intelligence_application_hourly_cost{name="shop"`,
		"shop's cost would be missing its database")
	assert.NotContains(t, got, `argocd_cost_intelligence_application_idle_hourly_cost{name="shop"`)
	assert.Contains(t, got, `argocd_cost_intelligence_application_hourly_cost{name="redis",namespace="argocd",resource="cpu"} 0.001`)
	assert.Contains(t, got, `argocd_cost_intelligence_application_workload_info{name="shop",namespace="argocd",workload_kind="StatefulSet"`,
		"what the Application manages is known without OpenCost")
	assert.InDelta(t, 1, testutil.ToFloat64(e.errors.WithLabelValues(StageOpenCost)), 0)
	assert.InDelta(t, float64(refreshedAt.Unix()), testutil.ToFloat64(e.last), 0, "the refresh still used the current Applications")
}

func TestRefresh_ExportsNothingWithoutTheApplicationList(t *testing.T) {
	t.Parallel()

	apps := &fakeApps{apps: []Application{redis}}
	e, _ := newExporter(apps, cluster(), &fakeEstimates{})
	e.Refresh(context.Background())
	require.Positive(t, testutil.CollectAndCount(e))
	lastSuccess := testutil.ToFloat64(e.last)

	apps.err = errors.New("listing Argo CD Applications: forbidden")
	e.now = func() time.Time { return refreshedAt.Add(5 * time.Minute) }
	e.Refresh(context.Background())

	assert.Zero(t, testutil.CollectAndCount(e), "no older values shown as current")
	assert.InDelta(t, 1, testutil.ToFloat64(e.errors.WithLabelValues(StageApplications)), 0)
	assert.InDelta(t, lastSuccess, testutil.ToFloat64(e.last), 0, "the timestamp shows how stale the exporter is")
	assert.True(t, e.Ready())
}

func TestRefresh_ConvertsEstimatesOfApplicationsThatExist(t *testing.T) {
	t.Parallel()

	estimates := &fakeEstimates{estimates: []models.AppCloudEstimate{
		{App: shop.Ref, Estimate: models.CloudServiceEstimate{Source: "infracost", TotalMonthlyCost: 730}},
		{App: shop.Ref, Estimate: models.CloudServiceEstimate{Source: "terracost", TotalMonthlyCost: 146}},
		// An Application deleted since CI pushed its estimate.
		{App: models.AppRef{Namespace: "argocd", Name: "retired"}, Estimate: models.CloudServiceEstimate{Source: "infracost", TotalMonthlyCost: 999}},
	}}
	e, _ := newExporter(&fakeApps{apps: []Application{shop, configOnly}}, cluster(), estimates)

	e.Refresh(context.Background())

	require.NoError(t, testutil.CollectAndCompare(e, strings.NewReader(estimateHeader+`
argocd_cost_intelligence_application_estimated_hourly_cost{name="shop",namespace="argocd",source="infracost"} 1
argocd_cost_intelligence_application_estimated_hourly_cost{name="shop",namespace="argocd",source="terracost"} 0.2
`), "argocd_cost_intelligence_application_estimated_hourly_cost"))

	estimates.err = errors.New("connection refused")
	e.Refresh(context.Background())

	got := gatherText(t, e)
	assert.NotContains(t, got, "estimated_hourly_cost")
	assert.Contains(t, got, `argocd_cost_intelligence_application_hourly_cost{name="shop"`, "costs do not depend on estimates")
	assert.InDelta(t, 1, testutil.ToFloat64(e.errors.WithLabelValues(StageEstimates)), 0)
}

func TestRefresh_ADeletedApplicationDisappears(t *testing.T) {
	t.Parallel()

	apps := &fakeApps{apps: []Application{redis, shop}}
	e, _ := newExporter(apps, cluster(), &fakeEstimates{})
	e.Refresh(context.Background())
	require.Contains(t, gatherText(t, e), `name="shop"`)

	apps.apps = []Application{redis}
	e.Refresh(context.Background())

	assert.NotContains(t, gatherText(t, e), `name="shop"`)
}

func TestNew_StartsEveryStageAtZeroAndIsNotReady(t *testing.T) {
	t.Parallel()

	e, reg := newExporter(&fakeApps{}, cluster(), &fakeEstimates{})

	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP argocd_cost_intelligence_exporter_refresh_errors_total Failed steps of the exporter's refreshes, by stage: applications, opencost (one per namespace) or estimates.
# TYPE argocd_cost_intelligence_exporter_refresh_errors_total counter
argocd_cost_intelligence_exporter_refresh_errors_total{stage="applications"} 0
argocd_cost_intelligence_exporter_refresh_errors_total{stage="estimates"} 0
argocd_cost_intelligence_exporter_refresh_errors_total{stage="opencost"} 0
`), "argocd_cost_intelligence_exporter_refresh_errors_total"))
	assert.False(t, e.Ready())
	assert.Zero(t, testutil.CollectAndCount(e))
}

func TestRun_RefreshesAtOnceAndStopsWithItsContext(t *testing.T) {
	t.Parallel()

	costs := cluster()
	e, _ := newExporter(&fakeApps{apps: []Application{redis}}, costs, &fakeEstimates{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		e.Run(ctx)
		close(done)
	}()
	require.Eventually(t, e.Ready, 5*time.Second, 10*time.Millisecond, "Run did not refresh at once")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}

// gatherText is the exporter's Application series in the text format.
func gatherText(t *testing.T, c prometheus.Collector) string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(c))
	families, err := reg.Gather()
	require.NoError(t, err)
	var b strings.Builder
	for _, f := range families {
		for _, m := range f.GetMetric() {
			labels := make([]string, 0, len(m.GetLabel()))
			for _, l := range m.GetLabel() {
				labels = append(labels, l.GetName()+`="`+l.GetValue()+`"`)
			}
			b.WriteString(f.GetName() + "{" + strings.Join(labels, ",") + "} ")
			b.WriteString(strconv.FormatFloat(m.GetGauge().GetValue(), 'f', -1, 64))
			b.WriteString("\n")
		}
	}
	return b.String()
}
