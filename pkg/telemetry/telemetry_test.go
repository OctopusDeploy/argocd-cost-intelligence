package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics_StartsEveryResultAtZero(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	NewMetrics(reg)

	err := testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP argocd_cost_intelligence_sync_webhooks_total Sync notifications received from Argo CD, by result: accepted, unauthorized, invalid, overloaded or remote.
# TYPE argocd_cost_intelligence_sync_webhooks_total counter
argocd_cost_intelligence_sync_webhooks_total{result="accepted"} 0
argocd_cost_intelligence_sync_webhooks_total{result="invalid"} 0
argocd_cost_intelligence_sync_webhooks_total{result="overloaded"} 0
argocd_cost_intelligence_sync_webhooks_total{result="remote"} 0
argocd_cost_intelligence_sync_webhooks_total{result="unauthorized"} 0
# HELP argocd_cost_intelligence_sync_measurements_total Cost measurements made after accepted sync notifications, by result: success or error.
# TYPE argocd_cost_intelligence_sync_measurements_total counter
argocd_cost_intelligence_sync_measurements_total{result="error"} 0
argocd_cost_intelligence_sync_measurements_total{result="success"} 0
# HELP argocd_cost_intelligence_history_runs_total Runs of the daily cost history recorder, by result: success, or error when any application failed.
# TYPE argocd_cost_intelligence_history_runs_total counter
argocd_cost_intelligence_history_runs_total{result="error"} 0
argocd_cost_intelligence_history_runs_total{result="success"} 0
`), "argocd_cost_intelligence_sync_webhooks_total", "argocd_cost_intelligence_sync_measurements_total",
		"argocd_cost_intelligence_history_runs_total")
	require.NoError(t, err)

	names := gatheredNames(t, reg)
	assert.Contains(t, names, "go_goroutines")
	assert.Contains(t, names, "process_cpu_seconds_total")
}

func TestNewExporterMetrics_LeavesOutWhatOnlyTheAPIDoes(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewExporterMetrics(reg)
	m.ObserveDatabase("ListCloudEstimates", nil, time.Millisecond)

	names := gatheredNames(t, reg)
	assert.Contains(t, names, "argocd_cost_intelligence_database_operation_duration_seconds")
	assert.Contains(t, names, "go_goroutines")
	assert.NotContains(t, names, "argocd_cost_intelligence_sync_webhooks_total")
	assert.NotContains(t, names, "argocd_cost_intelligence_http_request_duration_seconds")
	assert.NotContains(t, names, "argocd_cost_intelligence_history_runs_total")
}

func TestObserve_CountsByResult(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.ObserveWebhook(WebhookAccepted)
	m.ObserveWebhook(WebhookAccepted)
	m.ObserveWebhook(WebhookUnauthorized)
	m.ObserveMeasurement(nil, 6*time.Second)
	m.ObserveMeasurement(errors.New("opencost down"), 5*time.Second)
	m.ObserveMeasurement(nil, 7*time.Second)
	m.ObserveDatabase("SaveActual", errors.New("connection refused"), time.Millisecond)
	m.ObserveHistoryRun(time.Unix(1_791_000_000, 0), nil)

	assert.InDelta(t, 2, testutil.ToFloat64(m.webhooks.WithLabelValues(WebhookAccepted)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.webhooks.WithLabelValues(WebhookUnauthorized)), 0)
	assert.InDelta(t, 2, testutil.ToFloat64(m.measurements.WithLabelValues("success")), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.measurements.WithLabelValues("error")), 0)
	assert.Equal(t, map[string]uint64{"": 3}, histogramCounts(t, reg, "argocd_cost_intelligence_sync_measurement_duration_seconds"))
	assert.Equal(t, 1, testutil.CollectAndCount(m.databaseOperations), "one series: SaveActual, error")
	assert.InDelta(t, 1_791_000_000, testutil.ToFloat64(m.historyLastRun), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.historyRuns.WithLabelValues("success")), 0)
}

func TestHTTPMiddleware_LabelsByRoutePatternNotPath(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware)
	r.Route("/api/cost-analysis/{appName}", func(r chi.Router) {
		r.Get("/actual", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	})
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Get("/ready", func(http.ResponseWriter, *http.Request) {}) // writes nothing: net/http answers 200

	for _, path := range []string{"/api/cost-analysis/web/actual", "/api/cost-analysis/redis/actual", "/health", "/ready", "/nowhere"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))
	}

	counts := histogramCounts(t, reg, "argocd_cost_intelligence_http_request_duration_seconds", "route", "method", "code")
	assert.Equal(t, map[string]uint64{
		"/api/cost-analysis/{appName}/actual GET 404": 2,
		"/health GET 200":   1,
		"/ready GET 200":    1,
		"unmatched GET 404": 1,
	}, counts)
}

func TestInstrumentRoundTripper_LabelsByPathBelowTheBaseURL(t *testing.T) {
	t.Parallel()

	opencost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/assets" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(opencost.Close)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	client := &http.Client{Transport: m.InstrumentRoundTripper("/model/", nil)}

	get := func(c *http.Client, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
		require.NoError(t, err)
		return c.Do(req)
	}
	for _, path := range []string{"/model/allocation?window=1h", "/model/allocation", "/model/assets"} {
		resp, err := get(client, opencost.URL+path)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	resp, err := get(&http.Client{Transport: m.InstrumentRoundTripper("/model", nil)}, "http://127.0.0.1:1/model/allocation")
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)

	assert.Equal(t, map[string]uint64{
		"/allocation 200":   2,
		"/assets 500":       1,
		"/allocation error": 1,
	}, histogramCounts(t, reg, "argocd_cost_intelligence_opencost_request_duration_seconds", "endpoint", "code"))
}

func TestNilMetrics_RecordNothing(t *testing.T) {
	t.Parallel()

	var m *Metrics
	m.ObserveWebhook(WebhookAccepted)
	m.ObserveMeasurement(nil, time.Second)
	m.ObserveDatabase("SaveActual", nil, time.Second)
	m.ObserveHistoryRun(time.Now(), nil)

	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	assert.NotNil(t, m.HTTPMiddleware(next))
	assert.Equal(t, http.DefaultTransport, m.InstrumentRoundTripper("", http.DefaultTransport))
}

func TestHandler_ServesTheRegistry(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	NewMetrics(reg).ObserveWebhook(WebhookInvalid)
	rec := httptest.NewRecorder()

	Handler(reg).ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `argocd_cost_intelligence_sync_webhooks_total{result="invalid"} 1`)
}

func gatheredNames(t *testing.T, g prometheus.Gatherer) []string {
	t.Helper()
	families, err := g.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(families))
	for _, f := range families {
		names = append(names, f.GetName())
	}
	return names
}

// histogramCounts maps each series of a histogram, by the values of labels joined with spaces, to its
// sample count.
func histogramCounts(t *testing.T, g prometheus.Gatherer, name string, labels ...string) map[string]uint64 {
	t.Helper()
	families, err := g.Gather()
	require.NoError(t, err)
	counts := map[string]uint64{}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, metric := range f.GetMetric() {
			byName := map[string]string{}
			for _, l := range metric.GetLabel() {
				byName[l.GetName()] = l.GetValue()
			}
			values := make([]string, 0, len(labels))
			for _, l := range labels {
				values = append(values, byName[l])
			}
			counts[strings.Join(values, " ")] = metric.GetHistogram().GetSampleCount()
		}
	}
	return counts
}
