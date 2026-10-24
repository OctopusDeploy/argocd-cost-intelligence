package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/storage"
)

// mockStorage implements storage.CostStorage for testing.
type mockStorage struct {
	saveActualFn              func(ctx context.Context, actual *models.CostActual) error
	getLatestActualFn         func(ctx context.Context, app models.AppRef) (*models.CostActual, error)
	getActualsInDateRangeFn   func(ctx context.Context, app models.AppRef, start, end time.Time) ([]models.CostActual, error)
	getNamespaceCostSummaryFn func(ctx context.Context, namespace string, days int) (float64, error)
	// superseded makes SaveActualUnlessSuperseded find a later sync's record.
	superseded atomic.Bool
}

var _ storage.CostStorage = (*mockStorage)(nil)

func (m *mockStorage) InitializeSchema(_ context.Context) error { return nil }
func (m *mockStorage) Close() error                             { return nil }

func (m *mockStorage) SaveActual(ctx context.Context, actual *models.CostActual) error {
	if m.saveActualFn != nil {
		return m.saveActualFn(ctx, actual)
	}
	return nil
}

func (m *mockStorage) SaveActualUnlessSuperseded(ctx context.Context, actual *models.CostActual) (bool, error) {
	if m.superseded.Load() {
		return false, nil
	}
	return true, m.SaveActual(ctx, actual)
}

func (m *mockStorage) GetLatestActual(ctx context.Context, app models.AppRef) (*models.CostActual, error) {
	if m.getLatestActualFn != nil {
		return m.getLatestActualFn(ctx, app)
	}
	return nil, nil
}

func (m *mockStorage) GetActualsInDateRange(ctx context.Context, app models.AppRef, start, end time.Time) ([]models.CostActual, error) {
	if m.getActualsInDateRangeFn != nil {
		return m.getActualsInDateRangeFn(ctx, app, start, end)
	}
	return nil, nil
}

func (m *mockStorage) GetNamespaceCostSummary(ctx context.Context, namespace string, days int) (float64, error) {
	if m.getNamespaceCostSummaryFn != nil {
		return m.getNamespaceCostSummaryFn(ctx, namespace, days)
	}
	return 0, nil
}

func (m *mockStorage) ListNamespaces(_ context.Context) ([]models.NamespaceSummary, error) {
	return nil, nil
}

func (m *mockStorage) GetLatestActualsForNamespace(_ context.Context, _ string) ([]models.CostActual, error) {
	return nil, nil
}

func (m *mockStorage) GetRecentActuals(_ context.Context, _ models.AppRef, _ int) ([]models.CostActual, error) {
	return nil, nil
}

func (m *mockStorage) SaveCloudEstimate(_ context.Context, _ models.AppRef, _ *models.CloudServiceEstimate) error {
	return nil
}

func (m *mockStorage) GetCloudEstimate(_ context.Context, _ models.AppRef) (*models.CloudServiceEstimate, error) {
	return nil, nil
}

func (m *mockStorage) ListCloudEstimates(context.Context) ([]models.AppCloudEstimate, error) {
	return nil, nil
}

func (m *mockStorage) ListLatestActuals(context.Context) ([]models.CostActual, error) {
	return nil, nil
}

func (m *mockStorage) SaveDailyCosts(context.Context, models.AppRef, string, string, []models.DailyCost) error {
	return nil
}

func (m *mockStorage) GetDailyCosts(context.Context, models.AppRef, string, string, string) ([]models.DailyCost, error) {
	return nil, nil
}

func (m *mockStorage) GetDailyProgress(context.Context, models.AppRef) (*models.DailyProgress, error) {
	return nil, nil
}

func (m *mockStorage) SetDailyProgress(context.Context, models.AppRef, models.DailyProgress) error {
	return nil
}

func (m *mockStorage) PruneDailyCosts(context.Context, string) error { return nil }

func (m *mockStorage) DeleteMeasurements(context.Context, models.AppRef) (bool, error) {
	return false, nil
}

func (m *mockStorage) SaveFirstActual(context.Context, *models.CostActual) (bool, error) {
	return true, nil
}

func (m *mockStorage) Lock(context.Context, string) (func(), error) { return func() {}, nil }

// newOpenCostTestServer returns an httptest.Server that serves valid OpenCost
// allocation and summary responses. Caller must call Close() when done.
func newOpenCostTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newOpenCostTestServerWithPodPeaks(t, true)
}

