package hook

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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

func (m *mockStorage) SaveFirstActual(context.Context, *models.CostActual) (bool, error) {
	return true, nil
}

func (m *mockStorage) Lock(context.Context, string) (func(), error) { return func() {}, nil }

// newOpenCostTestServer returns an httptest.Server that serves valid OpenCost
// allocation and summary responses. Caller must call Close() when done.
func newOpenCostTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation":
			if r.URL.Query().Get("aggregate") == "pod" {
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
					Code: 200,
					Data: []map[string]opencost.AllocationData{
						{
							"redis-0": {
								CPUCost: 6.0, RAMCost: 3.0, TotalCost: 9.0,
								CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.25,
								RAMByteRequestAverage: 100, RAMByteUsageAverage: 50,
								Properties: opencost.AllocationProperties{Pod: "redis-0", Namespace: "test-ns", ControllerKind: "statefulset", Controller: "redis"},
							},
							"nginx-abc": {
								CPUCost: 4.0, RAMCost: 2.0, TotalCost: 6.0,
								Properties: opencost.AllocationProperties{Pod: "nginx-abc", Namespace: "test-ns", ControllerKind: "deployment", Controller: "nginx"},
							},
						},
					},
				})
			} else {
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
					Code: 200,
					Data: []map[string]opencost.AllocationData{
						{
							"test-ns": {
								CPUCost: 10.0, RAMCost: 5.0,
								RawAllocationOnly: &opencost.RawAllocationOnly{CPUCoreUsageMax: 0.8, RAMByteUsageMax: 400 * 1024 * 1024},
							},
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
								Efficiency:            0.55,
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
	assert.InDelta(t, 0.55, saved.Efficiency.TotalEfficiency, 1e-9)
	assert.InDelta(t, 0.8, saved.Efficiency.CPUCoresUsedMax, 1e-9, "peak CPU should be merged")
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
			{"test-ns": {CPUCost: 10.0, RAMCost: 5.0}},
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
					{"test-ns": {CPUCost: 10.0, RAMCost: 5.0}},
				},
			})
		case "/allocation/summary":
			_ = json.NewEncoder(w).Encode(opencost.SummaryAllocationResponse{
				Code: 200,
				Data: opencost.SummaryAllocationResponseData{
					Sets: []opencost.SummaryAllocationSet{{
						Allocations: map[string]opencost.SummaryAllocationData{
							"test-ns": {CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.4, Efficiency: 0.55},
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
					{"test-ns": {CPUCost: 10.0, RAMCost: 5.0}},
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
