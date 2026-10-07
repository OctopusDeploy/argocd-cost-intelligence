package rightsizing

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/models"
)

const (
	// cpuPrice is per core-hour and memPrice per GiB-hour: about $23 a core and $3.10 a GiB a month.
	cpuPrice = 0.032
	memPrice = 3.10 / 720
	gi       = 1024 * mi
)

// usage is one container's run: its requests, usage and memory limit (in cores and Mi).
type usage struct {
	pod, container, controllerKind, controller string
	hours                                      float64
	cpuRequest, cpuUsage                       float64
	memRequestMi, memUsageMi, memLimitMi       float64
}

// run is a container allocation as OpenCost reports it: charged for the larger of request and usage.
func run(u usage) models.ContainerAllocation {
	cpuHours := math.Max(u.cpuRequest, u.cpuUsage) * u.hours
	ramHours := math.Max(u.memRequestMi, u.memUsageMi) * mi * u.hours
	return models.ContainerAllocation{
		Pod: u.pod, Container: u.container, ControllerKind: u.controllerKind, Controller: u.controller,
		Hours:           u.hours,
		CPURequestCores: u.cpuRequest, CPUUsageCores: u.cpuUsage,
		RAMRequestBytes: u.memRequestMi * mi, RAMUsageBytes: u.memUsageMi * mi, RAMLimitBytes: u.memLimitMi * mi,
		CPUCoreHours: cpuHours, RAMByteHours: ramHours,
		CPUCost: cpuHours * cpuPrice, RAMCost: ramHours / gi * memPrice,
	}
}

func rec(container string, cpu, memMi float64) ContainerRecommendation {
	return ContainerRecommendation{
		Name:       container,
		Target:     models.Resources{CPUCores: cpu, MemoryBytes: memMi * mi},
		LowerBound: models.Resources{CPUCores: cpu * 0.7, MemoryBytes: memMi * 0.7 * mi},
		UpperBound: models.Resources{CPUCores: cpu * 1.8, MemoryBytes: memMi * 1.7 * mi},
	}
}

// The shop application: web (3 pods with an istio-proxy sidecar) and worker (2 pods) have
// recommendation-only VPAs, redis a VPA in Auto mode, batch a VPA still without a recommendation,
// and nightly-report none.
var shopResources = models.ManagedResources{
	{Kind: "Service", Name: "web", Namespace: "shop"},
	{Kind: "Deployment", Name: "web", Namespace: "shop"},
	{Kind: "Deployment", Name: "worker", Namespace: "shop"},
	{Kind: "StatefulSet", Name: "redis", Namespace: "shop"},
	{Kind: "Deployment", Name: "batch", Namespace: "shop"},
	{Kind: "CronJob", Name: "nightly-report", Namespace: "shop"},
}

var shopVPAs = []VPA{
	{TargetKind: "Deployment", TargetName: "web", UpdateMode: "Off", Containers: []ContainerRecommendation{
		rec("web", 0.114, 371), rec("istio-proxy", 0.023, 90),
	}},
	{TargetKind: "Deployment", TargetName: "worker", UpdateMode: "Off", LowConfidence: true, Containers: []ContainerRecommendation{
		rec("worker", 1.38, 1490),
	}},
	{TargetKind: "StatefulSet", TargetName: "redis", UpdateMode: "Auto", Containers: []ContainerRecommendation{rec("redis", 0.1, 256)}},
	{TargetKind: "Deployment", TargetName: "batch", UpdateMode: "Off"},
	// Another application's workload in the same namespace.
	{TargetKind: "Deployment", TargetName: "not-ours", UpdateMode: "Off", Containers: []ContainerRecommendation{rec("app", 0.01, 16)}},
}

