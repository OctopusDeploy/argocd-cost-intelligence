package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/hook"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
)

var testGateApp = models.AppRef{Namespace: "argocd", Name: "cost-intel"}

// redisStore holds cost data for the application redis, deploying to the namespace cache.
func redisStore() *mockStorage {
	return &mockStorage{
		getLatestActualFn: func(_ context.Context, app models.AppRef) (*models.CostActual, error) {
			return &models.CostActual{ApplicationName: app.Name, ApplicationNamespace: app.Namespace, DeploymentNamespace: "cache", TotalMonthlyCost: 100}, nil
		},
	}
}

func asApp(app string) map[string]string {
	if app == "" {
		return nil
	}
	return map[string]string{headerApplicationName: app}
}

var appScopedRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/cost-analysis/redis/actual"},
	{http.MethodGet, "/api/cost-analysis/redis/trend"},
	{http.MethodGet, "/api/cost-analysis/redis/daily"},
	{http.MethodGet, "/api/cost-analysis/redis/orphaned-pods"},
	{http.MethodGet, "/api/cost-analysis/redis/rightsizing"},
	{http.MethodPost, "/api/cost-analysis/redis/cloud-estimate"},
}

func TestAppScopedRoutes_RejectOtherApplications(t *testing.T) {
	t.Parallel()

	callers := map[string]string{
		"no header":        "",
		"malformed header": "redis",
		"other app":        "argocd:postgres",
		"gate app":         testGateApp.String(),
	}
	for _, route := range appScopedRoutes {
		for name, caller := range callers {
			t.Run(route.path+"/"+name, func(t *testing.T) {
				t.Parallel()
				srv := newTestAPIServer(t, redisStore())
				rr := doRequestWithHeaders(t, srv, route.method, route.path, nil, asApp(caller))
				assert.Equal(t, http.StatusForbidden, rr.Code)
			})
		}
	}
}

func TestAppScopedRoutes_AdmitTheNamedApplication(t *testing.T) {
	t.Parallel()

	for _, route := range appScopedRoutes {
		t.Run(route.path, func(t *testing.T) {
			t.Parallel()
			srv := newTestAPIServer(t, redisStore())
			// The namespace is that of the Application resource, which need not be argocd.
			rr := doRequestWithHeaders(t, srv, route.method, route.path, nil, asApp("team-a:redis"))
			assert.NotEqual(t, http.StatusForbidden, rr.Code, rr.Body.String())
		})
	}
}

var clusterScopedRoutes = []string{
	"/api/cost-analysis/namespaces",
	"/api/cost-analysis/namespace/cache/summary",
	"/api/cost-analysis/namespace/cache/apps",
	"/api/cost-analysis/namespace/cache/orphaned-pods",
	"/api/cost-analysis/chargeback?label=team",
	"/api/cost-analysis/chargeback/labels",
	"/api/cost-analysis/cluster/overhead",
	"/api/cost-analysis/waste",
}

func TestClusterScopedRoutes_RequireTheGateApplication(t *testing.T) {
	t.Parallel()

	callers := map[string]string{
		"no header":           "",
		"other app":           "argocd:redis",
		"same name elsewhere": "team-a:" + testGateApp.Name,
	}
	for _, path := range clusterScopedRoutes {
		for name, caller := range callers {
			t.Run(path+"/"+name, func(t *testing.T) {
				t.Parallel()
				srv := newTestAPIServer(t, redisStore())
				rr := doRequestWithHeaders(t, srv, http.MethodGet, path, nil, asApp(caller))
				assert.Equal(t, http.StatusForbidden, rr.Code)
			})
		}
		t.Run(path+"/gate app", func(t *testing.T) {
			t.Parallel()
			srv := newTestAPIServer(t, redisStore())
			rr := doRequestWithHeaders(t, srv, http.MethodGet, path, nil, asApp(testGateApp.String()))
			assert.NotEqual(t, http.StatusForbidden, rr.Code, rr.Body.String())
		})
	}
}

