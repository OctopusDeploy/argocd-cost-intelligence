package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/hook"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/rightsizing"
)

// redisControllers are the pod-owning resources of the redis application, which deploys into the argocd
// namespace beside Argo CD's own pods.
var redisControllers = models.ManagedResources{
	{Kind: "Deployment", Name: "redis", Namespace: "argocd"},
	{Kind: "CronJob", Name: "test-cronjob", Namespace: "argocd"},
}

// redisRecord is a measurement of the argocd namespace ($9.55/mo) with redis's own share ($0.63/mo),
// or without it when the record predates application measurements.
func redisRecord(id string, namespaceCost, appCost float64, measured bool) *models.CostActual {
	record := &models.CostActual{
		ID:                   id,
		ApplicationName:      "redis",
		ApplicationNamespace: "argocd",
		DeploymentNamespace:  "argocd",
		TotalMonthlyCost:     namespaceCost,
		Pods:                 []models.PodCost{{Name: "argocd-redis-1"}, {Name: "redis-1"}},
		Efficiency:           &models.EfficiencyMetrics{CPUEfficiency: 0.02},
	}
	if measured {
		record.App = &models.AppMeasurement{
			Controllers:      redisControllers,
			TotalMonthlyCost: appCost,
			OpenCostOutput:   models.OpenCostMetrics{AllocationTotals: models.AllocationTotal{TotalCost: appCost}},
			Efficiency:       &models.EfficiencyMetrics{CPUEfficiency: 0.96},
			Pods:             []models.PodCost{{Name: "redis-1", Controller: "redis"}},
		}
	}
	return record
}

// newScopedTestServer wires a server whose OpenCost answers with handler.
func newScopedTestServer(t *testing.T, store *mockStorage, handler http.HandlerFunc, opts ...Option) *CostAPIServer {
	t.Helper()
	ocSrv := httptest.NewServer(handler)
	t.Cleanup(ocSrv.Close)
	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	opts = append(opts, WithClusterScopeApp(testGateApp), WithWebhookSecret(testWebhookSecret))
	return NewCostAPIServer(hook.NewSyncHook(ocClient, store, time.Millisecond, logger), store, ocClient, logger, opts...)
}

func getJSON(t *testing.T, srv *CostAPIServer, path string, into any) {
	t.Helper()
	rr := doRequest(t, srv, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), into))
}

func TestHandleGetActualCost_ServesTheApplicationsOwnMeasurement(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) {
			return redisRecord("r3", 9.55, 0.63, true), nil
		},
	}
	var got map[string]any
	getJSON(t, newTestAPIServer(t, store), "/api/cost-analysis/redis/actual", &got)

	assert.Equal(t, "application", got["scope"])
	assert.InDelta(t, 0.63, got["totalMonthlyCost"], 1e-9)
	assert.InDelta(t, 0.96, got["efficiency"].(map[string]any)["cpuEfficiency"], 1e-9)
	pods := got["pods"].([]any)
	require.Len(t, pods, 1)
	assert.Equal(t, "redis-1", pods[0].(map[string]any)["name"])
	assert.NotContains(t, got, "App")
}

func TestHandleGetActualCost_OlderRecordIsTheNamespace(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) {
			return redisRecord("r1", 9.55, 0, false), nil
		},
	}
	var got map[string]any
	getJSON(t, newTestAPIServer(t, store), "/api/cost-analysis/redis/actual", &got)

	assert.Equal(t, "namespace", got["scope"])
	assert.InDelta(t, 9.55, got["totalMonthlyCost"], 1e-9)
}

func TestHandleGetActualCost_AnomalyComparesTheApplicationWithItself(t *testing.T) {
	t.Parallel()

	// The namespace is steady at $9.55, but redis went from $0.60 to $2.00: a spike of its own. The
	// older namespace-only records would hide it behind the namespace's higher cost.
	current := redisRecord("r5", 9.55, 2.0, true)
	store := &mockStorage{
		getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) { return current, nil },
		getRecentActualsFn: func(context.Context, models.AppRef, int) ([]models.CostActual, error) {
			return []models.CostActual{
				*current,
				*redisRecord("r4", 9.55, 0.6, true),
				*redisRecord("r3", 9.55, 0.6, true),
				*redisRecord("r2", 9.55, 0.6, true),
				*redisRecord("r1", 9.55, 0, false),
			}, nil
		},
	}
	var got models.CostActual
	getJSON(t, newTestAPIServer(t, store), "/api/cost-analysis/redis/actual", &got)

	require.NotNil(t, got.Anomaly)
	assert.Equal(t, "alert", got.Anomaly.Severity)
	assert.InDelta(t, 0.6, got.Anomaly.RollingAvg, 1e-9)
	assert.Equal(t, 3, got.Anomaly.RecordCount)
}