func shopContainers() []models.ContainerAllocation {
	containers := make([]models.ContainerAllocation, 0, 11)
	for _, pod := range []string{"web-6d4cf56db6-a1", "web-6d4cf56db6-b2", "web-6d4cf56db6-c3"} {
		containers = append(containers,
			run(usage{pod, "web", "deployment", "web", 24, 0.5, 0.05, 1024, 300, 512}),
			run(usage{pod, "istio-proxy", "deployment", "web", 24, 0.1, 0.01, 128, 60, 1024}),
		)
	}
	for _, pod := range []string{"worker-7f9-a1", "worker-7f9-b2"} {
		containers = append(containers, run(usage{pod, "worker", "deployment", "worker", 24, 1, 1.3, 2048, 1400, 2048}))
	}
	containers = append(containers,
		run(usage{"redis-0", "redis", "statefulset", "redis", 24, 0.2, 0.05, 512, 200, 0}),
		run(usage{"nightly-report-29312345-x1", "report", "job", "nightly-report-29312345", 1, 0.5, 0.4, 512, 300, 0}),
		run(usage{"not-ours-1", "app", "deployment", "not-ours", 24, 1, 0.01, 1024, 10, 0}),
	)
	return containers
}

func workload(t *testing.T, result models.Rightsizing, name string) models.WorkloadRightsizing {
	t.Helper()
	for _, w := range result.Workloads {
		if w.Name == name {
			return w
		}
	}
	require.Failf(t, "workload not found", "%s", name)
	return models.WorkloadRightsizing{}
}

func TestRecommend_MatchesVPAsToTheApplicationsWorkloads(t *testing.T) {
	t.Parallel()

	result := Recommend("shop", shopResources, shopVPAs, shopContainers())

	assert.True(t, result.Available)
	names := make([]string, 0, len(result.Workloads))
	for _, w := range result.Workloads {
		names = append(names, w.Name)
	}
	assert.Equal(t, []string{"batch", "nightly-report", "redis", "web", "worker"}, names,
		"the application's pod owners only: no Service, and no other application's VPA")

	assert.Equal(t, models.RightsizingRecommended, workload(t, result, "web").Status)
	assert.Equal(t, models.RightsizingManaged, workload(t, result, "redis").Status)
	assert.Equal(t, "Auto", workload(t, result, "redis").UpdateMode)
	assert.Empty(t, workload(t, result, "redis").Containers)
	assert.Equal(t, models.RightsizingPending, workload(t, result, "batch").Status)
	none := workload(t, result, "nightly-report")
	assert.Equal(t, models.RightsizingNone, none.Status)
	assert.Empty(t, none.UpdateMode)
	assert.Equal(t, 1, none.Pods, "a CronJob's run counts as a pod, through its Job")
	assert.True(t, workload(t, result, "worker").LowConfidence)
}

func TestRecommend_AWideUpperBoundIsUnsettled(t *testing.T) {
	t.Parallel()

	resources := models.ManagedResources{{Kind: "Deployment", Name: "api", Namespace: "shop"}}
	containers := []models.ContainerAllocation{
		run(usage{"api-1", "api", "deployment", "api", 24, 0.5, 0.01, 256, 50, 0}),
		run(usage{"api-1", "sidecar", "deployment", "api", 24, 0.1, 0.01, 64, 10, 0}),
	}
	// A VPA with a few minutes of history: its upper bounds are far above the targets.
	young := rec("api", 0.015, 100)
	young.UpperBound = models.Resources{CPUCores: 16.4, MemoryBytes: 3 * 100 * mi}
	wideMemory := rec("sidecar", 0.015, 100)
	wideMemory.UpperBound.MemoryBytes = 301 * mi
	vpas := []VPA{{TargetKind: "Deployment", TargetName: "api", UpdateMode: "Off", Containers: []ContainerRecommendation{young, wideMemory}}}

	api := Recommend("shop", resources, vpas, containers).Workloads[0]

	require.Len(t, api.Containers, 2)
	assert.True(t, api.Containers[0].Unsettled, "CPU's upper bound is a thousand times the target")
	assert.True(t, api.Containers[1].Unsettled, "memory's upper bound is just over three times the target")
	assert.True(t, api.LowConfidence)

	settled := Recommend("shop", resources, []VPA{{
		TargetKind: "Deployment", TargetName: "api", UpdateMode: "Off",
		Containers: []ContainerRecommendation{rec("api", 0.015, 100)},
	}}, containers).Workloads[0]
	assert.False(t, settled.Containers[0].Unsettled)
	assert.False(t, settled.LowConfidence)
}