// newOpenCostTestServerWithPodPeaks is newOpenCostTestServer, with or without the pods' peak usage.
// The namespace's allocation has a peak of its own (0.8 cores, 400 MiB), as the mock OpenCost sends.
func newOpenCostTestServerWithPodPeaks(t *testing.T, podPeaks bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation":
			if r.URL.Query().Get("aggregate") == "pod" {
				redis := opencost.AllocationData{
					CPUCost: 6.0, RAMCost: 3.0, TotalCost: 9.0,
					CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.25,
					RAMByteRequestAverage: 100, RAMByteUsageAverage: 50,
					Properties: opencost.AllocationProperties{Pod: "redis-0", Namespace: "test-ns", ControllerKind: "statefulset", Controller: "redis"},
				}
				nginx := opencost.AllocationData{
					CPUCost: 4.0, RAMCost: 2.0, TotalCost: 6.0,
					Properties: opencost.AllocationProperties{Pod: "nginx-abc", Namespace: "test-ns", ControllerKind: "deployment", Controller: "nginx"},
				}
				if podPeaks {
					redis.RawAllocationOnly = &opencost.RawAllocationOnly{CPUCoreUsageMax: 0.5, RAMByteUsageMax: 300 * 1024 * 1024}
					nginx.RawAllocationOnly = &opencost.RawAllocationOnly{CPUCoreUsageMax: 0.2, RAMByteUsageMax: 200 * 1024 * 1024}
				}
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
					Code: 200,
					Data: []map[string]opencost.AllocationData{{"redis-0": redis, "nginx-abc": nginx}},
				})
			} else {
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
					Code: 200,
					Data: []map[string]opencost.AllocationData{
						{
							"test-ns": measuredDay(opencost.AllocationData{
								CPUCost: 10.0, RAMCost: 5.0,
								RawAllocationOnly: &opencost.RawAllocationOnly{CPUCoreUsageMax: 0.8, RAMByteUsageMax: 400 * 1024 * 1024},
							}),
						},
					},
				})
			}
		case "/allocation/summary":
			_ = json.NewEncoder(w).Encode(opencost.SummaryAllocationResponse{
				Code: 200,
				Data: opencost.SummaryAllocationResponseData{
					Sets: []opencost.SummaryAllocationSet{{
						Allocations: map[string]opencost.SummaryAllocationData{
							"test-ns": {
								CPUCoreRequestAverage: 1.0,
								CPUCoreUsageAverage:   0.4,
								RAMByteRequestAverage: 512 * 1024 * 1024,
								RAMByteUsageAverage:   358 * 1024 * 1024,
								CPUCost:               0.3,
								RAMCost:               0.1,
							},
						},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

// measuredDay is a namespace's allocation with a whole day of OpenCost data, which projects onto a
// month by 30.
func measuredDay(a opencost.AllocationData) opencost.AllocationData {
	a.End = time.Now()
	a.Start = a.End.Add(-24 * time.Hour)
	return a
}

func newTestHook(t *testing.T, srv *httptest.Server, store storage.CostStorage) *SyncHook {
	t.Helper()
	client := opencost.NewClient(srv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	return NewSyncHook(client, store, 1*time.Millisecond, logger)
}

// myApp is the Application most tests measure.
var myApp = models.AppRef{Namespace: "argocd", Name: "my-app"}

// ---- OnApplicationSync ----

func TestOnApplicationSync_EmptyApplicationName(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	hook := newTestHook(t, srv, &mockStorage{})
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName: "",
		Namespace:       "test-ns",
		SyncResult:      "Succeeded",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "application name is required")
}

func TestValidateNotification(t *testing.T) {
	t.Parallel()

	valid := models.SyncNotification{ApplicationName: "my-app", ApplicationNamespace: "team-a", Namespace: "web"}
	tests := map[string]struct {
		mutate  func(n *models.SyncNotification)
		wantErr string
	}{
		"valid":                         {mutate: func(*models.SyncNotification) {}},
		"no deployment namespace":       {mutate: func(n *models.SyncNotification) { n.Namespace = "" }},
		"no application name":           {mutate: func(n *models.SyncNotification) { n.ApplicationName = "" }, wantErr: "application name is required"},
		"no application namespace":      {mutate: func(n *models.SyncNotification) { n.ApplicationNamespace = "" }, wantErr: "application namespace is required"},
		"invalid application namespace": {mutate: func(n *models.SyncNotification) { n.ApplicationNamespace = "Team_A" }, wantErr: "invalid application namespace"},
		"invalid deployment namespace":  {mutate: func(n *models.SyncNotification) { n.Namespace = "a/b" }, wantErr: "invalid namespace"},
		"resources": {mutate: func(n *models.SyncNotification) {
			n.Resources = []models.ManagedResource{{Kind: "Deployment", Name: "web"}, {Kind: "Service", Name: "Not_A_Pod_Owner"}}
		}},
		"invalid pod-owning resource": {mutate: func(n *models.SyncNotification) {
			n.Resources = []models.ManagedResource{{Kind: "Deployment", Name: `web","kind":"x`}}
		}, wantErr: "invalid resources"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n := valid
			tt.mutate(&n)
			err := ValidateNotification(&n)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestOnApplicationSync_MissingApplicationNamespace_NotStored(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	saved := false
	store := &mockStorage{
		saveActualFn: func(_ context.Context, _ *models.CostActual) error {
			saved = true
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName: "my-app",
		Namespace:       "test-ns",
		SyncResult:      "Succeeded",
	})
	require.Error(t, err)
	assert.False(t, saved, "a record without its Application namespace could never be read back")
}

func TestOnApplicationSync_FailedSync_Skips(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	saved := false
	store := &mockStorage{
		saveActualFn: func(_ context.Context, _ *models.CostActual) error {
			saved = true
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Failed",
	})
	require.NoError(t, err)
	assert.False(t, saved, "SaveActual should not be called for a failed sync")
}

func TestOnApplicationSync_OtherCluster_Skips(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                string
		server, clusterName string
	}{
		{"by server", "https://prod.example.com", ""},
		{"by name", "", "prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var queried atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				queried.Store(true)
				http.NotFound(w, nil)
			}))
			defer srv.Close()
			saved := false
			store := &mockStorage{
				saveActualFn: func(_ context.Context, _ *models.CostActual) error {
					saved = true
					return nil
				},
			}

			err := newTestHook(t, srv, store).OnApplicationSync(context.Background(), &models.SyncNotification{
				ApplicationName:      "my-app",
				ApplicationNamespace: "argocd",
				Namespace:            "test-ns",
				SyncResult:           "Succeeded",
				DestinationServer:    tc.server,
				DestinationName:      tc.clusterName,
			})

			require.NoError(t, err)
			assert.False(t, queried.Load(), "OpenCost here does not measure another cluster's test-ns")
			assert.False(t, saved)
		})
	}
}

func TestDeploysHere(t *testing.T) {
	t.Parallel()

	assert.True(t, DeploysHere(&models.SyncNotification{}), "an older template sends no destination")
	assert.True(t, DeploysHere(&models.SyncNotification{DestinationServer: "https://kubernetes.default.svc"}))
	assert.True(t, DeploysHere(&models.SyncNotification{DestinationName: "in-cluster"}))
	assert.False(t, DeploysHere(&models.SyncNotification{DestinationServer: "https://prod.example.com"}))
	assert.False(t, DeploysHere(&models.SyncNotification{DestinationName: "prod"}))
}

func TestOnApplicationSync_Success_StoresCostAndEfficiency(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			saved = actual
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
		SyncTimestamp:        time.Now(),
	})
	require.NoError(t, err)
	require.NotNil(t, saved)

	assert.Equal(t, "my-app", saved.ApplicationName)
	assert.Equal(t, "argocd", saved.ApplicationNamespace)
	assert.Equal(t, models.MeasuredOnSync, saved.MeasuredOn)
	assert.False(t, saved.SyncTimestamp.IsZero())
	assert.InDelta(t, 450.0, saved.TotalMonthlyCost, 1e-9) // (10+5) * 30

	require.NotNil(t, saved.Efficiency, "efficiency should be populated")
	assert.InDelta(t, 0.4, saved.Efficiency.CPUEfficiency, 1e-9)
	assert.InDelta(t, (0.4*0.3+358.0/512*0.1)/0.4, saved.Efficiency.TotalEfficiency, 1e-9) // weighted by cost
	// OpenCost drops the peak from aggregated allocations, so the namespace's is its pods'.
	assert.InDelta(t, 0.7, saved.Efficiency.CPUCoresUsedMax, 1e-9, "peak CPU should be the pods'")
	assert.InDelta(t, float64(500*1024*1024), saved.Efficiency.RAMBytesUsedMax, 1e-9, "peak RAM should be the pods'")
	// The summary has no idle cost; the namespace's is its pods' unused requests.
	assert.InDelta(t, 135.0, saved.Efficiency.CPUIdleCost, 1e-9) // 6 * 0.75 * 30
	assert.InDelta(t, 45.0, saved.Efficiency.RAMIdleCost, 1e-9)  // 3 * 0.5 * 30
	require.NotNil(t, saved.IdleMonthlyCost)
	assert.InDelta(t, 180.0, *saved.IdleMonthlyCost, 1e-9)

	assert.Len(t, saved.Pods, 2, "pod breakdown should be populated")
	var totalPodCost float64
	for _, p := range saved.Pods {
		totalPodCost += p.TotalCost
	}
	assert.InDelta(t, 450.0, totalPodCost, 1e-9) // (9+6) * 30
	assert.Nil(t, saved.App, "without the application's resources only the namespace is measured")
}

func TestOnApplicationSync_PodsWithoutPeak_KeepNamespacePeak(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServerWithPodPeaks(t, false)
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			saved = actual
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	require.NoError(t, hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
		SyncTimestamp:        time.Now(),
	}))
	require.NotNil(t, saved)
	require.NotNil(t, saved.Efficiency)

	assert.InDelta(t, 0.8, saved.Efficiency.CPUCoresUsedMax, 1e-9)
	assert.InDelta(t, float64(400*1024*1024), saved.Efficiency.RAMBytesUsedMax, 1e-9)
}