func TestHandleGetCostTrend_ServesEachRecordAsTheApplicationSeesIt(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getActualsInDateRangeFn: func(context.Context, models.AppRef, time.Time, time.Time) ([]models.CostActual, error) {
			return []models.CostActual{*redisRecord("r2", 9.55, 0.63, true), *redisRecord("r1", 8.54, 0, false)}, nil
		},
	}
	var got struct {
		Trend []models.CostActual `json:"trend"`
	}
	getJSON(t, newTestAPIServer(t, store), "/api/cost-analysis/redis/trend", &got)

	require.Len(t, got.Trend, 2)
	assert.Equal(t, models.ScopeApplication, got.Trend[0].Scope)
	assert.InDelta(t, 0.63, got.Trend[0].TotalMonthlyCost, 1e-9)
	assert.Equal(t, models.ScopeNamespace, got.Trend[1].Scope)
	assert.InDelta(t, 8.54, got.Trend[1].TotalMonthlyCost, 1e-9)
}

func TestHandleGetDailyCosts_CoversTheApplicationsControllers(t *testing.T) {
	t.Parallel()

	start := time.Now().UTC().Truncate(24 * time.Hour)
	controller := func(kind, name string, cost float64) opencost.AllocationData {
		return opencost.AllocationData{
			Start: start, End: start.Add(24 * time.Hour), CPUCost: cost, TotalCost: cost,
			Properties: opencost.AllocationProperties{Namespace: "argocd", ControllerKind: kind, Controller: name},
		}
	}
	for _, tt := range []struct {
		name          string
		record        *models.CostActual
		wantAggregate string
		wantScope     string
		wantTotal     float64
	}{
		{"measured application", redisRecord("r2", 9.55, 0.63, true), "controller", "application", 0.02},
		{"older record", redisRecord("r1", 9.55, 0, false), "namespace", "namespace", 0.27},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &mockStorage{
				getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) { return tt.record, nil },
			}
			srv := newScopedTestServer(t, store, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.wantAggregate, r.URL.Query().Get("aggregate"))
				_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{{
					"deployment:redis":        controller("deployment", "redis", 0.02),
					"deployment:argocd-redis": controller("deployment", "argocd-redis", 0.25),
				}}})
			})

			var got struct {
				Scope      string             `json:"scope"`
				DailyCosts []models.DailyCost `json:"dailyCosts"`
			}
			getJSON(t, srv, "/api/cost-analysis/redis/daily?days=1", &got)

			assert.Equal(t, tt.wantScope, got.Scope)
			require.Len(t, got.DailyCosts, 1)
			assert.InDelta(t, tt.wantTotal, got.DailyCosts[0].TotalCost, 1e-9)
		})
	}
}

func TestHandleGetAppOrphanedPods_OnlyTheApplicationsPods(t *testing.T) {
	t.Parallel()

	orphan := func(pod, kind, controller string) opencost.AllocationData {
		return opencost.AllocationData{
			PVCost: 0.01, TotalCost: 0.01,
			Properties: opencost.AllocationProperties{Namespace: "argocd", Pod: pod, ControllerKind: kind, Controller: controller},
		}
	}
	store := &mockStorage{
		getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) {
			return redisRecord("r2", 9.55, 0.63, true), nil
		},
	}
	srv := newScopedTestServer(t, store, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{{
			"test-cronjob-29123456-x": orphan("test-cronjob-29123456-x", "job", "test-cronjob-29123456"),
			"argocd-dex-x":            orphan("argocd-dex-x", "deployment", "argocd-dex-server"),
		}}})
	})

	var got struct {
		OrphanedPods []models.OrphanedPod `json:"orphanedPods"`
	}
	getJSON(t, srv, "/api/cost-analysis/redis/orphaned-pods", &got)

	require.Len(t, got.OrphanedPods, 1)
	assert.Equal(t, "test-cronjob-29123456-x", got.OrphanedPods[0].Name)
}