func TestRecommend_ComparesEachContainerWithItsRequests(t *testing.T) {
	t.Parallel()

	web := workload(t, Recommend("shop", shopResources, shopVPAs, shopContainers()), "web")

	assert.Equal(t, 3, web.Pods)
	require.Len(t, web.Containers, 2)
	main := web.Containers[0]
	assert.Equal(t, "web", main.Name)
	assert.InDelta(t, 0.5, main.Requests.CPUCores, 1e-9)
	assert.InDelta(t, 1024*mi, main.Requests.MemoryBytes, 1)
	assert.InDelta(t, 512*mi, main.MemoryLimitBytes, 1)
	assert.InDelta(t, 0.114, main.Target.CPUCores, 1e-9)
	assert.InDelta(t, 371*1.7*mi, main.UpperBound.MemoryBytes, 1)
	// Rounded up to 10m and 16Mi.
	assert.Equal(t, models.Resources{CPUCores: 0.12, MemoryBytes: 384 * mi}, main.Suggested)
	assert.Equal(t, models.Resources{CPUCores: 0.03, MemoryBytes: 96 * mi}, web.Containers[1].Suggested)
}

func TestRecommend_PricesTheChangeTheWayOpenCostCharges(t *testing.T) {
	t.Parallel()

	result := Recommend("shop", shopResources, shopVPAs, shopContainers())
	web, worker := workload(t, result, "web"), workload(t, result, "worker")

	// web: 500m → 120m on 3 pods all month; 1Gi → 384Mi.
	assert.InDelta(t, cpuPrice*(0.12-0.5)*24*3*30, web.Containers[0].MonthlyChange.CPU, 1e-6)
	assert.InDelta(t, -26.27, web.Containers[0].MonthlyChange.CPU, 0.01)
	assert.InDelta(t, memPrice*(384.0-1024)/1024*24*3*30, web.Containers[0].MonthlyChange.Memory, 1e-6)
	// worker uses 1.3 cores of its 1-core request, and is already charged for them: raising the
	// request to 1.38 costs only the 0.08 above its usage.
	assert.InDelta(t, cpuPrice*0.08*24*2*30, worker.Containers[0].MonthlyChange.CPU, 1e-6)
	assert.Positive(t, worker.Containers[0].MonthlyChange.CPU)
	assert.Negative(t, worker.Containers[0].MonthlyChange.Memory, "2Gi → 1504Mi")

	var saving, increase, webTotal float64
	for _, w := range result.Workloads {
		for _, c := range w.Containers {
			for _, change := range []float64{c.MonthlyChange.CPU, c.MonthlyChange.Memory} {
				if change < 0 {
					saving -= change
				} else {
					increase += change
				}
				if w.Name == "web" {
					webTotal += change
				}
			}
		}
	}
	assert.InDelta(t, saving, result.MonthlySaving, 1e-9)
	assert.InDelta(t, increase, result.MonthlyIncrease, 1e-9)
	assert.InDelta(t, cpuPrice*0.08*24*2*30, result.MonthlyIncrease, 1e-6)
	assert.InDelta(t, webTotal, web.MonthlyChange, 1e-9)
}