func TestOnApplicationSync_WithResources_MeasuresTheApplicationsOwnPods(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			saved = actual
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "cache",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
		SyncTimestamp:        time.Now(),
		Resources: []models.ManagedResource{
			{Kind: "Service", Name: "redis", Namespace: "test-ns"},
			{Kind: "StatefulSet", Name: "redis", Namespace: "test-ns"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, saved)

	assert.InDelta(t, 450.0, saved.TotalMonthlyCost, 1e-9, "the namespace measurement is kept")
	assert.Len(t, saved.Pods, 2)
	require.NotNil(t, saved.App)
	assert.Equal(t, models.ManagedResources{{Kind: "StatefulSet", Name: "redis", Namespace: "test-ns"}}, saved.App.Controllers)
	require.Len(t, saved.App.Pods, 1)
	assert.Equal(t, "redis-0", saved.App.Pods[0].Name)
	assert.InDelta(t, 270.0, saved.App.TotalMonthlyCost, 1e-9) // 9 * 30
}

func TestOnApplicationSync_WithResources_PodFailure_MeasuresNamespaceOnly(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/allocation" && r.URL.Query().Get("aggregate") == "pod" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{
			{"test-ns": measuredDay(opencost.AllocationData{CPUCost: 10.0, RAMCost: 5.0})},
		}})
	}))
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{saveActualFn: func(_ context.Context, actual *models.CostActual) error { saved = actual; return nil }}

	err := newTestHook(t, srv, store).OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "cache",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
		Resources:            []models.ManagedResource{{Kind: "StatefulSet", Name: "redis"}},
	})
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Nil(t, saved.App, "without pods the application cannot be told apart from its namespace")
}

