package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// measuredApp is an application's latest record with its own measurement: cpuIdle and ramIdle are
// its pods' idle cost, and the namespace's figures are deliberately larger.
func measuredApp(namespace, name string, cpuIdle, ramIdle float64) models.CostActual {
	return models.CostActual{
		ApplicationNamespace: namespace,
		ApplicationName:      name,
		DeploymentNamespace:  "shop",
		MeasuredAt:           time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		TotalMonthlyCost:     5000,
		Efficiency:           &models.EfficiencyMetrics{CPUIdleCost: 900, RAMIdleCost: 900},
		App: &models.AppMeasurement{
			TotalMonthlyCost: 400,
			OpenCostOutput:   models.OpenCostMetrics{AllocationTotals: models.AllocationTotal{CPUCost: 250}},
			Efficiency: &models.EfficiencyMetrics{
				CPUEfficiency: 0.3, CPUCoresRequested: 2, CPUCoresUsed: 0.6,
				CPUIdleCost: cpuIdle, RAMIdleCost: ramIdle,
			},
		},
	}
}

func TestRankWaste_RanksApplicationsByTheirOwnIdleCost(t *testing.T) {
	t.Parallel()

	ranking := rankWaste([]models.CostActual{
		measuredApp("team-a", "small", 10, 5),
		measuredApp("team-a", "big", 150, 30),
		measuredApp("team-b", "middle", 40, 20),
	}, nil)

	require.Len(t, ranking.Apps, 3)
	assert.Equal(t, []string{"big", "middle", "small"},
		[]string{ranking.Apps[0].ApplicationName, ranking.Apps[1].ApplicationName, ranking.Apps[2].ApplicationName})
	top := ranking.Apps[0]
	assert.InDelta(t, 180, top.IdleCost, 1e-9, "the application's idle, never its namespace's")
	assert.InDelta(t, 400, top.MonthlyCost, 1e-9)
	assert.InDelta(t, 250, top.CPUCost, 1e-9)
	assert.InDelta(t, 2, top.Efficiency.CPUCoresRequested, 1e-9)
	assert.Equal(t, "shop", top.DeploymentNamespace)
	assert.InDelta(t, 255, ranking.IdleCost, 1e-9, "the total is the sum of the ranked applications")
}

func TestRankWaste_IdentifiesApplicationsByNamespaceAndName(t *testing.T) {
	t.Parallel()

	ranking := rankWaste([]models.CostActual{
		measuredApp("team-a", "web", 10, 0),
		measuredApp("team-b", "web", 10, 0),
	}, map[models.AppRef]bool{{Namespace: "team-b", Name: "web"}: true})

	require.Len(t, ranking.Apps, 1)
	assert.Equal(t, "team-b", ranking.Apps[0].ApplicationNamespace)
}

func TestRankWaste_CountsButNeverRanksNamespaceOnlyRecords(t *testing.T) {
	t.Parallel()

	namespaceOnly := measuredApp("team-a", "legacy", 0, 0)
	namespaceOnly.App = nil
	noPods := measuredApp("team-a", "cronjobs", 0, 0)
	noPods.App.Efficiency = nil

	ranking := rankWaste([]models.CostActual{namespaceOnly, noPods, measuredApp("team-a", "web", 20, 0)}, nil)

	require.Len(t, ranking.Apps, 1)
	assert.Equal(t, "web", ranking.Apps[0].ApplicationName)
	assert.Equal(t, 1, ranking.NamespaceOnly)
	assert.Equal(t, 1, ranking.NoPods)
	assert.InDelta(t, 20, ranking.IdleCost, 1e-9)
}

func TestRankWaste_LeavesOutApplicationsNotDeployingHere(t *testing.T) {
	t.Parallel()

	remoteNamespaceOnly := measuredApp("argocd", "remote-legacy", 0, 0)
	remoteNamespaceOnly.App = nil
	local := localApplications([]exporter.Application{
		{Ref: models.AppRef{Namespace: "argocd", Name: "here"}},
		{Ref: models.AppRef{Namespace: "argocd", Name: "named"}, Destination: exporter.Destination{Name: exporter.LocalName}},
		{Ref: models.AppRef{Namespace: "argocd", Name: "remote"}, Destination: exporter.Destination{Server: "https://prod.example.com"}},
		{Ref: models.AppRef{Namespace: "argocd", Name: "remote-legacy"}, Destination: exporter.Destination{Name: "prod"}},
	})

	ranking := rankWaste([]models.CostActual{
		measuredApp("argocd", "here", 10, 0),
		measuredApp("argocd", "named", 5, 0),
		measuredApp("argocd", "remote", 500, 0),
		measuredApp("argocd", "deleted", 400, 0),
		remoteNamespaceOnly,
	}, local)

	require.Len(t, ranking.Apps, 2)
	assert.Equal(t, "here", ranking.Apps[0].ApplicationName)
	assert.Equal(t, "named", ranking.Apps[1].ApplicationName)
	assert.InDelta(t, 15, ranking.IdleCost, 1e-9)
	assert.Zero(t, ranking.NamespaceOnly, "an application on another cluster is not counted either")
}

func TestRankWaste_EmptyIsAnEmptyList(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(rankWaste(nil, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"apps":[],"idleCost":0,"namespaceOnly":0,"noPods":0}`, string(body))
}

type stubApplications struct {
	apps []exporter.Application
	err  error
}

func (s stubApplications) List(context.Context) ([]exporter.Application, error) { return s.apps, s.err }

func TestHandleGetWaste(t *testing.T) {
	t.Parallel()

	store := &mockStorage{listLatestActualsFn: func(context.Context) ([]models.CostActual, error) {
		return []models.CostActual{measuredApp("argocd", "web", 30, 10), measuredApp("argocd", "remote", 90, 0)}, nil
	}}
	gate := asApp(testGateApp.String())

	t.Run("filters by the live applications", func(t *testing.T) {
		t.Parallel()
		srv := newTestAPIServer(t, store, WithApplications(stubApplications{apps: []exporter.Application{
			{Ref: models.AppRef{Namespace: "argocd", Name: "web"}},
			{Ref: models.AppRef{Namespace: "argocd", Name: "remote"}, Destination: exporter.Destination{Name: "prod"}},
		}}))
		rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/waste", nil, gate)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var got models.WasteRanking
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		require.Len(t, got.Apps, 1)
		assert.Equal(t, "web", got.Apps[0].ApplicationName)
		assert.InDelta(t, 40, got.IdleCost, 1e-9)
	})

	t.Run("ranks every stored application without a list", func(t *testing.T) {
		t.Parallel()
		srv := newTestAPIServer(t, store)
		rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/waste", nil, gate)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var got models.WasteRanking
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		assert.Len(t, got.Apps, 2)
	})

	t.Run("a failed list is an error, not an unfiltered ranking", func(t *testing.T) {
		t.Parallel()
		srv := newTestAPIServer(t, store, WithApplications(stubApplications{err: errors.New("forbidden")}))
		rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/waste", nil, gate)
		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
		assert.NotContains(t, rr.Body.String(), "remote")
	})

	t.Run("a storage failure is an error", func(t *testing.T) {
		t.Parallel()
		failing := &mockStorage{listLatestActualsFn: func(context.Context) ([]models.CostActual, error) {
			return nil, errors.New("connection refused")
		}}
		srv := newTestAPIServer(t, failing)
		rr := doRequestWithHeaders(t, srv, http.MethodGet, "/api/cost-analysis/waste", nil, gate)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.NotContains(t, rr.Body.String(), "connection refused")
	})
}