func TestRecommend_LoweringARequestBelowUsageSavesOnlyDownToUsage(t *testing.T) {
	t.Parallel()

	resources := models.ManagedResources{{Kind: "Deployment", Name: "api", Namespace: "shop"}}
	vpas := []VPA{{TargetKind: "Deployment", TargetName: "api", UpdateMode: "Off", Containers: []ContainerRecommendation{rec("api", 0.5, 512)}}}
	containers := []models.ContainerAllocation{run(usage{"api-1", "api", "deployment", "api", 24, 1, 0.6, 1024, 600, 0})}

	api := Recommend("shop", resources, vpas, containers).Workloads[0]

	assert.InDelta(t, cpuPrice*(0.6-1)*24*30, api.Containers[0].MonthlyChange.CPU, 1e-6)
	assert.InDelta(t, memPrice*(600.0-1024)/1024*24*30, api.Containers[0].MonthlyChange.Memory, 1e-6)
}

func TestRecommend_LeavesOutContainersItCannotCompare(t *testing.T) {
	t.Parallel()

	resources := models.ManagedResources{{Kind: "Deployment", Name: "api", Namespace: "shop"}}
	// The VPA recommends for a container that did not run; the sidecar that ran has no recommendation.
	vpas := []VPA{{TargetKind: "Deployment", TargetName: "api", UpdateMode: "Off", Containers: []ContainerRecommendation{rec("init-only", 0.1, 64)}}}
	containers := []models.ContainerAllocation{run(usage{"api-1", "sidecar", "deployment", "api", 24, 0.1, 0.01, 64, 10, 0})}

	api := Recommend("shop", resources, vpas, containers).Workloads[0]

	assert.Equal(t, models.RightsizingRecommended, api.Status)
	assert.Empty(t, api.Containers)
	assert.Zero(t, api.MonthlyChange)
}

func TestRecommend_CountsAPodReplacedDuringTheDayOnce(t *testing.T) {
	t.Parallel()

	resources := models.ManagedResources{{Kind: "Deployment", Name: "api", Namespace: "shop"}}
	vpas := []VPA{{TargetKind: "Deployment", TargetName: "api", UpdateMode: "Off", Containers: []ContainerRecommendation{rec("api", 0.1, 64)}}}
	containers := []models.ContainerAllocation{
		run(usage{"api-old", "api", "deployment", "api", 10, 0.4, 0.01, 64, 10, 0}),
		run(usage{"api-new", "api", "deployment", "api", 14, 0.5, 0.01, 64, 10, 0}),
	}

	api := Recommend("shop", resources, vpas, containers).Workloads[0]

	assert.Equal(t, 1, api.Pods)
	assert.InDelta(t, 0.5, api.Containers[0].Requests.CPUCores, 1e-9, "the run that lasted longest gives the current request")
	assert.InDelta(t, cpuPrice*((0.1-0.4)*10+(0.1-0.5)*14)*30, api.Containers[0].MonthlyChange.CPU, 1e-6)
}

func TestRecommend_FindsARolloutsPodsThroughItsReplicaSets(t *testing.T) {
	t.Parallel()

	resources := models.ManagedResources{{Kind: "Rollout", Name: "api", Namespace: "shop"}}
	vpas := []VPA{{TargetKind: "Rollout", TargetName: "api", UpdateMode: "Off", Containers: []ContainerRecommendation{rec("api", 0.1, 64)}}}
	containers := []models.ContainerAllocation{run(usage{"api-7d9fbc6d5b-x1", "api", "replicaset", "api-7d9fbc6d5b", 24, 0.5, 0.01, 64, 10, 0})}

	api := Recommend("shop", resources, vpas, containers).Workloads[0]

	require.Len(t, api.Containers, 1)
	assert.Negative(t, api.MonthlyChange)
}

func TestRecommend_IgnoresResourcesInOtherNamespaces(t *testing.T) {
	t.Parallel()

	resources := models.ManagedResources{
		{Kind: "Deployment", Name: "api", Namespace: "elsewhere"},
		{Kind: "Deployment", Name: "web"},
	}

	result := Recommend("shop", resources, nil, nil)

	require.Len(t, result.Workloads, 1)
	assert.Equal(t, "web", result.Workloads[0].Name, "a resource listed without a namespace is in the app's own")
	assert.Equal(t, 0, result.Workloads[0].Pods)
}