func TestOnApplicationSync_NoData_RecordsNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []map[string]opencost.AllocationData
	}{
		{name: "no window", data: nil},
		{name: "namespace missing from the window", data: []map[string]opencost.AllocationData{{}}},
		{name: "seconds of data and no cost yet", data: []map[string]opencost.AllocationData{{
			"test-ns": {Start: time.Now().Add(-5 * time.Second), End: time.Now()},
		}}},
		{name: "minutes of data with a cost", data: []map[string]opencost.AllocationData{{
			"test-ns": {Start: time.Now().Add(-3 * time.Minute), End: time.Now(), RAMCost: 0.00001},
		}}},
		{name: "a second short of the minimum period", data: []map[string]opencost.AllocationData{{
			"test-ns": {Start: time.Now().Add(-minMeasuredPeriod + time.Second), End: time.Now(), RAMCost: 0.01},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: tt.data})
			}))
			defer srv.Close()

			saved := false
			store := &mockStorage{saveActualFn: func(context.Context, *models.CostActual) error { saved = true; return nil }}

			err := newTestHook(t, srv, store).OnApplicationSync(context.Background(), &models.SyncNotification{
				ApplicationName:      "my-app",
				ApplicationNamespace: "argocd",
				Namespace:            "test-ns",
				SyncResult:           "Succeeded",
			})
			require.ErrorIs(t, err, ErrNoData)
			assert.False(t, saved, "a namespace OpenCost has no data for has an unknown cost, not $0")
		})
	}
}