func TestClusterScopedRoutes_DisabledWithoutGateApplication(t *testing.T) {
	t.Parallel()

	ocClient := opencost.NewClient("http://opencost.invalid", time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	syncHook := hook.NewSyncHook(ocClient, redisStore(), time.Millisecond, logger)
	srv := NewCostAPIServer(syncHook, redisStore(), ocClient, logger)

	for _, path := range clusterScopedRoutes {
		rr := doRequestWithHeaders(t, srv, http.MethodGet, path, nil, asApp(testGateApp.String()))
		assert.Equal(t, http.StatusForbidden, rr.Code, path)
		assert.Contains(t, rr.Body.String(), "CLUSTER_SCOPE_APP", path)
	}
}

func TestConfig_ReportsGateApplication(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, redisStore())

	rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/config", nil, nil)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	rr = doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/config", nil, asApp("argocd:redis"))
	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		ClusterScopeApp *models.AppRef `json:"clusterScopeApp"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.NotNil(t, body.ClusterScopeApp)
	assert.Equal(t, testGateApp, *body.ClusterScopeApp)
}

func TestConfig_NullWithoutGateApplication(t *testing.T) {
	t.Parallel()

	ocClient := opencost.NewClient("http://opencost.invalid", time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	srv := NewCostAPIServer(hook.NewSyncHook(ocClient, redisStore(), time.Millisecond, logger), redisStore(), ocClient, logger)

	rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/config", nil, asApp("argocd:redis"))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"clusterScopeApp":null}`, rr.Body.String())
}

// newFilterRecordingServer returns a server whose OpenCost records the filter of every allocation
// query, so a test can see which namespace a handler measured.
func newFilterRecordingServer(t *testing.T, store *mockStorage) (*CostAPIServer, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var filters []string
	ocSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		filters = append(filters, r.URL.Query().Get("filter"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200})
	}))
	t.Cleanup(ocSrv.Close)

	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	srv := NewCostAPIServer(hook.NewSyncHook(ocClient, store, time.Millisecond, logger), store, ocClient, logger)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), filters...)
	}
}

func TestDailyCosts_IgnoresCallerNamespace(t *testing.T) {
	t.Parallel()

	srv, filters := newFilterRecordingServer(t, redisStore())
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/daily?namespace=kube-system", nil)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "cache", body["namespace"])
	assert.Equal(t, []string{`namespace:"cache"`}, filters())
}

func TestAppOrphanedPods_UsesApplicationNamespace(t *testing.T) {
	t.Parallel()

	srv, filters := newFilterRecordingServer(t, redisStore())
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/orphaned-pods", nil)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotEmpty(t, filters())
	for _, f := range filters() {
		assert.Contains(t, f, `"cache"`)
	}
}

func TestAppOrphanedPods_NoRecord_Returns404(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return nil, assert.AnError
		},
	}
	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/orphaned-pods", nil)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Two Applications named web: with apps-in-any-namespace, team-a:web and team-b:web are different
// Applications, deploying to different namespaces. A viewer of one must never see the other's data.
var (
	teamAWeb = models.AppRef{Namespace: "team-a", Name: "web"}
	teamBWeb = models.AppRef{Namespace: "team-b", Name: "web"}
)