func TestHandleGetDailyCosts_AsksOpenCostForTheRequestedDays(t *testing.T) {
	t.Parallel()

	var windows []string
	store := &mockStorage{
		getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) {
			return redisRecord("r1", 9.55, 0.63, true), nil
		},
	}
	srv := newScopedTestServer(t, store, func(w http.ResponseWriter, r *http.Request) {
		windows = append(windows, r.URL.Query().Get("window"))
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200})
	})

	var got struct {
		Days int `json:"days"`
	}
	getJSON(t, srv, "/api/cost-analysis/redis/daily?from=2026-09-01&to=2026-09-30", &got)
	assert.Equal(t, 30, got.Days)
	getJSON(t, srv, "/api/cost-analysis/redis/daily?days=90", &got)
	assert.Equal(t, 90, got.Days)

	require.Len(t, windows, 2)
	assert.Equal(t, "2026-09-01T00:00:00Z,2026-10-01T00:00:00Z", windows[0])
	today := time.Now().UTC().Truncate(24 * time.Hour)
	assert.True(t, strings.HasPrefix(windows[1], today.AddDate(0, 0, -89).Format(time.RFC3339)+","),
		"the last 90 days start 89 days before today: %s", windows[1])
}

func TestHandleGetDailyCosts_ServesRecordedDaysAndAsksOpenCostForTheRest(t *testing.T) {
	t.Parallel()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1).Format(time.DateOnly)
	var windows []string
	var storedRange []string
	store := &mockStorage{
		getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) {
			return redisRecord("r1", 9.55, 0.63, true), nil
		},
		getDailyProgressFn: func(_ context.Context, app models.AppRef) (*models.DailyProgress, error) {
			assert.Equal(t, models.AppRef{Namespace: "argocd", Name: "redis"}, app)
			return &models.DailyProgress{Through: yesterday, Scope: models.ScopeApplication}, nil
		},
		getDailyCostsFn: func(_ context.Context, _ models.AppRef, scope, from, to string) ([]models.DailyCost, error) {
			assert.Equal(t, models.ScopeApplication, scope)
			storedRange = []string{from, to}
			return []models.DailyCost{{Date: yesterday, TotalCost: 0.5}}, nil
		},
	}
	srv := newScopedTestServer(t, store, func(w http.ResponseWriter, r *http.Request) {
		windows = append(windows, r.URL.Query().Get("window"))
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{{
			"deployment:redis": {
				Start: today, End: today.Add(time.Hour), TotalCost: 0.1,
				Properties: opencost.AllocationProperties{Namespace: "argocd", ControllerKind: "deployment", Controller: "redis"},
			},
		}}})
	})

	var got struct {
		DailyCosts []models.DailyCost `json:"dailyCosts"`
	}
	getJSON(t, srv, "/api/cost-analysis/redis/daily?days=365", &got)

	assert.Equal(t, []string{today.AddDate(0, 0, -364).Format(time.DateOnly), yesterday}, storedRange)
	require.Len(t, windows, 1)
	assert.True(t, strings.HasPrefix(windows[0], today.Format(time.RFC3339)+","), "only today is asked of OpenCost: %s", windows[0])
	require.Len(t, got.DailyCosts, 2)
	assert.Equal(t, yesterday, got.DailyCosts[0].Date)
	assert.Equal(t, today.Format(time.DateOnly), got.DailyCosts[1].Date)
}

// stubLister answers List with vpas or err, and records the namespaces it was asked about.
type stubLister struct {
	vpas       []rightsizing.VPA
	err        error
	namespaces []string
}

func (l *stubLister) List(_ context.Context, namespace string) ([]rightsizing.VPA, error) {
	l.namespaces = append(l.namespaces, namespace)
	return l.vpas, l.err
}

// newRightsizingServer wires a server whose OpenCost answers container allocations for the argocd
// namespace, and records the aggregations it is asked for.
func newRightsizingServer(t *testing.T, record *models.CostActual, opts ...Option) (*CostAPIServer, *[]string) {
	t.Helper()
	var aggregates []string
	store := &mockStorage{getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) { return record, nil }}
	ocSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aggregates = append(aggregates, r.URL.Query().Get("aggregate"))
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{{
			"redis-1/redis": {
				Minutes: 1440, CPUCoreRequestAverage: 0.5, CPUCoreUsageAverage: 0.01, CPUCoreHours: 12, CPUCost: 0.4,
				Properties: opencost.AllocationProperties{Pod: "redis-1", Container: "redis", ControllerKind: "deployment", Controller: "redis"},
			},
		}}})
	}))
	t.Cleanup(ocSrv.Close)
	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	opts = append(opts, WithClusterScopeApp(testGateApp), WithWebhookSecret(testWebhookSecret))
	return NewCostAPIServer(hook.NewSyncHook(ocClient, store, time.Millisecond, logger), store, ocClient, logger, opts...), &aggregates
}

