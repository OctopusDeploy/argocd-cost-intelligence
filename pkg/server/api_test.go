package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/hook"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/storage"
)

// mockStorage implements storage.CostStorage for testing.
type mockStorage struct {
	getLatestActualFn              func(ctx context.Context, app models.AppRef) (*models.CostActual, error)
	getActualsInDateRangeFn        func(ctx context.Context, app models.AppRef, start, end time.Time) ([]models.CostActual, error)
	getNamespaceCostSummaryFn      func(ctx context.Context, namespace string, days int) (float64, error)
	listNamespacesFn               func(ctx context.Context) ([]models.NamespaceSummary, error)
	getLatestActualsForNamespaceFn func(ctx context.Context, namespace string) ([]models.CostActual, error)
	getRecentActualsFn             func(ctx context.Context, app models.AppRef, limit int) ([]models.CostActual, error)
	saveCloudEstimateFn            func(ctx context.Context, app models.AppRef, estimate *models.CloudServiceEstimate) error
	getCloudEstimateFn             func(ctx context.Context, app models.AppRef) (*models.CloudServiceEstimate, error)
	getDailyCostsFn                func(ctx context.Context, app models.AppRef, scope, from, to string) ([]models.DailyCost, error)
	getDailyProgressFn             func(ctx context.Context, app models.AppRef) (*models.DailyProgress, error)
}

var _ storage.CostStorage = (*mockStorage)(nil)

func (m *mockStorage) InitializeSchema(_ context.Context) error                 { return nil }
func (m *mockStorage) SaveActual(_ context.Context, _ *models.CostActual) error { return nil }
func (m *mockStorage) SaveFirstActual(context.Context, *models.CostActual) (bool, error) {
	return true, nil
}
func (m *mockStorage) Lock(context.Context, string) (func(), error) { return func() {}, nil }
func (m *mockStorage) Close() error                                 { return nil }

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

func (m *mockStorage) ListNamespaces(ctx context.Context) ([]models.NamespaceSummary, error) {
	if m.listNamespacesFn != nil {
		return m.listNamespacesFn(ctx)
	}
	return nil, nil
}

func (m *mockStorage) GetLatestActualsForNamespace(ctx context.Context, namespace string) ([]models.CostActual, error) {
	if m.getLatestActualsForNamespaceFn != nil {
		return m.getLatestActualsForNamespaceFn(ctx, namespace)
	}
	return nil, nil
}

func (m *mockStorage) GetRecentActuals(ctx context.Context, app models.AppRef, limit int) ([]models.CostActual, error) {
	if m.getRecentActualsFn != nil {
		return m.getRecentActualsFn(ctx, app, limit)
	}
	return nil, nil
}

func (m *mockStorage) SaveCloudEstimate(ctx context.Context, app models.AppRef, estimate *models.CloudServiceEstimate) error {
	if m.saveCloudEstimateFn != nil {
		return m.saveCloudEstimateFn(ctx, app, estimate)
	}
	return nil
}

func (m *mockStorage) ListCloudEstimates(context.Context) ([]models.AppCloudEstimate, error) {
	return nil, nil
}

func (m *mockStorage) GetCloudEstimate(ctx context.Context, app models.AppRef) (*models.CloudServiceEstimate, error) {
	if m.getCloudEstimateFn != nil {
		return m.getCloudEstimateFn(ctx, app)
	}
	return nil, nil
}

func (m *mockStorage) ListLatestActuals(context.Context) ([]models.CostActual, error) {
	return nil, nil
}

func (m *mockStorage) SaveDailyCosts(context.Context, models.AppRef, string, string, []models.DailyCost) error {
	return nil
}

func (m *mockStorage) GetDailyCosts(ctx context.Context, app models.AppRef, scope, from, to string) ([]models.DailyCost, error) {
	if m.getDailyCostsFn != nil {
		return m.getDailyCostsFn(ctx, app, scope, from, to)
	}
	return nil, nil
}

func (m *mockStorage) GetDailyProgress(ctx context.Context, app models.AppRef) (*models.DailyProgress, error) {
	if m.getDailyProgressFn != nil {
		return m.getDailyProgressFn(ctx, app)
	}
	return nil, nil
}

func (m *mockStorage) SetDailyProgress(context.Context, models.AppRef, models.DailyProgress) error {
	return nil
}

func (m *mockStorage) PruneDailyCosts(context.Context, string) error { return nil }

func (m *mockStorage) DeleteMeasurements(context.Context, models.AppRef) (bool, error) {
	return false, nil
}