// sameNameStore is keyed by Application like the real storage: it holds cost records for both
// Applications named web, and a cloud estimate for team-b's only.
func sameNameStore() *mockStorage {
	actuals := map[models.AppRef]models.CostActual{
		teamAWeb: {ID: "a1", ApplicationName: "web", ApplicationNamespace: "team-a", DeploymentNamespace: "web-a", TotalMonthlyCost: 100},
		teamBWeb: {ID: "b1", ApplicationName: "web", ApplicationNamespace: "team-b", DeploymentNamespace: "web-b", TotalMonthlyCost: 900},
	}
	estimates := map[models.AppRef]models.CloudServiceEstimate{
		teamBWeb: {Source: "infracost", TotalMonthlyCost: 5000},
	}
	records := func(app models.AppRef) []models.CostActual {
		if a, ok := actuals[app]; ok {
			return []models.CostActual{a}
		}
		return nil
	}
	return &mockStorage{
		getLatestActualFn: func(_ context.Context, app models.AppRef) (*models.CostActual, error) {
			a, ok := actuals[app]
			if !ok {
				return nil, errors.New("no cost actual found for application " + app.String())
			}
			return &a, nil
		},
		getRecentActualsFn: func(_ context.Context, app models.AppRef, _ int) ([]models.CostActual, error) {
			return records(app), nil
		},
		getActualsInDateRangeFn: func(_ context.Context, app models.AppRef, _, _ time.Time) ([]models.CostActual, error) {
			return records(app), nil
		},
		getCloudEstimateFn: func(_ context.Context, app models.AppRef) (*models.CloudServiceEstimate, error) {
			if e, ok := estimates[app]; ok {
				return &e, nil
			}
			return nil, nil
		},
	}
}

func TestActual_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, sameNameStore())

	rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/web/actual", nil, asApp(teamAWeb.String()))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "a1", got.ID)
	assert.Equal(t, "team-a", got.ApplicationNamespace)
	assert.Nil(t, got.CloudEstimate, "team-b:web's cloud estimate must not be attached to team-a:web")

	rr = doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/web/actual", nil, asApp(teamBWeb.String()))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got = models.CostActual{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "b1", got.ID)
	require.NotNil(t, got.CloudEstimate)
	assert.InDelta(t, 5000.0, got.CloudEstimate.TotalMonthlyCost, 1e-9)

	// A third Application named web has no data of its own, and gets nobody else's.
	rr = doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/web/actual", nil, asApp("team-c:web"))
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.NotContains(t, rr.Body.String(), "a1")
	assert.NotContains(t, rr.Body.String(), "b1")
}

func TestTrend_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, sameNameStore())
	rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/web/trend", nil, asApp(teamAWeb.String()))

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Trend []models.CostActual `json:"trend"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Trend, 1)
	assert.Equal(t, "a1", body.Trend[0].ID)
}

func TestDeploymentNamespaceRoutes_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/api/cost-analysis/web/daily", "/api/cost-analysis/web/orphaned-pods"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			srv, filters := newFilterRecordingServer(t, sameNameStore())
			rr := doRequestWithHeaders(t, srv, http.MethodGet, path, nil, asApp(teamAWeb.String()))

			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			require.NotEmpty(t, filters())
			for _, f := range filters() {
				assert.Contains(t, f, `"web-a"`, "team-a:web deploys to web-a")
				assert.NotContains(t, f, `"web-b"`, "web-b is where team-b:web deploys")
			}
		})
	}
}

func TestCloudEstimate_SavedForTheHeaderApplication(t *testing.T) {
	t.Parallel()

	var saved []models.AppRef
	store := &mockStorage{
		saveCloudEstimateFn: func(_ context.Context, app models.AppRef, _ *models.CloudServiceEstimate) error {
			saved = append(saved, app)
			return nil
		},
	}
	srv := newTestAPIServer(t, store)
	rr := doRequestWithHeaders(t, srv, http.MethodPost, "/api/cost-analysis/web/cloud-estimate", validInfracostJSON, asApp(teamAWeb.String()))

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, []models.AppRef{teamAWeb}, saved)
}

func TestAppScoped_FailsClosedWithoutRequireApp(t *testing.T) {
	t.Parallel()

	called := false
	h := appScoped(func(http.ResponseWriter, *http.Request, models.AppRef) { called = true })
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/cost-analysis/web/actual", http.NoBody)
	req.Header.Set(headerApplicationName, teamAWeb.String())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.False(t, called)
}
