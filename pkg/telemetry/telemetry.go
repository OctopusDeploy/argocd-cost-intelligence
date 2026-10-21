// Package telemetry holds the metrics every replica of the API reports about itself: HTTP requests,
// sync webhooks and measurements, OpenCost and database calls, and the daily history recorder. They
// describe one process, so each replica reports its own series and Prometheus tells them apart by
// target. Cost per application is not here: it is global state, exported once by the exporter
// (pkg/exporter).
//
// Every method is safe on a nil *Metrics, so code can be built and tested without a registry.
package telemetry

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Namespace prefixes every metric the service exports.
const Namespace = "argocd_cost_intelligence"

// Webhook results, for ObserveWebhook.
const (
	WebhookAccepted     = "accepted"
	WebhookUnauthorized = "unauthorized"
	WebhookInvalid      = "invalid"
	WebhookOverloaded   = "overloaded"
	WebhookRemote       = "remote"
	WebhookIgnored      = "ignored"
)

// latencyBuckets suit calls to OpenCost and PostgreSQL: a few milliseconds to the 10s timeout.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics are the API process's own metrics.
type Metrics struct {
	httpRequests        *prometheus.HistogramVec
	webhooks            *prometheus.CounterVec
	measurements        *prometheus.CounterVec
	measurementDuration prometheus.Histogram
	opencostRequests    *prometheus.HistogramVec
	databaseOperations  *prometheus.HistogramVec
	historyRuns         *prometheus.CounterVec
	historyLastRun      prometheus.Gauge
}

// NewMetrics registers the API's metrics, with Go runtime and process metrics, on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := newMetrics()
	reg.MustRegister(m.httpRequests, m.webhooks, m.measurements, m.measurementDuration, m.historyRuns, m.historyLastRun)
	m.registerShared(reg)
	// Every result appears from the start, at zero, so rates and alerts work before the first event.
	for _, result := range []string{WebhookAccepted, WebhookUnauthorized, WebhookInvalid, WebhookOverloaded, WebhookRemote, WebhookIgnored} {
		m.webhooks.WithLabelValues(result)
	}
	for _, result := range []string{"success", "error"} {
		m.measurements.WithLabelValues(result)
		m.historyRuns.WithLabelValues(result)
	}
	return m
}

// NewExporterMetrics registers the metrics the exporter shares with the API, its calls to OpenCost and
// PostgreSQL and Go runtime and process metrics, on reg. It serves no API, webhook or history.
func NewExporterMetrics(reg prometheus.Registerer) *Metrics {
	m := newMetrics()
	m.registerShared(reg)
	return m
}

func (m *Metrics) registerShared(reg prometheus.Registerer) {
	reg.MustRegister(
		m.opencostRequests, m.databaseOperations,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

func newMetrics() *Metrics {
	m := &Metrics{
		httpRequests: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests to the API, by route pattern, method and status code.",
			Buckets:   latencyBuckets,
		}, []string{"route", "method", "code"}),
		webhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "sync_webhooks_total",
			Help:      "Sync notifications received from Argo CD, by result: accepted, unauthorized, invalid, overloaded, remote or ignored.",
		}, []string{"result"}),
		measurements: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "sync_measurements_total",
			Help:      "Cost measurements made after accepted sync notifications, by result: success or error.",
		}, []string{"result"}),
		measurementDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "sync_measurement_duration_seconds",
			Help:      "Duration of a cost measurement after a sync, including the wait for metrics to settle.",
			Buckets:   []float64{5, 6, 7.5, 10, 15, 20, 30, 45, 60},
		}),
		opencostRequests: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "opencost_request_duration_seconds",
			Help:      "Duration of requests to OpenCost, by API path and status code (\"error\" when no response arrived).",
			Buckets:   latencyBuckets,
		}, []string{"endpoint", "code"}),
		databaseOperations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "database_operation_duration_seconds",
			Help:      "Duration of storage operations against PostgreSQL, by operation and result: success or error.",
			Buckets:   latencyBuckets,
		}, []string{"operation", "result"}),
		historyRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "history_runs_total",
			Help:      "Runs of the daily cost history recorder, by result: success, or error when any application failed.",
		}, []string{"result"}),
		historyLastRun: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "history_last_run_timestamp_seconds",
			Help:      "When the daily cost history recorder last finished a run, as a Unix timestamp.",
		}),
	}
	return m
}

// Handler serves the metrics gathered by g in the Prometheus or OpenMetrics text format.
func Handler(g prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// ObserveWebhook counts a sync notification by its result.
func (m *Metrics) ObserveWebhook(result string) {
	if m == nil {
		return
	}
	m.webhooks.WithLabelValues(result).Inc()
}

// ObserveMeasurement records a cost measurement made after a sync, with how long it took.
func (m *Metrics) ObserveMeasurement(err error, took time.Duration) {
	if m == nil {
		return
	}
	m.measurements.WithLabelValues(result(err)).Inc()
	m.measurementDuration.Observe(took.Seconds())
}

// ObserveDatabase records a storage operation. Its signature matches storage.Observer.
func (m *Metrics) ObserveDatabase(operation string, err error, took time.Duration) {
	if m == nil {
		return
	}
	m.databaseOperations.WithLabelValues(operation, result(err)).Observe(took.Seconds())
}

// ObserveHistoryRun records a finished run of the daily cost history recorder.
func (m *Metrics) ObserveHistoryRun(at time.Time, err error) {
	if m == nil {
		return
	}
	m.historyRuns.WithLabelValues(result(err)).Inc()
	m.historyLastRun.Set(float64(at.Unix()))
}

func result(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

// HTTPMiddleware times each request by the chi route it matched, so paths carrying an application or
// namespace name collapse into one series per route. A request that matched no route counts as
// "unmatched".
func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		route := "unmatched"
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if pattern := rctx.RoutePattern(); pattern != "" {
				route = pattern
			}
		}
		m.httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(status)).Observe(time.Since(start).Seconds())
	})
}

// InstrumentRoundTripper times each request to OpenCost by its path below basePath, the path part of
// OpenCost's base URL, so a base URL such as http://opencost/model reports /allocation, not
// /model/allocation.
func (m *Metrics) InstrumentRoundTripper(basePath string, next http.RoundTripper) http.RoundTripper {
	if m == nil {
		return next
	}
	if next == nil {
		next = http.DefaultTransport
	}
	basePath = strings.TrimSuffix(basePath, "/")
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		start := time.Now()
		resp, err := next.RoundTrip(req)
		code := "error"
		if err == nil {
			code = strconv.Itoa(resp.StatusCode)
		}
		endpoint := strings.TrimPrefix(req.URL.Path, basePath)
		m.opencostRequests.WithLabelValues(endpoint, code).Observe(time.Since(start).Seconds())
		return resp, err
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
