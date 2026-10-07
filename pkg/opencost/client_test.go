package opencost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// ---- parseSummaryResponse ----

func TestParseSummaryResponse_EmptyData(t *testing.T) {
	t.Parallel()

	got := parseSummaryResponse(SummaryAllocationResponse{})
	assert.Nil(t, got)
}

func summaryResp(allocs map[string]SummaryAllocationData) SummaryAllocationResponse {
	return SummaryAllocationResponse{
		Data: SummaryAllocationResponseData{
			Sets: []SummaryAllocationSet{{Allocations: allocs}},
		},
	}
}

func TestParseSummaryResponse_SingleWindow(t *testing.T) {
	t.Parallel()

	resp := summaryResp(map[string]SummaryAllocationData{
		"my-namespace": {
			CPUCoreRequestAverage: 1.0,
			CPUCoreUsageAverage:   0.4,
			CPUCoreLimitAverage:   2.0,
			RAMByteRequestAverage: 512 * 1024 * 1024,
			RAMByteUsageAverage:   358 * 1024 * 1024,
			RAMByteLimitAverage:   1024 * 1024 * 1024,
			CPUCostIdle:           10.0,
			RAMCostIdle:           5.0,
			Efficiency:            0.55,
		},
	})
	got := parseSummaryResponse(resp)
	require.NotNil(t, got)

	assert.InDelta(t, 0.4, got.CPUEfficiency, 1e-9)
	assert.InDelta(t, float64(358*1024*1024)/float64(512*1024*1024), got.RAMEfficiency, 1e-9)
	assert.InDelta(t, 0.55, got.TotalEfficiency, 1e-9)
	assert.InDelta(t, 1.0, got.CPUCoresRequested, 1e-9)
	assert.InDelta(t, 0.4, got.CPUCoresUsed, 1e-9)
	assert.InDelta(t, float64(512*1024*1024), got.RAMBytesRequested, 1e-9)
	assert.InDelta(t, float64(358*1024*1024), got.RAMBytesUsed, 1e-9)
	assert.InDelta(t, 300.0, got.CPUIdleCost, 1e-9) // 10 * 30
	assert.InDelta(t, 150.0, got.RAMIdleCost, 1e-9) // 5 * 30
	assert.InDelta(t, 2.0, got.CPUCoresLimit, 1e-9)
	assert.InDelta(t, float64(1024*1024*1024), got.RAMBytesLimit, 1e-9)
}

func TestParseSummaryResponse_ZeroRequests_NoDivisionByZero(t *testing.T) {
	t.Parallel()

	// Non-zero efficiency keeps the record from being nil, but zero request averages
	// must not cause division by zero.
	resp := summaryResp(map[string]SummaryAllocationData{
		"my-namespace": {Efficiency: 0.55},
	})
	got := parseSummaryResponse(resp)
	require.NotNil(t, got)

	assert.InDelta(t, 0.0, got.CPUEfficiency, 1e-9)
	assert.InDelta(t, 0.0, got.RAMEfficiency, 1e-9)
}

func TestParseSummaryResponse_MultipleNamespaces_Aggregated(t *testing.T) {
	t.Parallel()

	resp := summaryResp(map[string]SummaryAllocationData{
		"ns-1": {CPUCoreRequestAverage: 1.0, CPUCoreUsageAverage: 0.5, Efficiency: 0.5},
		"ns-2": {CPUCoreRequestAverage: 1.0, CPUCoreUsageAverage: 0.3, Efficiency: 0.7},
	})
	got := parseSummaryResponse(resp)
	require.NotNil(t, got)

	// cpuEff = (0.5+0.3) / (1.0+1.0) = 0.4
	assert.InDelta(t, 0.4, got.CPUEfficiency, 1e-9)
	// TotalEfficiency = (0.5+0.7)/2 = 0.6
	assert.InDelta(t, 0.6, got.TotalEfficiency, 1e-9)
}

