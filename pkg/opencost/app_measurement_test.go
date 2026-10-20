package opencost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

var day = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)

// podAlloc is a pod's allocation over a full day unless the caller changes Start and Minutes.
func podAlloc(pod, kind, controller string, cpuCost, ramCost float64) AllocationData {
	return AllocationData{
		Start:     day,
		End:       day.Add(24 * time.Hour),
		Minutes:   24 * 60,
		CPUCost:   cpuCost,
		RAMCost:   ramCost,
		TotalCost: cpuCost + ramCost,
		Properties: AllocationProperties{
			Namespace: "argocd", Pod: pod, ControllerKind: kind, Controller: controller,
		},
	}
}

// sharedNamespace is the argocd namespace of the demo cluster: Argo CD's own pods, with the redis
// application's Deployment and CronJob run beside them.
func sharedNamespace() *PodAllocations {
	return &PodAllocations{resp: AllocationResponse{Data: []map[string]AllocationData{{
		"argocd-redis-1":      podAlloc("argocd-redis-1", "deployment", "argocd-redis", 0.2, 0.05),
		"argocd-server-1":     podAlloc("argocd-server-1", "deployment", "argocd-server", 0.004, 0.001),
		"redis-1":             podAlloc("redis-1", "deployment", "redis", 0.001, 0.02),
		"test-cronjob-2912-x": podAlloc("test-cronjob-2912-x", "job", "test-cronjob-29123456", 0.01, 0),
		"__idle__":            {TotalCost: 5},
	}}}}
}

var redisResources = models.ManagedResources{
	{Kind: "Deployment", Name: "redis", Namespace: "argocd"},
	{Kind: "CronJob", Name: "test-cronjob", Namespace: "argocd"},
}

func TestForApplication_MeasuresOnlyTheApplicationsPods(t *testing.T) {
	t.Parallel()

	got := sharedNamespace().ForApplication(redisResources)

	assert.Equal(t, redisResources, got.Controllers)
	names := make([]string, 0, len(got.Pods))
	for _, pod := range got.Pods {
		names = append(names, pod.Name)
	}
	assert.ElementsMatch(t, []string{"redis-1", "test-cronjob-2912-x"}, names)
	assert.InDelta(t, (0.001+0.02+0.01)*30, got.TotalMonthlyCost, 1e-9)
	assert.InDelta(t, (0.001+0.01)*30, got.OpenCostOutput.AllocationTotals.CPUCost, 1e-9)
	assert.InDelta(t, 0.02*30, got.OpenCostOutput.AllocationTotals.MemoryCost, 1e-9)
	assert.Nil(t, got.OpenCostOutput.ResourceDetails, "raw allocations are stored with the namespace only")
}

func TestForApplication_ProjectsFromThePeriodItsPodsRan(t *testing.T) {
	t.Parallel()

	// The namespace has run all day; the application was deployed into it two hours before the end.
	young := podAlloc("web-1", "deployment", "web", 1, 0)
	young.Start = day.Add(22 * time.Hour)
	young.Minutes = 120
	allocations := &PodAllocations{resp: AllocationResponse{Data: []map[string]AllocationData{{
		"old-1": podAlloc("old-1", "deployment", "old", 24, 0),
		"web-1": young,
	}}}}

	got := allocations.ForApplication(models.ManagedResources{{Kind: "Deployment", Name: "web"}})

	assert.InDelta(t, 360.0, got.TotalMonthlyCost, 1e-9, "2 hours of cost projected onto 720")
	assert.InDelta(t, 2.0, got.OpenCostOutput.MeasuredHours, 1e-9)
}

func TestForApplication_ApplicationWithoutPods(t *testing.T) {
	t.Parallel()

	got := sharedNamespace().ForApplication(models.ManagedResources{})

	assert.Zero(t, got.TotalMonthlyCost)
	assert.Empty(t, got.Pods)
	assert.Nil(t, got.Efficiency)
}

func TestPodEfficiency_PricesTheUnusedShareOfEachRequest(t *testing.T) {
	t.Parallel()

	api := podAlloc("api-1", "deployment", "api", 10, 4)
	api.CPUCoreRequestAverage, api.CPUCoreUsageAverage, api.CPUCoreLimitAverage = 1, 0.25, 2
	api.RAMByteRequestAverage, api.RAMByteUsageAverage = 1000, 500
	api.RawAllocationOnly = &RawAllocationOnly{CPUCoreUsageMax: 0.5, RAMByteUsageMax: 600}
	// Uses more than it requests: it pays for its use, and nothing is idle.
	busy := podAlloc("busy-1", "deployment", "busy", 6, 0)
	busy.CPUCoreRequestAverage, busy.CPUCoreUsageAverage = 0.5, 0.75
	busy.RawAllocationOnly = &RawAllocationOnly{CPUCoreUsageMax: 1}

	got := podEfficiency(AllocationResponse{Data: []map[string]AllocationData{{"api-1": api, "busy-1": busy}}})

	require.NotNil(t, got)
	assert.InDelta(t, 1.5, got.CPUCoresRequested, 1e-9)
	assert.InDelta(t, 1.0, got.CPUCoresUsed, 1e-9)
	assert.InDelta(t, 2.0, got.CPUCoresLimit, 1e-9)
	assert.InDelta(t, 1.0/1.5, got.CPUEfficiency, 1e-9)
	assert.InDelta(t, 0.5, got.RAMEfficiency, 1e-9)
	assert.InDelta(t, (1.0/1.5*16+0.5*4)/20, got.TotalEfficiency, 1e-9, "weighted by cost")
	assert.InDelta(t, 10*0.75*30, got.CPUIdleCost, 1e-9, "three quarters of api's CPU request is unused")
	assert.InDelta(t, 4*0.5*30, got.RAMIdleCost, 1e-9)
	assert.InDelta(t, 1.5, got.CPUCoresUsedMax, 1e-9)
	assert.InDelta(t, 600.0, got.RAMBytesUsedMax, 1e-9)
}