func TestOnApplicationSync_MinutesOfData_RecordsOnceThereAreEnough(t *testing.T) {
	t.Parallel()

	end := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{{
			"test-ns": {Start: end.Add(-minMeasuredPeriod), End: end, RAMCost: 0.01},
		}}})
	}))
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{saveActualFn: func(_ context.Context, actual *models.CostActual) error { saved = actual; return nil }}

	err := newTestHook(t, srv, store).OnApplicationSync(context.Background(), succeededSync())
	require.NoError(t, err)
	require.NotNil(t, saved, "the minimum period of data is enough to project from")
	assert.InDelta(t, 86.4, saved.TotalMonthlyCost, 0.5) // 0.01 per 5 minutes over 720 hours
}

// newOpenCostWithDataAfter is an OpenCost that has no data for test-ns for the first noData queries of
// its cost, and a day of data after them.
func newOpenCostWithDataAfter(t *testing.T, noData int32) *httptest.Server {
	t.Helper()
	var queries atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/allocation" || r.URL.Query().Get("aggregate") != "namespace" {
			http.NotFound(w, r) // no efficiency or pods: a record has neither
			return
		}
		window := map[string]opencost.AllocationData{}
		if queries.Add(1) > noData {
			window["test-ns"] = measuredDay(opencost.AllocationData{CPUCost: 10.0, RAMCost: 5.0})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{window}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// observedResults collects the results of a hook's retries.
type observedResults struct {
	mu      sync.Mutex
	results []string
}

func (o *observedResults) observe(result string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.results = append(o.results, result)
}

func (o *observedResults) get() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.results...)
}

func newRetryingHook(ctx context.Context, t *testing.T, srv *httptest.Server, store storage.CostStorage, delays ...time.Duration) (*SyncHook, *observedResults) {
	t.Helper()
	h := newTestHook(t, srv, store)
	observed := &observedResults{}
	h.RetryWithoutData(ctx, delays, observed.observe)
	return h, observed
}

func pendingRetries(h *SyncHook) int {
	h.retries.mu.Lock()
	defer h.retries.mu.Unlock()
	return len(h.retries.pending)
}

// savedRecords is a store that hands each saved record to a channel.
func savedRecords() (*mockStorage, chan *models.CostActual) {
	saved := make(chan *models.CostActual, 8)
	return &mockStorage{saveActualFn: func(_ context.Context, actual *models.CostActual) error {
		saved <- actual
		return nil
	}}, saved
}

func succeededSync() *models.SyncNotification {
	return &models.SyncNotification{
		ApplicationName:      myApp.Name,
		ApplicationNamespace: myApp.Namespace,
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
		SyncTimestamp:        time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC),
	}
}

func TestOnApplicationSync_NoData_MeasuresAgainUntilOpenCostHasData(t *testing.T) {
	t.Parallel()

	store, saved := savedRecords()
	h, observed := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 2), store,
		time.Millisecond, time.Millisecond, time.Millisecond)

	require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)

	select {
	case actual := <-saved:
		assert.Equal(t, models.AppRef{Namespace: actual.ApplicationNamespace, Name: actual.ApplicationName}, myApp)
		assert.Equal(t, models.MeasuredOnSync, actual.MeasuredOn, "a retry's record is the sync's")
		assert.True(t, succeededSync().SyncTimestamp.Equal(actual.SyncTimestamp), "a retry keeps the sync's timestamp")
		assert.Positive(t, actual.TotalMonthlyCost)
	case <-time.After(5 * time.Second):
		t.Fatal("no record after OpenCost had data")
	}
	require.Eventually(t, func() bool { return pendingRetries(h) == 0 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"no_data", "success"}, observed.get(), "the first retry found no data, the second recorded")
	assert.Empty(t, saved, "nothing measured after the record")
}

func TestOnApplicationSync_NoData_GivesUpAfterTheLastRetry(t *testing.T) {
	t.Parallel()

	store, saved := savedRecords()
	h, observed := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 1000), store,
		time.Millisecond, time.Millisecond)

	require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)

	require.Eventually(t, func() bool { return pendingRetries(h) == 0 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"no_data", "no_data"}, observed.get())
	assert.Empty(t, saved)
}

func TestOnApplicationSync_NoData_RetryEndsWithTheContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store, saved := savedRecords()
	h, observed := newRetryingHook(ctx, t, newOpenCostWithDataAfter(t, 1), store, time.Hour)

	require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)
	require.Equal(t, 1, pendingRetries(h))
	cancel()

	require.Eventually(t, func() bool { return pendingRetries(h) == 0 }, 5*time.Second, time.Millisecond)
	assert.Empty(t, observed.get())
	assert.Empty(t, saved)
}