func TestParseSummaryResponse_LimitFields_Forwarded(t *testing.T) {
	t.Parallel()

	resp := summaryResp(map[string]SummaryAllocationData{
		"my-namespace": {
			CPUCoreRequestAverage: 1.0,
			CPUCoreUsageAverage:   0.4,
			CPUCoreLimitAverage:   4.0,
			RAMByteRequestAverage: 512 * 1024 * 1024,
			RAMByteUsageAverage:   256 * 1024 * 1024,
			RAMByteLimitAverage:   2 * 1024 * 1024 * 1024,
			Efficiency:            0.5,
		},
	})
	got := parseSummaryResponse(resp)
	require.NotNil(t, got)

	assert.InDelta(t, 4.0, got.CPUCoresLimit, 1e-9, "CPUCoresLimit should be forwarded from cpuCoreLimitAverage")
	assert.InDelta(t, float64(2*1024*1024*1024), got.RAMBytesLimit, 1e-9, "RAMBytesLimit should be forwarded from ramByteLimitAverage")
}

// ---- parseOpenCostResponse ----

func TestParseOpenCostResponse_EmptyData(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	got := parseOpenCostResponse(AllocationResponse{Data: nil}, start, end)
	require.NotNil(t, got)

	assert.InDelta(t, 0.0, got.AllocationTotals.TotalCost, 1e-9)
	assert.Len(t, got.Window, 2)
}

func TestParseOpenCostResponse_StoresWindowInUTC(t *testing.T) {
	t.Parallel()

	athens := time.FixedZone("EEST", 3*60*60)
	end := time.Date(2026, 10, 1, 18, 43, 58, 0, athens)
	got := parseOpenCostResponse(AllocationResponse{}, end.Add(-24*time.Hour), end)

	assert.Equal(t, []string{"2026-09-30T15:43:58Z", "2026-10-01T15:43:58Z"}, got.Window)
}

func TestParseOpenCostResponse_AppliesMonthlyMultiplier(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"my-ns": {
					CPUCost: 10.0, RAMCost: 5.0, PVCost: 1.0, GPUCost: 0.5,
					RawAllocationOnly: &RawAllocationOnly{CPUCoreUsageMax: 0.8, RAMByteUsageMax: 500},
				},
			},
		},
	}
	got := parseOpenCostResponse(resp, start, end)

	assert.InDelta(t, 300.0, got.AllocationTotals.CPUCost, 1e-9)    // 10 * 30
	assert.InDelta(t, 150.0, got.AllocationTotals.MemoryCost, 1e-9) // 5 * 30
	assert.InDelta(t, 30.0, got.AllocationTotals.StorageCost, 1e-9) // 1 * 30
	assert.InDelta(t, 15.0, got.AllocationTotals.GPUCost, 1e-9)     // 0.5 * 30
	assert.InDelta(t, 495.0, got.AllocationTotals.TotalCost, 1e-9)  // 16.5 * 30
	// Peak is instantaneous — no monthly multiplier
	assert.InDelta(t, 0.8, got.PeakCPUCores, 1e-9)
	assert.InDelta(t, 500.0, got.PeakRAMBytes, 1e-9)
}

func TestParseOpenCostResponse_PeakUsage_ExtractedFromRawAllocation(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"ns-a": {
					CPUCost: 5.0, RAMCost: 2.0,
					RawAllocationOnly: &RawAllocationOnly{CPUCoreUsageMax: 1.5, RAMByteUsageMax: 300 * 1024 * 1024},
				},
				"ns-b": {
					CPUCost: 3.0, RAMCost: 1.0,
					RawAllocationOnly: &RawAllocationOnly{CPUCoreUsageMax: 0.5, RAMByteUsageMax: 100 * 1024 * 1024},
				},
			},
		},
	}
	got := parseOpenCostResponse(resp, start, end)

	// Peak values are summed across allocations
	assert.InDelta(t, 2.0, got.PeakCPUCores, 1e-9, "peak CPU should sum across allocations")
	assert.InDelta(t, float64(400*1024*1024), got.PeakRAMBytes, 1e-9, "peak RAM should sum across allocations")
}

func TestParseOpenCostResponse_NilRawAllocation_PeakIsZero(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"my-ns": {CPUCost: 10.0, RAMCost: 5.0},
			},
		},
	}
	got := parseOpenCostResponse(resp, start, end)

	assert.InDelta(t, 0.0, got.PeakCPUCores, 1e-9, "PeakCPUCores should be zero when rawAllocationOnly is absent")
	assert.InDelta(t, 0.0, got.PeakRAMBytes, 1e-9, "PeakRAMBytes should be zero when rawAllocationOnly is absent")
}

