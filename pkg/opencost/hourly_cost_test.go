package opencost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var hour = time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)

// hourAlloc is a pod's allocation over the hour from hour, with a cost for every resource and a CPU
// and memory request it half uses.
func hourAlloc(pod, kind, controller string, minutes float64) AllocationData {
	return AllocationData{
		Start: hour.Add(time.Duration(60-minutes) * time.Minute), End: hour.Add(time.Hour), Minutes: minutes,
		CPUCost: 0.4, RAMCost: 0.2, GPUCost: 0.1, PVCost: 0.05, NetworkCost: 0.03, LoadBalancerCost: 0.02,
		SharedCost: 9, ExternalCost: 9,
		CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.5,
		RAMByteRequestAverage: 1 << 30, RAMByteUsageAverage: 1 << 29,
		Properties: AllocationProperties{Namespace: "argocd", Pod: pod, ControllerKind: kind, Controller: controller},
	}
}

func TestHourlyCostOf_DividesTheApplicationsPodsByTheMeasuredPeriod(t *testing.T) {
	t.Parallel()

	pods := &PodAllocations{
		start: hour, end: hour.Add(time.Hour),
		resp: AllocationResponse{Data: []map[string]AllocationData{{
			"redis-1":             hourAlloc("redis-1", "deployment", "redis", 60),
			"test-cronjob-2912-x": hourAlloc("test-cronjob-2912-x", "job", "test-cronjob-29123456", 15),
			"argocd-server-1":     hourAlloc("argocd-server-1", "deployment", "argocd-server", 60),
			"__idle__":            {CPUCost: 50, Start: hour, End: hour.Add(time.Hour)},
		}}},
	}

	got := pods.HourlyCostOf(redisResources)

	// Two pods of the application, over the hour the namespace was measured; argocd-server and idle are
	// not the application's, and shared and external costs are not part of a pod's cost.
	assert.InDelta(t, 0.8, got.CPU, 1e-9)
	assert.InDelta(t, 0.4, got.Memory, 1e-9)
	assert.InDelta(t, 0.2, got.GPU, 1e-9)
	assert.InDelta(t, 0.1, got.Storage, 1e-9)
	assert.InDelta(t, 0.06, got.Network, 1e-9)
	assert.InDelta(t, 0.04, got.LoadBalancer, 1e-9)
	assert.InDelta(t, 0.4, got.CPUIdle, 1e-9, "half of each pod's CPU request went unused")
	assert.InDelta(t, 0.2, got.MemoryIdle, 1e-9)
}

func TestHourlyCostOf_ANamespaceMeasuredForPartOfTheWindow(t *testing.T) {
	t.Parallel()

	// OpenCost has only the last 30 minutes of a new namespace: its cost is per half hour.
	pods := &PodAllocations{
		start: hour, end: hour.Add(time.Hour),
		resp: AllocationResponse{Data: []map[string]AllocationData{{"redis-1": hourAlloc("redis-1", "deployment", "redis", 30)}}},
	}

	assert.InDelta(t, 0.8, pods.HourlyCostOf(redisResources).CPU, 1e-9)
}

func TestHourlyCostOf_NothingMeasured(t *testing.T) {
	t.Parallel()

	pods := &PodAllocations{start: hour, end: hour.Add(time.Hour), resp: AllocationResponse{}}

	assert.Equal(t, HourlyCost{}, pods.HourlyCostOf(redisResources))
	assert.Equal(t, HourlyCost{}, (&PodAllocations{}).HourlyCostOf(redisResources), "an empty window")
}

func TestGetRecentPodAllocations_AsksForTheWindowUpToNow(t *testing.T) {
	t.Parallel()

	var window, filter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		window, filter = r.URL.Query().Get("window"), r.URL.Query().Get("filter")
		_ = json.NewEncoder(w).Encode(AllocationResponse{Code: 200, Data: []map[string]AllocationData{{
			"redis-1": hourAlloc("redis-1", "deployment", "redis", 60),
		}}})
	}))
	t.Cleanup(srv.Close)
	before := time.Now()

	pods, err := NewClient(srv.URL, time.Second, nil).GetRecentPodAllocations(context.Background(), "argocd", time.Hour)

	require.NoError(t, err)
	assert.Equal(t, `namespace:"argocd"`, filter)
	start, end, ok := strings.Cut(window, ",")
	require.True(t, ok, window)
	startTime, err := time.Parse(time.RFC3339, start)
	require.NoError(t, err)
	endTime, err := time.Parse(time.RFC3339, end)
	require.NoError(t, err)
	assert.Equal(t, time.Hour, endTime.Sub(startTime))
	assert.WithinDuration(t, before, endTime, 2*time.Second)
	assert.InDelta(t, 0.4, pods.HourlyCostOf(redisResources).CPU, 1e-9)
}

func TestWithTransport_SendsRequestsThroughIt(t *testing.T) {
	t.Parallel()

	var sent []string
	transport := roundTripper(func(r *http.Request) (*http.Response, error) {
		sent = append(sent, r.URL.Path)
		return http.DefaultTransport.RoundTrip(r)
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AllocationResponse{Code: 200})
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL, time.Second, nil, WithTransport(transport)).GetRecentPodAllocations(context.Background(), "argocd", time.Hour)

	require.NoError(t, err)
	assert.Equal(t, []string{"/allocation"}, sent)
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