// newTestAPIServer wires a CostAPIServer backed by the given mock storage.
// A minimal OpenCost test server is used for the SyncHook so the /on-sync
// goroutine does not panic when it fires.
func newTestAPIServer(t *testing.T, store storage.CostStorage, opts ...Option) *CostAPIServer {
	t.Helper()

	// Minimal OpenCost server — used by the async goroutine in /on-sync only.
	ocSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation":
			_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
				Code: 200,
				Data: []map[string]opencost.AllocationData{
					{"ns": {CPUCost: 10.0, RAMCost: 5.0}},
				},
			})
		case "/allocation/summary":
			_ = json.NewEncoder(w).Encode(opencost.SummaryAllocationResponse{
				Code: 200,
				Data: opencost.SummaryAllocationResponseData{
					Sets: []opencost.SummaryAllocationSet{{
						Allocations: map[string]opencost.SummaryAllocationData{
							"ns": {CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.4, Efficiency: 0.55},
						},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ocSrv.Close)

	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	syncHook := hook.NewSyncHook(ocClient, store, 1*time.Millisecond, logger)

	opts = append([]Option{WithClusterScopeApp(testGateApp), WithWebhookSecret(testWebhookSecret)}, opts...)
	return NewCostAPIServer(syncHook, store, ocClient, logger, opts...)
}

// testWebhookSecret is the token newTestAPIServer requires on POST /on-sync.
const testWebhookSecret = "test-webhook-secret-0123456789ab"

func doRequestWithHeaders(t *testing.T, srv *CostAPIServer, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequestWithContext(context.Background(), method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequestWithContext(context.Background(), method, path, http.NoBody)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

// doRequest sends the request as an authorized caller would: through Argo CD's proxy naming the
// application in the path for app-scoped routes and the gate app otherwise, and, for the webhook,
// as Argo CD Notifications with the webhook token. Authorization itself is tested in auth_test.go.
func doRequest(t *testing.T, srv *CostAPIServer, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	headers := map[string]string{headerApplicationName: authorizedApp(path)}
	if path == "/api/cost-analysis/on-sync" {
		headers["Authorization"] = "Bearer " + testWebhookSecret
	}
	return doRequestWithHeaders(t, srv, method, path, body, headers)
}

func authorizedApp(path string) string {
	rest, _ := strings.CutPrefix(path, "/api/cost-analysis/")
	segment, _, _ := strings.Cut(rest, "/")
	segment, _, _ = strings.Cut(segment, "?")
	switch segment {
	case "on-sync", "config", "data-quality", "namespaces", "namespace", "chargeback", "cluster":
		return testGateApp.String()
	}
	return "argocd:" + segment
}

// ---- /health ----

func TestHandleHealth(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodGet, "/health", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

// ---- /ready ----

func TestHandleReady(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodGet, "/ready", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "ready", body["status"])
}

// ---- POST /api/cost-analysis/on-sync ----

func TestHandleOnSync_ValidBody_Returns202(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	payload, _ := json.Marshal(models.SyncNotification{
		ApplicationName:      "redis",
		ApplicationNamespace: "argocd",
		Namespace:            "argocd",
		SyncResult:           "Failed", // avoids OpenCost calls in the goroutine
		SyncTimestamp:        time.Now(),
	})
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", payload)

	assert.Equal(t, http.StatusAccepted, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["message"], "accepted")
}

func TestHandleOnSync_OtherCluster_Returns200(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	payload, _ := json.Marshal(models.SyncNotification{
		ApplicationName:      "redis",
		ApplicationNamespace: "argocd",
		Namespace:            "argocd",
		SyncResult:           "Succeeded",
		SyncTimestamp:        time.Now(),
		DestinationName:      "prod",
	})
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", payload)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "another cluster")
}

func TestHandleOnSync_MissingApplicationNamespace_Returns400(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	payload, _ := json.Marshal(models.SyncNotification{
		ApplicationName: "redis",
		Namespace:       "cache",
		SyncResult:      "Succeeded",
		SyncTimestamp:   time.Now(),
	})
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", payload)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "application namespace is required")
}

func TestHandleOnSync_InvalidBody_Returns400(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", []byte("not-json"))

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "invalid request body")
}

// ---- GET /api/cost-analysis/{appName}/actual ----

func TestHandleGetActualCost_Found(t *testing.T) {
	t.Parallel()

	want := &models.CostActual{
		ID:               "redis-1",
		ApplicationName:  "redis",
		TotalMonthlyCost: 450.0,
		Efficiency: &models.EfficiencyMetrics{
			CPUEfficiency:   0.4,
			RAMEfficiency:   0.7,
			TotalEfficiency: 0.55,
		},
	}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, app models.AppRef) (*models.CostActual, error) {
			assert.Equal(t, models.AppRef{Namespace: "argocd", Name: "redis"}, app)
			return want, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/actual", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "redis", got.ApplicationName)
	assert.InDelta(t, 450.0, got.TotalMonthlyCost, 1e-9)
	require.NotNil(t, got.Efficiency)
	assert.InDelta(t, 0.4, got.Efficiency.CPUEfficiency, 1e-9)
}

func TestHandleGetActualCost_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return nil, errors.New("no cost actual found")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/unknown/actual", nil)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "unknown")
}

func TestHandleGetActualCost_AnomalyDetected_Warning(t *testing.T) {
	t.Parallel()

	current := &models.CostActual{ID: "app-10", ApplicationName: "payments", TotalMonthlyCost: 200.0}
	history := []models.CostActual{
		{ID: "app-10", TotalMonthlyCost: 200.0}, // index 0 — same as current, skipped
		{ID: "app-9", TotalMonthlyCost: 100.0},
		{ID: "app-8", TotalMonthlyCost: 100.0},
		{ID: "app-7", TotalMonthlyCost: 100.0},
	}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return current, nil
		},
		getRecentActualsFn: func(_ context.Context, _ models.AppRef, _ int) ([]models.CostActual, error) {
			return history, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/payments/actual", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.NotNil(t, got.Anomaly)
	assert.Equal(t, "warning", got.Anomaly.Severity)
	assert.InDelta(t, 100.0, got.Anomaly.RollingAvg, 1e-9)
	assert.Equal(t, 3, got.Anomaly.RecordCount)
	assert.Contains(t, got.Anomaly.Message, "100%")
}

func TestHandleGetActualCost_AnomalyDetected_Alert(t *testing.T) {
	t.Parallel()

	// current cost is 3× the average → score=2.0 → alert
	current := &models.CostActual{ID: "app-10", ApplicationName: "payments", TotalMonthlyCost: 300.0}
	history := []models.CostActual{
		{ID: "app-10", TotalMonthlyCost: 300.0},
		{ID: "app-9", TotalMonthlyCost: 100.0},
		{ID: "app-8", TotalMonthlyCost: 100.0},
		{ID: "app-7", TotalMonthlyCost: 100.0},
	}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return current, nil
		},
		getRecentActualsFn: func(_ context.Context, _ models.AppRef, _ int) ([]models.CostActual, error) {
			return history, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/payments/actual", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.NotNil(t, got.Anomaly)
	assert.Equal(t, "alert", got.Anomaly.Severity)
}

func TestHandleGetActualCost_NoAnomaly_CostBelowThreshold(t *testing.T) {
	t.Parallel()

	// current cost is only 10% above average — below warning threshold
	current := &models.CostActual{ID: "app-10", ApplicationName: "payments", TotalMonthlyCost: 110.0}
	history := []models.CostActual{
		{ID: "app-10", TotalMonthlyCost: 110.0},
		{ID: "app-9", TotalMonthlyCost: 100.0},
		{ID: "app-8", TotalMonthlyCost: 100.0},
		{ID: "app-7", TotalMonthlyCost: 100.0},
	}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return current, nil
		},
		getRecentActualsFn: func(_ context.Context, _ models.AppRef, _ int) ([]models.CostActual, error) {
			return history, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/payments/actual", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Nil(t, got.Anomaly)
}

func TestHandleGetActualCost_NoAnomaly_InsufficientHistory(t *testing.T) {
	t.Parallel()

	// Only 2 historical records — below the minimum of 3
	current := &models.CostActual{ID: "app-3", ApplicationName: "payments", TotalMonthlyCost: 500.0}
	history := []models.CostActual{
		{ID: "app-3", TotalMonthlyCost: 500.0},
		{ID: "app-2", TotalMonthlyCost: 100.0},
		{ID: "app-1", TotalMonthlyCost: 100.0},
	}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return current, nil
		},
		getRecentActualsFn: func(_ context.Context, _ models.AppRef, _ int) ([]models.CostActual, error) {
			return history, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/payments/actual", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Nil(t, got.Anomaly)
}

func TestHandleGetActualCost_GetRecentActualsError_StillReturnsActual(t *testing.T) {
	t.Parallel()

	current := &models.CostActual{ID: "app-1", ApplicationName: "payments", TotalMonthlyCost: 100.0}
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return current, nil
		},
		getRecentActualsFn: func(_ context.Context, _ models.AppRef, _ int) ([]models.CostActual, error) {
			return nil, errors.New("db connection timeout")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/payments/actual", nil)

	// Still 200 — anomaly failure is non-fatal
	assert.Equal(t, http.StatusOK, rr.Code)
	var got models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "payments", got.ApplicationName)
	assert.Nil(t, got.Anomaly)
}

// ---- computeAnomaly unit tests ----

func TestComputeAnomaly_NilWhenInsufficientHistory(t *testing.T) {
	t.Parallel()

	history := []models.CostActual{
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
	}
	assert.Nil(t, computeAnomaly(500, history))
}

func TestComputeAnomaly_NilWhenZeroAverage(t *testing.T) {
	t.Parallel()

	history := []models.CostActual{
		{TotalMonthlyCost: 0},
		{TotalMonthlyCost: 0},
		{TotalMonthlyCost: 0},
	}
	assert.Nil(t, computeAnomaly(100, history))
}

func TestComputeAnomaly_NilWhenBelowThreshold(t *testing.T) {
	t.Parallel()

	history := []models.CostActual{
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
	}
	// 20% above average — under the 30% warning threshold
	assert.Nil(t, computeAnomaly(120, history))
}

func TestComputeAnomaly_WarningSeverity(t *testing.T) {
	t.Parallel()

	history := []models.CostActual{
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
	}
	// 50% above average → warning (>30%, ≤100%)
	got := computeAnomaly(150, history)
	require.NotNil(t, got)
	assert.Equal(t, "warning", got.Severity)
	assert.InDelta(t, 0.5, got.Score, 1e-9)
	assert.InDelta(t, 100.0, got.RollingAvg, 1e-9)
	assert.Equal(t, 3, got.RecordCount)
	assert.Contains(t, got.Message, "50%")
}

func TestComputeAnomaly_AlertSeverity(t *testing.T) {
	t.Parallel()

	history := []models.CostActual{
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
		{TotalMonthlyCost: 100},
	}
	// 200% above average → alert (>100%)
	got := computeAnomaly(300, history)
	require.NotNil(t, got)
	assert.Equal(t, "alert", got.Severity)
	assert.InDelta(t, 2.0, got.Score, 1e-9)
}

// ---- GET /api/cost-analysis/{appName}/trend ----

func TestHandleGetCostTrend_Success_CalculatesStats(t *testing.T) {
	t.Parallel()

	records := []models.CostActual{
		{ApplicationName: "redis", TotalMonthlyCost: 100.0},
		{ApplicationName: "redis", TotalMonthlyCost: 200.0},
		{ApplicationName: "redis", TotalMonthlyCost: 150.0},
	}
	store := &mockStorage{
		getActualsInDateRangeFn: func(_ context.Context, _ models.AppRef, _, _ time.Time) ([]models.CostActual, error) {
			return records, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/trend?days=30", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))

	stats := body["statistics"].(map[string]any)
	assert.InDelta(t, 150.0, stats["average"].(float64), 1e-9)
	assert.InDelta(t, 200.0, stats["maximum"].(float64), 1e-9)
	assert.InDelta(t, 100.0, stats["minimum"].(float64), 1e-9)
	assert.InDelta(t, 450.0, stats["total"].(float64), 1e-9)
	assert.InDelta(t, float64(3), body["count"].(float64), 1e-9)
	assert.InDelta(t, float64(30), body["days"].(float64), 1e-9)
}

func TestHandleGetCostTrend_EmptyResult(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getActualsInDateRangeFn: func(_ context.Context, _ models.AppRef, _, _ time.Time) ([]models.CostActual, error) {
			return []models.CostActual{}, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/trend", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.InDelta(t, float64(0), body["count"].(float64), 1e-9)

	stats := body["statistics"].(map[string]any)
	assert.InDelta(t, float64(0), stats["minimum"].(float64), 1e-9, "no records must not report a sentinel minimum")
}

func TestHandleGetCostTrend_DefaultDays(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getActualsInDateRangeFn: func(_ context.Context, _ models.AppRef, _, _ time.Time) ([]models.CostActual, error) {
			return nil, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/trend", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.InDelta(t, float64(30), body["days"].(float64), 1e-9)
}

func TestHandleGetCostTrend_StorageError_Returns500(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getActualsInDateRangeFn: func(_ context.Context, _ models.AppRef, _, _ time.Time) ([]models.CostActual, error) {
			return nil, errors.New("db error")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/trend", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---- GET /api/cost-analysis/namespace/{namespace}/summary ----

func TestHandleGetNamespaceSummary_Success(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getNamespaceCostSummaryFn: func(_ context.Context, namespace string, days int) (float64, error) {
			assert.Equal(t, "argocd", namespace)
			assert.Equal(t, 30, days)
			return 1234.56, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/argocd/summary?days=30", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "argocd", body["namespace"])
	assert.InDelta(t, 1234.56, body["averageCost"].(float64), 1e-9)
	assert.InDelta(t, float64(30), body["days"].(float64), 1e-9)
}

func TestHandleGetNamespaceSummary_StorageError_Returns500(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getNamespaceCostSummaryFn: func(_ context.Context, _ string, _ int) (float64, error) {
			return 0, errors.New("db error")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/argocd/summary", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---- Authentication behavior ----

func syncPayload(t *testing.T) []byte {
	t.Helper()
	b, _ := json.Marshal(models.SyncNotification{
		ApplicationName:      "redis",
		ApplicationNamespace: "argocd",
		Namespace:            "argocd",
		SyncResult:           "Failed",
		SyncTimestamp:        time.Now(),
	})
	return b
}

// POST /on-sync auth: the webhook is not proxied, so the shared WEBHOOK_SECRET authenticates it.

func TestHandleOnSync_Token(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authorization string
		want          int
	}{
		{"the configured token", "Bearer " + testWebhookSecret, http.StatusAccepted},
		{"the scheme in lower case", "bearer " + testWebhookSecret, http.StatusAccepted},
		{"no Authorization header", "", http.StatusUnauthorized},
		{"a wrong token", "Bearer wrong-secret", http.StatusUnauthorized},
		{"the token with a suffix", "Bearer " + testWebhookSecret + "x", http.StatusUnauthorized},
		{"the token without a scheme", testWebhookSecret, http.StatusUnauthorized},
		{"the token as Basic credentials", "Basic " + testWebhookSecret, http.StatusUnauthorized},
		{"an empty bearer token", "Bearer ", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := newTestAPIServer(t, &mockStorage{})
			headers := map[string]string{}
			if tt.authorization != "" {
				headers["Authorization"] = tt.authorization
			}
			rr := doRequestWithHeaders(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", syncPayload(t), headers)

			assert.Equal(t, tt.want, rr.Code)
			if tt.want == http.StatusUnauthorized {
				assert.Equal(t, "Bearer", rr.Header().Get("WWW-Authenticate"))
				assert.Contains(t, rr.Body.String(), "missing or invalid webhook token")
			}
		})
	}
}

func TestHandleOnSync_NoSecretConfigured_RefusesEveryRequest(t *testing.T) {
	t.Parallel()

	logger := log.New(io.Discard, "", 0)
	srv := NewCostAPIServer(hook.NewSyncHook(nil, &mockStorage{}, time.Millisecond, logger), &mockStorage{}, nil, logger)
	for _, authorization := range []string{"", "Bearer ", "Bearer anything"} {
		rr := doRequestWithHeaders(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", syncPayload(t),
			map[string]string{"Authorization": authorization})

		assert.Equal(t, http.StatusUnauthorized, rr.Code, "Authorization %q", authorization)
	}
}

// The token is checked before the body is read, and a refused webhook takes no measurement slot.
func TestHandleOnSync_WithoutToken_DoesNotReadBodyOrTakeASlot(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequestWithHeaders(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", []byte("not-json"), nil)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, srv.syncSem)
}

// Health/ready probes bypass auth so Kubernetes can always reach them.

func TestHandleHealth_SecretRequired_NoHeader_Still200(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodGet, "/health", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHandleReady_SecretRequired_NoHeader_Still200(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodGet, "/ready", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// ---- Rate limiting / goroutine cap / body size limit ----

func TestHandleOnSync_SemaphoreFull_Returns429(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	// Fill the semaphore so the next request cannot acquire a slot.
	for range maxConcurrentSyncs {
		srv.syncSem <- struct{}{}
	}
	// Release at cleanup so the server is not left in a broken state.
	t.Cleanup(func() {
		for range maxConcurrentSyncs {
			<-srv.syncSem
		}
	})

	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", syncPayload(t))

	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "too many")
}

func TestHandleOnSync_OversizedBody_Returns400(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	oversized := make([]byte, maxBodyBytes+1)
	for i := range oversized {
		oversized[i] = 'a'
	}

	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", oversized)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleGetCostTrend_ExcessiveDays_Returns400(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/trend?days=99999", nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "days exceeds maximum")
}

func TestHandleGetNamespaceSummary_InvalidNamespace_Returns400(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getNamespaceCostSummaryFn: func(_ context.Context, _ string, _ int) (float64, error) {
			t.Error("storage must not be queried for an invalid namespace")
			return 0, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/Not_A_Namespace/summary", nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleGetNamespaceSummary_ExcessiveDays_Returns400(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/argocd/summary?days=99999", nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "days exceeds maximum")
}

func TestHandleGetDailyCosts_ExcessiveDays_Returns400(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return &models.CostActual{DeploymentNamespace: "argocd"}, nil
		},
	}
	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/daily?days=99999", nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "days exceeds maximum")
}

// ---- Namespace validation (filter injection prevention) ----

func TestHandleGetDailyCosts_InvalidNamespaceFromDB_Returns400(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return &models.CostActual{DeploymentNamespace: `bad"namespace`}, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/daily", nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "invalid namespace")
}

// ---- GET /api/cost-analysis/namespaces ----

func TestHandleListNamespaces_Success(t *testing.T) {
	t.Parallel()

	namespaces := []models.NamespaceSummary{
		{Namespace: "production", TotalCost: 1500.0, IdleCost: 320.5, AppCount: 3},
		{Namespace: "staging", TotalCost: 500.0, AppCount: 2},
	}
	store := &mockStorage{
		listNamespacesFn: func(_ context.Context) ([]models.NamespaceSummary, error) {
			return namespaces, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespaces", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	nsList := body["namespaces"].([]any)
	assert.Len(t, nsList, 2)
	first := nsList[0].(map[string]any)
	assert.Equal(t, "production", first["namespace"])
	assert.InDelta(t, 1500.0, first["totalCost"].(float64), 1e-9)
	assert.InDelta(t, 320.5, first["idleCost"].(float64), 1e-9)
	assert.InDelta(t, 3.0, first["appCount"].(float64), 1e-9)
}

func TestHandleListNamespaces_Empty(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		listNamespacesFn: func(_ context.Context) ([]models.NamespaceSummary, error) {
			return []models.NamespaceSummary{}, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespaces", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	nsList := body["namespaces"].([]any)
	assert.Empty(t, nsList)
}

func TestHandleListNamespaces_StorageError_Returns500(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		listNamespacesFn: func(_ context.Context) ([]models.NamespaceSummary, error) {
			return nil, errors.New("db error")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespaces", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "failed to list namespaces")
}

// ---- GET /api/cost-analysis/namespace/{ns}/apps ----

func TestHandleGetNamespaceApps_Success(t *testing.T) {
	t.Parallel()

	apps := []models.CostActual{
		{ApplicationName: "redis", TotalMonthlyCost: 450.0, DeploymentNamespace: "production"},
		{ApplicationName: "nginx", TotalMonthlyCost: 200.0, DeploymentNamespace: "production"},
	}
	store := &mockStorage{
		getLatestActualsForNamespaceFn: func(_ context.Context, namespace string) ([]models.CostActual, error) {
			assert.Equal(t, "production", namespace)
			return apps, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/production/apps", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	appsList := body["apps"].([]any)
	assert.Len(t, appsList, 2)
	first := appsList[0].(map[string]any)
	assert.Equal(t, "redis", first["applicationName"])
}

func TestHandleGetNamespaceApps_Empty_Returns404(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualsForNamespaceFn: func(_ context.Context, _ string) ([]models.CostActual, error) {
			return []models.CostActual{}, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/unknown/apps", nil)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "unknown")
}

func TestHandleGetNamespaceApps_StorageError_Returns500(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualsForNamespaceFn: func(_ context.Context, _ string) ([]models.CostActual, error) {
			return nil, errors.New("db error")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/production/apps", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "failed to retrieve namespace apps")
}

// ---- GET /api/cost-analysis/namespace/{ns}/orphaned-pods ----

// newOrphanedPodsTestServer creates a CostAPIServer backed by an OpenCost mock
// that returns one running pod and one orphaned pod (zero CPU/RAM usage, non-zero PV cost).
func newOrphanedPodsTestServer(t *testing.T) *CostAPIServer {
	t.Helper()

	ocSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/allocation":
			_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{
				Code: 200,
				Data: []map[string]opencost.AllocationData{
					{
						"running-pod": {
							CPUCost: 10.0, RAMCost: 5.0, TotalCost: 15.0,
							CPUCoreUsageAverage: 0.5, RAMByteUsageAverage: 536870912,
							Properties: opencost.AllocationProperties{
								Pod: "running-pod", Namespace: "default",
								Controller: "web", ControllerKind: "Deployment",
							},
						},
						"evicted-pod-xk2p9": {
							PVCost: 0.14, TotalCost: 0.14,
							Properties: opencost.AllocationProperties{
								Pod: "evicted-pod-xk2p9", Namespace: "default",
								Controller: "batch-job", ControllerKind: "Job",
							},
						},
					},
				},
			})
		case "/allocation/summary":
			_ = json.NewEncoder(w).Encode(opencost.SummaryAllocationResponse{
				Code: 200,
				Data: opencost.SummaryAllocationResponseData{
					Sets: []opencost.SummaryAllocationSet{{
						Allocations: map[string]opencost.SummaryAllocationData{
							"default": {CPUCoreRequestAverage: 1, CPUCoreUsageAverage: 0.4, Efficiency: 0.55},
						},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ocSrv.Close)

	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	syncHook := hook.NewSyncHook(ocClient, &mockStorage{}, 1*time.Millisecond, logger)
	return NewCostAPIServer(syncHook, &mockStorage{}, ocClient, logger, WithClusterScopeApp(testGateApp))
}

func TestHandleGetOrphanedPods_ReturnsOrphanedPods(t *testing.T) {
	t.Parallel()

	srv := newOrphanedPodsTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/default/orphaned-pods", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))

	pods := body["orphanedPods"].([]any)
	assert.Len(t, pods, 1)
	pod := pods[0].(map[string]any)
	assert.Equal(t, "evicted-pod-xk2p9", pod["name"])
	assert.Equal(t, "Job", pod["controllerKind"])

	totalWasted := body["totalWastedCost"].(float64)
	assert.InDelta(t, 4.2, totalWasted, 0.01)
}

func TestHandleGetOrphanedPods_InvalidNamespace_Returns400(t *testing.T) {
	t.Parallel()

	srv := newOrphanedPodsTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, `/api/cost-analysis/namespace/INVALID!"/orphaned-pods`, nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "invalid namespace")
}

func TestHandleGetOrphanedPods_EmptyNamespace_ReturnsEmptyList(t *testing.T) {
	t.Parallel()

	// OpenCost returns no pods for an empty namespace
	ocSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{}})
	}))
	t.Cleanup(ocSrv.Close)

	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	syncHook := hook.NewSyncHook(ocClient, &mockStorage{}, 1*time.Millisecond, logger)
	srv := NewCostAPIServer(syncHook, &mockStorage{}, ocClient, logger, WithClusterScopeApp(testGateApp))

	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/namespace/empty/orphaned-pods", nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	pods := body["orphanedPods"]
	assert.Nil(t, pods)
	assert.InDelta(t, 0.0, body["totalWastedCost"].(float64), 1e-9)
}

// ---- GET /api/cost-analysis/chargeback/labels ----

// newLabelKeysTestServer creates a CostAPIServer whose OpenCost mock answers /allocation/autocomplete
// with handler.
func newLabelKeysTestServer(t *testing.T, handler http.HandlerFunc) *CostAPIServer {
	t.Helper()

	ocSrv := httptest.NewServer(handler)
	t.Cleanup(ocSrv.Close)

	ocClient := opencost.NewClient(ocSrv.URL, 5*time.Second, nil)
	logger := log.New(os.Stdout, "", 0)
	syncHook := hook.NewSyncHook(ocClient, &mockStorage{}, 1*time.Millisecond, logger)
	return NewCostAPIServer(syncHook, &mockStorage{}, ocClient, logger, WithClusterScopeApp(testGateApp))
}

func TestHandleGetChargebackLabels_ReturnsLabelKeys(t *testing.T) {
	t.Parallel()

	var gotFilter string
	srv := newLabelKeysTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"data":["team","kubernetes_io_hostname","app"]}}`))
	})

	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/chargeback/labels?namespace=payments", nil)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Namespace string   `json:"namespace"`
		LabelKeys []string `json:"labelKeys"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "payments", body.Namespace)
	assert.Equal(t, []string{"app", "team"}, body.LabelKeys)
	assert.Equal(t, `namespace:"payments"`, gotFilter)
}

func TestHandleGetChargebackLabels_InvalidNamespace_Returns400(t *testing.T) {
	t.Parallel()

	called := false
	srv := newLabelKeysTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := doRequest(t, srv, http.MethodGet, `/api/cost-analysis/chargeback/labels?namespace=x"+namespace:"y`, nil)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, called, "an invalid namespace must never reach OpenCost's filter")
}

func TestHandleGetChargebackLabels_OpenCostError_Returns500(t *testing.T) {
	t.Parallel()

	srv := newLabelKeysTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "404 page not found", http.StatusNotFound)
	})

	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/chargeback/labels", nil)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "failed to retrieve label keys", body["error"])
}

// ---- POST /{appName}/cloud-estimate ----

var validInfracostJSON = []byte(`{
	"currency": "USD",
	"totalMonthlyCost": "115.00",
	"projects": [{
		"breakdown": {
			"totalMonthlyCost": "115.00",
			"resources": [
				{"name": "aws_db_instance.main", "resourceType": "aws_db_instance", "monthlyCost": "72.00"},
				{"name": "aws_elasticache_cluster.cache", "resourceType": "aws_elasticache_cluster", "monthlyCost": "38.00"},
				{"name": "aws_s3_bucket.assets", "resourceType": "aws_s3_bucket", "monthlyCost": "5.00"}
			]
		}
	}]
}`)

func TestHandlePostCloudEstimate_DefaultSource_Saves(t *testing.T) {
	t.Parallel()

	var savedApp models.AppRef
	var savedEstimate *models.CloudServiceEstimate

	store := &mockStorage{
		saveCloudEstimateFn: func(_ context.Context, app models.AppRef, est *models.CloudServiceEstimate) error {
			savedApp = app
			savedEstimate = est
			return nil
		},
		getCloudEstimateFn: func(_ context.Context, _ models.AppRef) (*models.CloudServiceEstimate, error) {
			return nil, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/my-app/cloud-estimate", validInfracostJSON)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, models.AppRef{Namespace: "argocd", Name: "my-app"}, savedApp)
	require.NotNil(t, savedEstimate)
	assert.Equal(t, "infracost", savedEstimate.Source)
	assert.InDelta(t, 115.00, savedEstimate.TotalMonthlyCost, 1e-9)
	assert.Len(t, savedEstimate.Resources, 3)
}

func TestHandlePostCloudEstimate_ExplicitSource_Saves(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		saveCloudEstimateFn: func(_ context.Context, _ models.AppRef, _ *models.CloudServiceEstimate) error {
			return nil
		},
		getCloudEstimateFn: func(_ context.Context, _ models.AppRef) (*models.CloudServiceEstimate, error) {
			return nil, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/my-app/cloud-estimate?source=infracost", validInfracostJSON)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHandlePostCloudEstimate_InvalidSource_ReturnsBadRequest(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/my-app/cloud-estimate?source=unknown-tool", validInfracostJSON)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "unsupported source")
}

func TestHandlePostCloudEstimate_InvalidJSON_ReturnsBadRequest(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, &mockStorage{})
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/my-app/cloud-estimate", []byte("not json at all"))

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "invalid infracost JSON")
}

func TestHandlePostCloudEstimate_StorageError_Returns500(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		saveCloudEstimateFn: func(_ context.Context, _ models.AppRef, _ *models.CloudServiceEstimate) error {
			return errors.New("db connection lost")
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodPost, "/api/cost-analysis/my-app/cloud-estimate", validInfracostJSON)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestHandleGetActualCost_WithCloudEstimate_AttachesEstimate(t *testing.T) {
	t.Parallel()

	pushedAt := time.Now().Add(-1 * time.Hour)
	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return &models.CostActual{
				ID:               "a1",
				ApplicationName:  "my-app",
				TotalMonthlyCost: 42.0,
				MeasuredAt:       time.Now(),
			}, nil
		},
		getCloudEstimateFn: func(_ context.Context, _ models.AppRef) (*models.CloudServiceEstimate, error) {
			return &models.CloudServiceEstimate{
				Source:           "infracost",
				TotalMonthlyCost: 115.0,
				Currency:         "USD",
				Resources: []models.CloudServiceResource{
					{Name: "aws_db_instance.main", ResourceType: "aws_db_instance", MonthlyCost: 115.0},
				},
				PushedAt: pushedAt,
			}, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/my-app/actual", nil)

	require.Equal(t, http.StatusOK, rr.Code)
	var body models.CostActual
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.NotNil(t, body.CloudEstimate)
	assert.Equal(t, "infracost", body.CloudEstimate.Source)
	assert.InDelta(t, 115.0, body.CloudEstimate.TotalMonthlyCost, 1e-9)
	assert.Len(t, body.CloudEstimate.Resources, 1)
}

func TestHandleGetActualCost_NoCloudEstimate_OmitsField(t *testing.T) {
	t.Parallel()

	store := &mockStorage{
		getLatestActualFn: func(_ context.Context, _ models.AppRef) (*models.CostActual, error) {
			return &models.CostActual{
				ID:               "a1",
				ApplicationName:  "my-app",
				TotalMonthlyCost: 42.0,
				MeasuredAt:       time.Now(),
			}, nil
		},
		getCloudEstimateFn: func(_ context.Context, _ models.AppRef) (*models.CloudServiceEstimate, error) {
			return nil, nil
		},
	}

	srv := newTestAPIServer(t, store)
	rr := doRequest(t, srv, http.MethodGet, "/api/cost-analysis/my-app/actual", nil)

	require.Equal(t, http.StatusOK, rr.Code)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &raw))
	_, hasField := raw["cloudEstimate"]
	assert.False(t, hasField, "cloudEstimate should be omitted when nil")
}

func TestDailyRange_RejectsInvalidRanges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		query   string
		wantErr string
	}{
		{"from=2026-09-01", "from and to must be given together"},
		{"to=2026-09-01", "from and to must be given together"},
		{"from=2026-9-1&to=2026-09-30", "from and to must be dates in the form YYYY-MM-DD"},
		{"from=2026-09-30&to=2026-09-01", "from must not be after to"},
		{"from=2026-10-04&to=2026-10-31", "from must not be in the future"},
		{"from=2025-10-02&to=2026-10-02", "the range exceeds the maximum of 365 days"},
		{"days=366", "days exceeds maximum of 365"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			query, err := url.ParseQuery(tt.query)
			require.NoError(t, err)
			_, _, _, msg := dailyRange(query, now)
			assert.Equal(t, tt.wantErr, msg)
		})
	}

	// A range may run past today: the series then ends with today, partly measured.
	query, _ := url.ParseQuery("from=2026-10-01&to=2026-10-31")
	_, _, days, msg := dailyRange(query, now)
	assert.Empty(t, msg)
	assert.Equal(t, 31, days)
	query, _ = url.ParseQuery("from=2025-10-03&to=2026-10-02")
	_, _, days, msg = dailyRange(query, now)
	assert.Empty(t, msg)
	assert.Equal(t, 365, days)

	// The last N days end today.
	query, _ = url.ParseQuery("days=7")
	from, to, days, msg := dailyRange(query, now)
	assert.Empty(t, msg)
	assert.Equal(t, 7, days)
	assert.Equal(t, "2026-09-27", from.Format(time.DateOnly))
	assert.Equal(t, "2026-10-03", to.Format(time.DateOnly))
}
