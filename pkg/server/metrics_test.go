package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/telemetry"
)

// counterValues reads a counter's series from reg, keyed by the value of label.
func counterValues(t *testing.T, reg prometheus.Gatherer, name, label string) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	values := map[string]float64{}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == label {
					values[l.GetValue()] = m.GetCounter().GetValue()
				}
			}
		}
	}
	return values
}

func TestMetrics_CountWebhooksByResultAndTheMeasurementsThatFollow(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	srv := newTestAPIServer(t, &mockStorage{}, WithMetrics(telemetry.NewMetrics(reg)))

	require.Equal(t, http.StatusAccepted, doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", syncPayload(t)).Code)
	require.Equal(t, http.StatusUnauthorized, doRequestWithHeaders(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", syncPayload(t), nil).Code)
	require.Equal(t, http.StatusBadRequest, doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", []byte("not-json")).Code)
	remote, _ := json.Marshal(models.SyncNotification{ApplicationName: "redis", ApplicationNamespace: "argocd", SyncResult: "Succeeded", DestinationServer: "https://prod.example.com"})
	require.Equal(t, http.StatusOK, doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", remote).Code)
	require.Eventually(t, func() bool { return len(srv.syncSem) == 0 }, 5*time.Second, 10*time.Millisecond)
	for range maxConcurrentSyncs {
		srv.syncSem <- struct{}{}
	}
	require.Equal(t, http.StatusTooManyRequests, doRequest(t, srv, http.MethodPost, "/api/cost-analysis/on-sync", syncPayload(t)).Code)
	for range maxConcurrentSyncs {
		<-srv.syncSem
	}

	assert.Equal(t, map[string]float64{"accepted": 1, "unauthorized": 1, "invalid": 1, "overloaded": 1, "remote": 1},
		counterValues(t, reg, "argocd_cost_intelligence_sync_webhooks_total", "result"))
	measured := counterValues(t, reg, "argocd_cost_intelligence_sync_measurements_total", "result")
	assert.InDelta(t, 1, measured["success"]+measured["error"], 0, "one measurement, for the accepted webhook only")
}

func TestMetrics_TimeRequestsByRoute(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	srv := newTestAPIServer(t, &mockStorage{}, WithMetrics(telemetry.NewMetrics(reg)))

	doRequest(t, srv, http.MethodGet, "/api/cost-analysis/redis/actual", nil)
	doRequest(t, srv, http.MethodGet, "/health", nil)

	families, err := reg.Gather()
	require.NoError(t, err)
	routes := map[string]uint64{}
	for _, f := range families {
		if f.GetName() != "argocd_cost_intelligence_http_request_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "route" {
					routes[l.GetValue()] += m.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	assert.Equal(t, map[string]uint64{"/api/cost-analysis/{appName}/actual": 1, "/health": 1}, routes)
}