func TestOnApplicationSync_LaterSyncReplacesPendingRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		later       func(*models.SyncNotification)
		failSave    bool
		wantPending int
		wantSaved   int
	}{
		{name: "a succeeded sync measures instead", later: func(*models.SyncNotification) {}, wantPending: 0, wantSaved: 1},
		{name: "a succeeded sync that fails to record takes it over", later: func(*models.SyncNotification) {}, failSave: true, wantPending: 1},
		{name: "a move to another cluster", later: func(n *models.SyncNotification) { n.DestinationServer = "https://prod.example.com" }, wantPending: 0},
		{name: "a failed sync leaves it", later: func(n *models.SyncNotification) { n.SyncResult = "Failed" }, wantPending: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, saved := savedRecords()
			h, observed := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 1), store, time.Hour)
			require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)
			require.Equal(t, 1, pendingRetries(h))

			later := succeededSync()
			later.SyncTimestamp = later.SyncTimestamp.Add(time.Minute)
			tt.later(later)
			if tt.failSave {
				store.saveActualFn = func(context.Context, *models.CostActual) error { return errors.New("database down") }
				require.Error(t, h.OnApplicationSync(context.Background(), later))
			} else {
				require.NoError(t, h.OnApplicationSync(context.Background(), later))
			}

			assert.Equal(t, tt.wantPending, pendingRetries(h))
			assert.Len(t, saved, tt.wantSaved)
			assert.Empty(t, observed.get())
		})
	}
}

func TestOnApplicationSync_EarlierSyncFinishingLaterLeavesNoRetry(t *testing.T) {
	t.Parallel()

	store, saved := savedRecords()
	h, observed := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 0), store, time.Hour)

	// An earlier sync of the app starts, and finds no data only after a later sync has recorded.
	earlier := h.beginSync(myApp)
	later := succeededSync()
	later.SyncTimestamp = later.SyncTimestamp.Add(time.Minute)
	require.NoError(t, h.OnApplicationSync(context.Background(), later))
	h.endSync(myApp, *succeededSync(), earlier, ErrNoData)

	assert.Equal(t, 0, pendingRetries(h), "the later sync's record is newer than anything the earlier sync's retry would save")
	assert.Len(t, saved, 1)
	assert.Empty(t, observed.get())
}

func TestOnApplicationSync_FailureWithoutPendingRetry_SchedulesNone(t *testing.T) {
	t.Parallel()

	store := &mockStorage{saveActualFn: func(context.Context, *models.CostActual) error { return errors.New("database down") }}
	h, _ := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 0), store, time.Hour)

	require.Error(t, h.OnApplicationSync(context.Background(), succeededSync()))
	assert.Equal(t, 0, pendingRetries(h), "only a sync without OpenCost data starts retrying")
}

func TestOnApplicationSync_OverlappingSyncsFailing_KeepTheRetryTheyReplaced(t *testing.T) {
	t.Parallel()

	failed := errors.New("OpenCost down")
	for _, tc := range []struct {
		name         string
		newerSettles bool // the newer sync finishes first
	}{
		{"the earlier finishing first", false},
		{"the newer finishing first", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store, _ := savedRecords()
			h, _ := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 1), store, time.Hour)
			require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)
			require.Equal(t, 1, pendingRetries(h))

			// Two syncs start while the retry waits; the first replaces it, and both fail outright.
			first := h.beginSync(myApp)
			second := h.beginSync(myApp)
			if tc.newerSettles {
				h.endSync(myApp, *succeededSync(), second, failed)
				h.endSync(myApp, *succeededSync(), first, failed)
			} else {
				h.endSync(myApp, *succeededSync(), first, failed)
				h.endSync(myApp, *succeededSync(), second, failed)
			}

			assert.Equal(t, 1, pendingRetries(h), "the newest sync takes over the retry the first one replaced")
		})
	}
}

func TestOnApplicationSync_RecordedSync_OwesNoRetry(t *testing.T) {
	t.Parallel()

	store, _ := savedRecords()
	h, _ := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 1), store, time.Hour)
	require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)

	require.NoError(t, h.OnApplicationSync(context.Background(), succeededSync()), "OpenCost has data now")
	h.endSync(myApp, *succeededSync(), h.beginSync(myApp), errors.New("OpenCost down"))

	assert.Equal(t, 0, pendingRetries(h), "the recorded sync settled the retry it replaced")
}