func TestEfficiency_MeasuresTheNamespacesIdleCostFromItsPods(t *testing.T) {
	t.Parallel()

	// Two applications share a namespace that has run for six hours.
	web := podAlloc("web-1", "deployment", "web", 2, 1)
	web.CPUCoreRequestAverage, web.CPUCoreUsageAverage = 1, 0.25
	web.RAMByteRequestAverage, web.RAMByteUsageAverage = 1000, 500
	api := podAlloc("api-1", "deployment", "api", 1, 0)
	api.CPUCoreRequestAverage, api.CPUCoreUsageAverage = 1, 0.5
	for _, a := range []*AllocationData{&web, &api} {
		a.Start = day.Add(18 * time.Hour)
		a.Minutes = 360
	}
	pods := &PodAllocations{resp: AllocationResponse{Data: []map[string]AllocationData{{
		"web-1": web,
		"api-1": api,
		// OpenCost's unallocated capacity requests nothing of the namespace's.
		"__idle__": {CPUCost: 9, CPUCoreRequestAverage: 1},
	}}}}

	got := pods.Efficiency()

	require.NotNil(t, got)
	assert.InDelta(t, (2*0.75+1*0.5)*120, got.CPUIdleCost, 1e-9, "6 hours of unused requests projected onto 720")
	assert.InDelta(t, 1*0.5*120, got.RAMIdleCost, 1e-9)
	var apps float64
	for _, controller := range []string{"web", "api"} {
		app := pods.ForApplication(models.ManagedResources{{Kind: "Deployment", Name: controller}})
		apps += app.Efficiency.CPUIdleCost + app.Efficiency.RAMIdleCost
	}
	assert.InDelta(t, got.CPUIdleCost+got.RAMIdleCost, apps, 1e-9, "the applications' idle cost adds up to the namespace's")
}

func TestEfficiency_NamespaceWithoutPods(t *testing.T) {
	t.Parallel()

	pods := &PodAllocations{resp: AllocationResponse{Data: []map[string]AllocationData{{"__idle__": {CPUCost: 9}}}}}

	assert.Nil(t, pods.Efficiency())
}

func TestPodEfficiency_CountsAReplacedPodAndItsReplacementAsOne(t *testing.T) {
	t.Parallel()

	before := podAlloc("web-a", "deployment", "web", 3, 0)
	before.End = day.Add(18 * time.Hour)
	before.Minutes = 18 * 60
	before.CPUCoreRequestAverage, before.CPUCoreUsageAverage = 1, 0.5
	after := podAlloc("web-b", "deployment", "web", 1, 0)
	after.Start = day.Add(18 * time.Hour)
	after.Minutes = 6 * 60
	after.CPUCoreRequestAverage, after.CPUCoreUsageAverage = 1, 0.5

	got := podEfficiency(AllocationResponse{Data: []map[string]AllocationData{{"web-a": before, "web-b": after}}})

	require.NotNil(t, got)
	assert.InDelta(t, 1.0, got.CPUCoresRequested, 1e-9)
	assert.InDelta(t, 0.5, got.CPUCoresUsed, 1e-9)
}

func TestPodEfficiency_NilWithoutPods(t *testing.T) {
	t.Parallel()

	assert.Nil(t, podEfficiency(AllocationResponse{Data: []map[string]AllocationData{{"__idle__": {CPUCost: 1}}}}))
}

func TestGetDailyTimeSeries_ScopedToTheApplicationsControllers(t *testing.T) {
	t.Parallel()

	controller := func(start time.Time, kind, name string, cost float64) AllocationData {
		return AllocationData{
			Start: start, End: start.Add(24 * time.Hour), CPUCost: cost, TotalCost: cost,
			Properties: AllocationProperties{ControllerKind: kind, Controller: name},
		}
	}
	var aggregates []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aggregates = append(aggregates, r.URL.Query().Get("aggregate"))
		assert.Equal(t, `namespace:"argocd"`, r.URL.Query().Get("filter"))
		_ = json.NewEncoder(w).Encode(AllocationResponse{Code: 200, Data: []map[string]AllocationData{
			{
				"deployment:redis":        controller(day, "deployment", "redis", 0.02),
				"deployment:argocd-redis": controller(day, "deployment", "argocd-redis", 0.25),
				"__unallocated__":         controller(day, "", "", 1),
			},
			{"deployment:argocd-redis": controller(day.Add(24*time.Hour), "deployment", "argocd-redis", 0.25)},
		}})
	}))
	defer srv.Close()
	client := NewClient(srv.URL, 5*time.Second, nil)

	got, err := client.GetDailyTimeSeries(context.Background(), "argocd", day, day.Add(24*time.Hour), redisResources)
	require.NoError(t, err)
	require.Len(t, got, 1, "a day the application did not run is left out")
	assert.Equal(t, "2026-10-02", got[0].Date)
	assert.InDelta(t, 0.02, got[0].TotalCost, 1e-9)

	whole, err := client.GetDailyTimeSeries(context.Background(), "argocd", day, day.Add(24*time.Hour), nil)
	require.NoError(t, err)
	assert.Len(t, whole, 2)
	assert.InDelta(t, 1.27, whole[0].TotalCost, 1e-9)
	assert.Equal(t, []string{"controller", "namespace"}, aggregates)
}