func TestParseOpenCostResponse_MultipleAllocations(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"pod-a": {CPUCost: 5.0, RAMCost: 2.0},
				"pod-b": {CPUCost: 3.0, RAMCost: 1.0},
			},
		},
	}
	got := parseOpenCostResponse(resp, start, end)

	assert.InDelta(t, 240.0, got.AllocationTotals.CPUCost, 1e-9)   // (5+3) * 30
	assert.InDelta(t, 90.0, got.AllocationTotals.MemoryCost, 1e-9) // (2+1) * 30
}

func TestParseOpenCostResponse_NetworkSubcosts_Extracted(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"my-ns": {
					NetworkCost:                  1.0,
					NetworkCrossZoneCost:         0.4,
					NetworkCrossRegionCost:       0.3,
					NetworkInternetCost:          0.2,
					NetworkNatGatewayEgressCost:  0.07,
					NetworkNatGatewayIngressCost: 0.03,
				},
			},
		},
	}
	got := parseOpenCostResponse(resp, start, end)

	assert.InDelta(t, 30.0, got.AllocationTotals.NetworkCost, 1e-9)           // 1.0 * 30
	assert.InDelta(t, 12.0, got.AllocationTotals.NetworkCrossZoneCost, 1e-9)  // 0.4 * 30
	assert.InDelta(t, 9.0, got.AllocationTotals.NetworkCrossRegionCost, 1e-9) // 0.3 * 30
	assert.InDelta(t, 6.0, got.AllocationTotals.NetworkInternetCost, 1e-9)    // 0.2 * 30
	assert.InDelta(t, 3.0, got.AllocationTotals.NetworkNatGatewayCost, 1e-9)  // (0.07+0.03) * 30
}

// ---- GetNamespaceCost ----

func TestGetNamespaceCost_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/allocation", r.URL.Path)
		assert.Equal(t, "namespace", r.URL.Query().Get("aggregate"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AllocationResponse{
			Code: 200,
			Data: []map[string]AllocationData{
				{
					"test-ns": {CPUCost: 10.0, RAMCost: 5.0},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	got, err := client.GetNamespaceCost(context.Background(), "test-ns")
	require.NoError(t, err)

	assert.InDelta(t, 300.0, got.AllocationTotals.CPUCost, 1e-9)
	assert.InDelta(t, 450.0, got.AllocationTotals.TotalCost, 1e-9)
}

func TestGetNamespaceCost_Non200Response(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	_, err := client.GetNamespaceCost(context.Background(), "missing-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestGetNamespaceCost_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-valid-json"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	_, err := client.GetNamespaceCost(context.Background(), "test-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

// ---- GetNamespaceSummary ----

func TestGetNamespaceSummary_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/allocation/summary", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(summaryResp(map[string]SummaryAllocationData{
			"test-ns": {
				CPUCoreRequestAverage: 1.0,
				CPUCoreUsageAverage:   0.4,
				RAMByteRequestAverage: 512 * 1024 * 1024,
				RAMByteUsageAverage:   358 * 1024 * 1024,
				Efficiency:            0.55,
			},
		}))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	got, err := client.GetNamespaceSummary(context.Background(), "test-ns")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.IsType(t, &models.EfficiencyMetrics{}, got)
	assert.InDelta(t, 0.4, got.CPUEfficiency, 1e-9)
	assert.InDelta(t, 0.55, got.TotalEfficiency, 1e-9)
}

func TestGetNamespaceSummary_Non200Response(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	_, err := client.GetNamespaceSummary(context.Background(), "test-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestGetNamespaceSummary_EmptyResponse_ReturnsNil(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SummaryAllocationResponse{Code: 200})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	got, err := client.GetNamespaceSummary(context.Background(), "test-ns")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ---- parsePodBreakdown ----

func TestParsePodBreakdown_Empty(t *testing.T) {
	t.Parallel()

	got := parsePodBreakdown(AllocationResponse{Data: nil})
	assert.Empty(t, got)
}

func TestParsePodBreakdown_ExtractsPropertiesAndAppliesMultiplier(t *testing.T) {
	t.Parallel()

	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"redis-0": {
					CPUCost:   5.0,
					RAMCost:   2.0,
					TotalCost: 7.0,
					Properties: AllocationProperties{
						Pod:            "redis-0",
						Namespace:      "argocd",
						ControllerKind: "StatefulSet",
					},
				},
				"nginx-abc": {
					CPUCost:   3.0,
					RAMCost:   1.0,
					TotalCost: 4.0,
					Properties: AllocationProperties{
						Pod:            "nginx-abc",
						Namespace:      "argocd",
						ControllerKind: "Deployment",
					},
				},
			},
		},
	}
	got := parsePodBreakdown(resp)
	require.Len(t, got, 2)

	byName := make(map[string]models.PodCost)
	for _, p := range got {
		byName[p.Name] = p
	}

	redis := byName["redis-0"]
	assert.Equal(t, "StatefulSet", redis.ControllerKind)
	assert.Equal(t, "argocd", redis.Namespace)
	assert.InDelta(t, 150.0, redis.CPUCost, 1e-9)   // 5 * 30
	assert.InDelta(t, 60.0, redis.RAMCost, 1e-9)    // 2 * 30
	assert.InDelta(t, 210.0, redis.TotalCost, 1e-9) // 7 * 30
}

func TestParsePodBreakdown_FallsBackToMapKey(t *testing.T) {
	t.Parallel()

	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{
				"some-pod": {CPUCost: 1.0, RAMCost: 1.0, TotalCost: 2.0},
			},
		},
	}
	got := parsePodBreakdown(resp)
	require.Len(t, got, 1)
	assert.Equal(t, "some-pod", got[0].Name)
}