func TestHandleGetRightsizing_PricesTheVPAsOfTheApplicationsControllers(t *testing.T) {
	t.Parallel()

	lister := &stubLister{vpas: []rightsizing.VPA{{
		TargetKind: "Deployment", TargetName: "redis", UpdateMode: "Off",
		Containers: []rightsizing.ContainerRecommendation{{Name: "redis", Target: models.Resources{CPUCores: 0.1, MemoryBytes: 64 << 20}}},
	}}}
	srv, aggregates := newRightsizingServer(t, redisRecord("r1", 9.55, 0.63, true), WithVPALister(lister))

	var got models.Rightsizing
	getJSON(t, srv, "/api/cost-analysis/redis/rightsizing", &got)

	assert.Equal(t, []string{"argocd"}, lister.namespaces, "the app's deployment namespace, from its record")
	assert.Equal(t, []string{"pod,container"}, *aggregates)
	assert.True(t, got.Available)
	require.Len(t, got.Workloads, 2)
	assert.Equal(t, "redis", got.Workloads[0].Name)
	assert.Equal(t, models.RightsizingRecommended, got.Workloads[0].Status)
	require.Len(t, got.Workloads[0].Containers, 1)
	assert.Positive(t, got.MonthlySaving)
	assert.Equal(t, "test-cronjob", got.Workloads[1].Name)
	assert.Equal(t, models.RightsizingNone, got.Workloads[1].Status)
}

func TestHandleGetRightsizing_UnavailableWithoutTheVPAAPI(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string][]Option{
		"not in a cluster":        nil,
		"no VPA or no permission": {WithVPALister(&stubLister{err: rightsizing.ErrUnavailable})},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, aggregates := newRightsizingServer(t, redisRecord("r1", 9.55, 0.63, true), opts...)

			var got models.Rightsizing
			getJSON(t, srv, "/api/cost-analysis/redis/rightsizing", &got)

			assert.False(t, got.Available)
			assert.NotNil(t, got.Workloads)
			assert.Empty(t, got.Workloads)
			assert.Empty(t, *aggregates, "OpenCost is not asked")
		})
	}
}

func TestHandleGetRightsizing_NoWorkloadsForARecordWithoutControllers(t *testing.T) {
	t.Parallel()

	lister := &stubLister{vpas: []rightsizing.VPA{{TargetKind: "Deployment", TargetName: "argocd-redis", UpdateMode: "Off"}}}
	srv, aggregates := newRightsizingServer(t, redisRecord("r1", 9.55, 0, false), WithVPALister(lister))

	var got models.Rightsizing
	getJSON(t, srv, "/api/cost-analysis/redis/rightsizing", &got)

	assert.True(t, got.Available)
	assert.Empty(t, got.Workloads, "which of the namespace's VPAs are this app's is not known")
	assert.Empty(t, *aggregates)
}

func TestHandleGetRightsizing_ReportsAFailureToListVPAs(t *testing.T) {
	t.Parallel()

	srv, _ := newRightsizingServer(t, redisRecord("r1", 9.55, 0.63, true), WithVPALister(&stubLister{err: errors.New("timeout")}))
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/rightsizing", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.NotContains(t, rr.Body.String(), "timeout", "the cause is logged, not sent")
}

func TestHandleGetRightsizing_ReadsTheVPAsWhereTheApplicationDeploys(t *testing.T) {
	t.Parallel()

	record := redisRecord("r1", 9.55, 0.63, true)
	record.DeploymentNamespace = "cache"
	lister := &stubLister{}
	srv, aggregates := newRightsizingServer(t, record, WithVPALister(lister))

	var got models.Rightsizing
	getJSON(t, srv, "/api/cost-analysis/redis/rightsizing", &got)

	assert.Equal(t, []string{"cache"}, lister.namespaces, "not argocd, where the Application object lives")
	assert.Empty(t, *aggregates, "without a VPA there is nothing to price")
	assert.True(t, got.Available)
}

func TestHandleGetRightsizing_ReportsAFailureToReadContainerCosts(t *testing.T) {
	t.Parallel()

	store := &mockStorage{getLatestActualFn: func(context.Context, models.AppRef) (*models.CostActual, error) {
		return redisRecord("r1", 9.55, 0.63, true), nil
	}}
	lister := &stubLister{vpas: []rightsizing.VPA{{TargetKind: "Deployment", TargetName: "redis", UpdateMode: "Off"}}}
	srv := newScopedTestServer(t, store, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}, WithVPALister(lister))
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/rightsizing", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "failed to retrieve container costs")
}