func TestRetry_LaterSyncRecordedElsewhere_StopsRetrying(t *testing.T) {
	t.Parallel()

	store, saved := savedRecords()
	store.superseded.Store(true) // another replica has recorded a later sync of the app
	h, observed := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 1), store,
		time.Millisecond, time.Millisecond, time.Millisecond)

	require.ErrorIs(t, h.OnApplicationSync(context.Background(), succeededSync()), ErrNoData)

	require.Eventually(t, func() bool { return pendingRetries(h) == 0 }, 5*time.Second, time.Millisecond)
	assert.Empty(t, observed.get(), "a retry with nothing newer to store measured nothing of use")
	assert.Empty(t, saved)
}

func TestRetry_PanicReleasesItsSlot(t *testing.T) {
	t.Parallel()

	var panicking atomic.Bool
	panicking.Store(true)
	saved := make(chan *models.CostActual, 1)
	store := &mockStorage{saveActualFn: func(_ context.Context, actual *models.CostActual) error {
		if panicking.Load() {
			panic("storage bug")
		}
		saved <- actual
		return nil
	}}
	h, _ := newRetryingHook(context.Background(), t, newOpenCostWithDataAfter(t, 0), store, time.Millisecond)

	for i := range maxConcurrentRetries {
		n := succeededSync()
		n.ApplicationName = fmt.Sprintf("app-%d", i)
		h.endSync(models.AppRef{Namespace: n.ApplicationNamespace, Name: n.ApplicationName}, *n, 0, ErrNoData)
	}
	require.Eventually(t, func() bool { return pendingRetries(h) == 0 }, 5*time.Second, time.Millisecond)
	panicking.Store(false)
	h.endSync(myApp, *succeededSync(), 0, ErrNoData)

	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("a retry after others panicked never got to measure")
	}
}

func TestMeasurementResult(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "success", MeasurementResult(nil))
	assert.Equal(t, "no_data", MeasurementResult(ErrNoData))
	assert.Equal(t, "error", MeasurementResult(errors.New("connection refused")))
}

func TestOnApplicationSync_MissingNamespace_FallsBackToAppNamespace(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			saved = actual
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "", // intentionally empty
		SyncResult:           "Succeeded",
	})
	require.NoError(t, err)
	require.NotNil(t, saved)
}

func TestOnApplicationSync_StorageFails_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	store := &mockStorage{
		saveActualFn: func(_ context.Context, _ *models.CostActual) error {
			return errors.New("db unavailable")
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save actual costs")
}

func TestOnApplicationSync_PodFailure_IsNonFatal(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation":
			if r.URL.Query().Get("aggregate") == "pod" {
				http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
				Code: 200,
				Data: []map[string]opencost.AllocationData{
					{"test-ns": measuredDay(opencost.AllocationData{CPUCost: 10.0, RAMCost: 5.0})},
				},
			})
		case "/allocation/summary":
			_ = json.NewEncoder(w).Encode(opencost.SummaryAllocationResponse{
				Code: 200,
				Data: opencost.SummaryAllocationResponseData{
					Sets: []opencost.SummaryAllocationSet{{
						Allocations: map[string]opencost.SummaryAllocationData{
							"test-ns": {CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.4, CPUCost: 0.3},
						},
					}},
				},
			})
		}
	}))
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			saved = actual
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
	})
	require.NoError(t, err, "pod failure should not abort the sync")
	require.NotNil(t, saved)
	assert.Empty(t, saved.Pods, "pods should be nil/empty when pod call fails")
	assert.Nil(t, saved.IdleMonthlyCost, "without its pods the namespace's idle cost is not measured")
}

func TestOnApplicationSync_EfficiencyFailure_IsNonFatal(t *testing.T) {
	t.Parallel()

	// OpenCost server that succeeds for /allocation but fails for /allocation/summary
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation":
			_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
				Code: 200,
				Data: []map[string]opencost.AllocationData{
					{"test-ns": measuredDay(opencost.AllocationData{CPUCost: 10.0, RAMCost: 5.0})},
				},
			})
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var saved *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			saved = actual
			return nil
		},
	}

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
	})
	require.NoError(t, err, "efficiency failure should not abort the sync")
	require.NotNil(t, saved)
	assert.Nil(t, saved.Efficiency, "efficiency should be nil when summary call fails")
	require.NotNil(t, saved.IdleMonthlyCost, "the idle cost comes from the pods, not the summary")
	assert.Zero(t, *saved.IdleMonthlyCost)
}