func TestParsePodBreakdown_AggregatesAcrossWindows(t *testing.T) {
	t.Parallel()

	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{"pod-a": {CPUCost: 2.0, RAMCost: 1.0, TotalCost: 3.0}},
			{"pod-a": {CPUCost: 3.0, RAMCost: 2.0, TotalCost: 5.0}},
		},
	}
	got := parsePodBreakdown(resp)
	require.Len(t, got, 1)
	assert.InDelta(t, 150.0, got[0].CPUCost, 1e-9)   // (2+3) * 30
	assert.InDelta(t, 240.0, got[0].TotalCost, 1e-9) // (3+5) * 30
}

// ---- GetPodAllocations ----

func TestGetPodAllocations_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/allocation", r.URL.Path)
		assert.Equal(t, "pod", r.URL.Query().Get("aggregate"))
		assert.Equal(t, `namespace:"test-ns"`, r.URL.Query().Get("filter"))
		assert.Equal(t, "true", r.URL.Query().Get("includeRawAllocation"), "peaks are needed per pod")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AllocationResponse{
			Code: 200,
			Data: []map[string]AllocationData{
				{
					"redis-0": {
						CPUCost: 5.0, RAMCost: 2.0, TotalCost: 7.0,
						Properties: AllocationProperties{Pod: "redis-0", ControllerKind: "StatefulSet"},
					},
					"redis-1": {
						CPUCost: 5.0, RAMCost: 2.0, TotalCost: 7.0,
						Properties: AllocationProperties{Pod: "redis-1", ControllerKind: "StatefulSet"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	allocations, err := client.GetPodAllocations(context.Background(), "test-ns")
	require.NoError(t, err)
	got := allocations.Pods()
	assert.Len(t, got, 2)

	var totalCost float64
	for _, p := range got {
		totalCost += p.TotalCost
	}
	assert.InDelta(t, 420.0, totalCost, 1e-9) // (7+7) * 30
}

func TestGetPodAllocations_Non200Response(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	_, err := client.GetPodAllocations(context.Background(), "test-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// ---- monthly projection from the measured period ----

func TestMonthlyFactor(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		start, end time.Time
		want       float64
	}{
		{name: "full day", start: end.Add(-24 * time.Hour), end: end, want: 30},
		{name: "six hours", start: end.Add(-6 * time.Hour), end: end, want: 120},
		{name: "thirty minutes", start: end.Add(-30 * time.Minute), end: end, want: 1440},
		{name: "no start reported", end: end, want: 30},
		{name: "no end reported", start: end, want: 30},
		{name: "empty period", start: end, end: end, want: 30},
		{name: "end before start", start: end, end: end.Add(-time.Hour), want: 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tt.want, monthlyFactor(tt.start, tt.end), 1e-9)
		})
	}
}

func TestParseOpenCostResponse_NewNamespace_ScalesByMeasuredPeriod(t *testing.T) {
	t.Parallel()

	// The namespace has existed for 6 of the 24 queried hours: $1 over 6h is $120 a month,
	// not the $30 a fixed ×30 would give.
	queryEnd := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	queryStart := queryEnd.Add(-24 * time.Hour)
	resp := AllocationResponse{
		Data: []map[string]AllocationData{
			{"new-ns": {Start: queryEnd.Add(-6 * time.Hour), End: queryEnd, CPUCost: 0.6, RAMCost: 0.4, TotalCost: 1.0}},
		},
	}
	got := parseOpenCostResponse(resp, queryStart, queryEnd)

	assert.InDelta(t, 72.0, got.AllocationTotals.CPUCost, 1e-9)    // 0.6 * 120
	assert.InDelta(t, 48.0, got.AllocationTotals.MemoryCost, 1e-9) // 0.4 * 120
	assert.InDelta(t, 120.0, got.AllocationTotals.TotalCost, 1e-9)
	assert.InDelta(t, 6.0, got.MeasuredHours, 1e-9)
}

func TestParseOpenCostResponse_IgnoresVirtualEntriesForPeriod(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	resp := AllocationResponse{
		Data: []map[string]AllocationData{{
			"new-ns":   {Start: end.Add(-6 * time.Hour), End: end, CPUCost: 1.0},
			"__idle__": {Start: end.Add(-24 * time.Hour), End: end},
		}},
	}
	got := parseOpenCostResponse(resp, end.Add(-24*time.Hour), end)

	assert.InDelta(t, 6.0, got.MeasuredHours, 1e-9)
	assert.InDelta(t, 120.0, got.AllocationTotals.CPUCost, 1e-9)
}

func TestParsePodBreakdown_ReplacedPod_SharesNamespacePeriod(t *testing.T) {
	t.Parallel()

	// The namespace is 12h old, and a sync replaced old-pod with new-pod 6h ago. Scaling each
	// pod by its own 6h would project both onto a full month and double the namespace; sharing
	// the namespace's 12h keeps the pods adding up to what the namespace costs.
	end := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	replacedAt := end.Add(-6 * time.Hour)
	resp := AllocationResponse{
		Data: []map[string]AllocationData{{
			"old-pod": {Start: end.Add(-12 * time.Hour), End: replacedAt, CPUCost: 0.5, TotalCost: 0.5},
			"new-pod": {Start: replacedAt, End: end, CPUCost: 0.5, TotalCost: 0.5},
		}},
	}
	got := parsePodBreakdown(resp)
	require.Len(t, got, 2)

	var total float64
	for _, p := range got {
		total += p.TotalCost
	}
	assert.InDelta(t, 60.0, total, 1e-9) // $1 over 12h
}

func TestParseSummaryResponse_IdleCostScalesByMeasuredPeriod(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	resp := summaryResp(map[string]SummaryAllocationData{
		"new-ns": {Start: end.Add(-6 * time.Hour), End: end, CPUCostIdle: 1.0, RAMCostIdle: 0.5, Efficiency: 0.5},
	})
	got := parseSummaryResponse(resp)
	require.NotNil(t, got)

	assert.InDelta(t, 120.0, got.CPUIdleCost, 1e-9) // 1.0 * 120
	assert.InDelta(t, 60.0, got.RAMIdleCost, 1e-9)  // 0.5 * 120
}

func TestParseChargebackEntries_ScalesByMeasuredPeriod(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	alloc := AllocationResponse{
		Data: []map[string]AllocationData{{
			"team-a": {Start: end.Add(-12 * time.Hour), End: end, TotalCost: 1.0},
		}},
	}
	got := parseChargebackEntries(alloc, SummaryAllocationResponse{})
	require.Len(t, got, 1)
	assert.InDelta(t, 60.0, got[0].TotalCost, 1e-9) // 1.0 * 720/12
}

func TestGetNamespaceCost_DecodesMeasuredPeriodFromWire(t *testing.T) {
	t.Parallel()

	// OpenCost's own wire format: start and end are RFC 3339 strings, with fractional
	// seconds on the end of the window.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":[{"new-ns":{` +
			`"start":"2026-10-01T06:00:00Z","end":"2026-10-01T12:00:00.000000000Z",` +
			`"cpuCost":0.5,"ramCost":0.5,"totalCost":1.0}}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	got, err := client.GetNamespaceCost(context.Background(), "new-ns")
	require.NoError(t, err)

	assert.InDelta(t, 6.0, got.MeasuredHours, 1e-9)
	assert.InDelta(t, 120.0, got.AllocationTotals.TotalCost, 1e-9)
}

// ---- GetLabelKeys ----

func TestGetLabelKeys_QueriesAutocompleteForTheNamespace(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/allocation/autocomplete", r.URL.Path)
		assert.Equal(t, "label", r.URL.Query().Get("field"))
		assert.Equal(t, `namespace:"payments"`, r.URL.Query().Get("filter"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"data":` +
			`["team","app_kubernetes_io_name","kubernetes_io_hostname","pod_template_hash"]}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	got, err := client.GetLabelKeys(context.Background(), "payments")
	require.NoError(t, err)
	assert.Equal(t, []string{"app_kubernetes_io_name", "team"}, got)
}

func TestGetLabelKeys_ClusterWideSendsNoFilter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasFilter := r.URL.Query()["filter"]
		assert.False(t, hasFilter)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"data":[]}}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	got, err := client.GetLabelKeys(context.Background(), "")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestGetLabelKeys_Non200Response(t *testing.T) {
	t.Parallel()

	// OpenCost older than 1.120.3 has no autocomplete route.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "404 page not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	_, err := client.GetLabelKeys(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestChargebackLabelKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		keys []string
		want []string
	}{
		{
			name: "keeps labels teams set, sorted",
			keys: []string{"team", "app", "cost_center", "app_kubernetes_io_part_of", "helm_sh_chart"},
			want: []string{"app", "app_kubernetes_io_part_of", "cost_center", "helm_sh_chart", "team"},
		},
		{
			name: "drops node and namespace labels",
			keys: []string{
				"kubernetes_io_hostname", "kubernetes_io_metadata_name", "beta_kubernetes_io_arch",
				"node_kubernetes_io_instance_type", "node_role_kubernetes_io_control_plane",
				"topology_kubernetes_io_zone", "eks_amazonaws_com_nodegroup", "karpenter_sh_capacity_type",
				"team",
			},
			want: []string{"team"},
		},
		{
			name: "drops controller bookkeeping",
			keys: []string{
				"controller_revision_hash", "pod_template_hash", "pod_template_generation", "controller_uid",
				"job_name", "batch_kubernetes_io_job_name", "apps_kubernetes_io_pod_index",
				"statefulset_kubernetes_io_pod_name", "providerID", "app",
			},
			want: []string{"app"},
		},
		{
			name: "removes duplicates and empty keys",
			keys: []string{"team", "", "team"},
			want: []string{"team"},
		},
		{
			name: "nothing left is an empty list, not nil",
			keys: []string{"pod_template_hash"},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, chargebackLabelKeys(tt.keys))
		})
	}
}

// ---- window parameter ----

// OpenCost rejects a window carrying a UTC offset (2026-10-01T18:43:58+03:00) as an "illegal
// window", so every request must write both ends in UTC whatever the backend's time zone. The
// test sets time.Local, so it must not run in parallel with tests that read the local zone.
func TestRequests_WriteWindowInUTC(t *testing.T) {
	athens := time.FixedZone("EEST", 3*60*60)
	saved := time.Local
	time.Local = athens
	t.Cleanup(func() { time.Local = saved })

	var mu sync.Mutex
	var windows []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		windows = append(windows, r.URL.Query().Get("window"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation/summary":
			_, _ = w.Write([]byte(`{"code":200,"data":{"sets":[]}}`))
		case "/allocation/autocomplete":
			_, _ = w.Write([]byte(`{"code":200,"data":{"data":[]}}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"data":[]}`))
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, nil)
	calls := map[string]struct {
		call     func() error
		requests int
	}{
		"GetNamespaceCost": {func() error { _, err := client.GetNamespaceCost(t.Context(), "ns"); return err }, 1},
		"GetNamespaceSummary": {
			func() error { _, err := client.GetNamespaceSummary(t.Context(), "ns"); return err }, 1,
		},
		"GetPodAllocations": {func() error { _, err := client.GetPodAllocations(t.Context(), "ns"); return err }, 1},
		"GetOrphanedPods":   {func() error { _, err := client.GetOrphanedPods(t.Context(), "ns"); return err }, 1},
		"GetChargeback": {
			func() error { _, err := client.GetChargeback(t.Context(), "team", "ns"); return err }, 2,
		},
		"GetLabelKeys": {func() error { _, err := client.GetLabelKeys(t.Context(), "ns"); return err }, 1},
	}

	for name, tc := range calls {
		t.Run(name, func(t *testing.T) {
			mu.Lock()
			windows = nil
			mu.Unlock()

			require.NoError(t, tc.call())

			mu.Lock()
			defer mu.Unlock()
			require.Len(t, windows, tc.requests)
			for _, window := range windows {
				start, end, ok := strings.Cut(window, ",")
				require.True(t, ok, window)
				assert.True(t, strings.HasSuffix(start, "Z"), "window start not in UTC: %s", window)
				assert.True(t, strings.HasSuffix(end, "Z"), "window end not in UTC: %s", window)
			}
		})
	}
}

// ---- dayWindow ----

func TestDayWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 3, 17, 42, 31, 0, time.UTC)
	date := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		require.NoError(t, err)
		return d
	}
	for _, tt := range []struct {
		name     string
		from, to time.Time
		want     string
	}{
		{"a past range ends after its last day", date("2026-09-01"), date("2026-09-30"), "2026-09-01T00:00:00Z,2026-10-01T00:00:00Z"},
		{"a range reaching today ends now", date("2026-10-01"), date("2026-10-31"), "2026-10-01T00:00:00Z,2026-10-03T17:42:00Z"},
		{"only the dates count", date("2026-09-01").Add(13 * time.Hour), date("2026-09-02").Add(5 * time.Hour), "2026-09-01T00:00:00Z,2026-09-03T00:00:00Z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, dayWindow(tt.from, tt.to, now))
		})
	}

	// Just after midnight a range starting today still covers a moment, not nothing.
	justAfterMidnight := time.Date(2026, 10, 3, 0, 0, 20, 0, time.UTC)
	assert.Equal(t, "2026-10-03T00:00:00Z,2026-10-03T00:01:00Z", dayWindow(date("2026-10-03"), date("2026-10-03"), justAfterMidnight))
}

// ---- GetContainerAllocations ----

func TestGetContainerAllocations_OneEntryPerContainerOfEachPod(t *testing.T) {
	t.Parallel()

	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewEncoder(w).Encode(AllocationResponse{Code: 200, Data: []map[string]AllocationData{{
			"web-1/web": {
				Minutes: 1440, CPUCoreRequestAverage: 0.5, CPUCoreUsageAverage: 0.05, CPUCoreHours: 12, CPUCost: 0.38,
				RAMByteRequestAverage: 1 << 30, RAMByteUsageAverage: 300 << 20, RAMByteLimitAverage: 512 << 20, RAMByteHours: 24 << 30, RAMCost: 0.1,
				Properties: AllocationProperties{Pod: "web-1", Container: "web", ControllerKind: "deployment", Controller: "web"},
			},
			"web-1/istio-proxy": {Minutes: 720, Properties: AllocationProperties{Pod: "web-1", Container: "istio-proxy", ControllerKind: "deployment", Controller: "web"}},
			"__idle__":          {CPUCost: 5},
			"lonely":            {Properties: AllocationProperties{Pod: "lonely"}},
		}}})
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, 5*time.Second, nil).GetContainerAllocations(context.Background(), "shop")

	require.NoError(t, err)
	assert.Equal(t, "pod,container", query.Get("aggregate"))
	assert.Equal(t, `namespace:"shop"`, query.Get("filter"))
	require.Len(t, got, 2, "virtual entries and allocations without a container are left out")
	assert.Equal(t, "istio-proxy", got[0].Container)
	assert.InDelta(t, 12, got[0].Hours, 1e-9)
	web := got[1]
	assert.Equal(t, models.ContainerAllocation{
		Pod: "web-1", Container: "web", ControllerKind: "deployment", Controller: "web", Hours: 24,
		CPURequestCores: 0.5, CPUUsageCores: 0.05, RAMRequestBytes: 1 << 30, RAMUsageBytes: 300 << 20, RAMLimitBytes: 512 << 20,
		CPUCoreHours: 12, RAMByteHours: 24 << 30, CPUCost: 0.38, RAMCost: 0.1,
	}, web)
}