// ---- GetLatestCostForApplication ----

func TestGetLatestCostForApplication_DelegatesToStorage(t *testing.T) {
	t.Parallel()

	want := &models.CostActual{ApplicationName: "my-app", TotalMonthlyCost: 100.0}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, app models.AppRef) (*models.CostActual, error) {
			assert.Equal(t, myApp, app)
			return want, nil
		},
	}

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	hook := newTestHook(t, srv, store)
	got, err := hook.GetLatestCostForApplication(context.Background(), myApp)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetLatestCostForApplication_StorageError(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return nil, errors.New("not found")
		},
	}

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	hook := newTestHook(t, srv, store)
	_, err := hook.GetLatestCostForApplication(context.Background(), models.AppRef{Namespace: "argocd", Name: "missing"})
	require.Error(t, err)
}

// ---- GetCostTrend ----

func TestGetCostTrend_DelegatesToStorage(t *testing.T) {
	t.Parallel()

	want := []models.CostActual{
		{ApplicationName: "my-app", TotalMonthlyCost: 100.0},
		{ApplicationName: "my-app", TotalMonthlyCost: 120.0},
	}
	store := &mockStorage{
		getActualsInDateRangeFn: func(_ context.Context, app models.AppRef, _, _ time.Time) ([]models.CostActual, error) {
			assert.Equal(t, myApp, app)
			return want, nil
		},
	}

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	hook := newTestHook(t, srv, store)
	got, err := hook.GetCostTrend(context.Background(), myApp, 30)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ---- newRecordID (finding #8 — record ID must not be derived from user input) ----

func TestNewRecordID_IsHex32Chars(t *testing.T) {
	t.Parallel()

	id := newRecordID()
	assert.Len(t, id, 32, "expected 32 hex chars (16 bytes)")
	for _, c := range id {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"expected lowercase hex char, got %q", c)
	}
}

func TestNewRecordID_IsUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := newRecordID()
		_, dup := seen[id]
		assert.False(t, dup, "duplicate ID generated: %s", id)
		seen[id] = struct{}{}
	}
}

func TestOnApplicationSync_RecordID_NotDerivedFromAppName(t *testing.T) {
	t.Parallel()

	var savedActual *models.CostActual
	store := &mockStorage{
		saveActualFn: func(_ context.Context, actual *models.CostActual) error {
			savedActual = actual
			return nil
		},
	}

	srv := newOpenCostTestServer(t)
	defer srv.Close()

	hook := newTestHook(t, srv, store)
	err := hook.OnApplicationSync(context.Background(), &models.SyncNotification{
		ApplicationName:      "billing-service",
		ApplicationNamespace: "argocd",
		Namespace:            "test-ns",
		SyncResult:           "Succeeded",
	})
	require.NoError(t, err)
	require.NotNil(t, savedActual)

	assert.NotContains(t, savedActual.ID, "billing-service",
		"record ID must not contain the application name")
	assert.Len(t, savedActual.ID, 32, "record ID must be a 32-char hex string")
}

func TestOnApplicationSync_NoPods_IdleCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace opencost.AllocationData
		want      *float64
	}{
		{name: "pods missing from a namespace with CPU and memory cost", namespace: opencost.AllocationData{CPUCost: 10, RAMCost: 5}},
		{name: "namespace without CPU or memory cost", namespace: opencost.AllocationData{PVCost: 2, TotalCost: 2}, want: new(float64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				data := map[string]opencost.AllocationData{"test-ns": measuredDay(tt.namespace)}
				if r.URL.Query().Get("aggregate") == "pod" {
					data = map[string]opencost.AllocationData{"__idle__": {CPUCost: 3}}
				}
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{data}})
			}))
			defer srv.Close()

			var saved *models.CostActual
			store := &mockStorage{
				saveActualFn: func(_ context.Context, actual *models.CostActual) error {
					saved = actual
					return nil
				},
			}

			err := newTestHook(t, srv, store).OnApplicationSync(context.Background(), &models.SyncNotification{
				ApplicationName:      "my-app",
				ApplicationNamespace: "argocd",
				Namespace:            "test-ns",
				SyncResult:           "Succeeded",
			})
			require.NoError(t, err)
			require.NotNil(t, saved)
			assert.Equal(t, tt.want, saved.IdleMonthlyCost)
		})
	}
}
