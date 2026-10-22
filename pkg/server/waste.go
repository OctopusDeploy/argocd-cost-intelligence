package server

import (
	"net/http"
	"sort"

	"github.com/go-chi/render"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// GET /api/cost-analysis/waste
// Ranks the applications by the idle cost of their own pods, from each one's latest record. With the
// live Application list, applications deploying to another cluster or deleted from Argo CD are left
// out; without one (outside a cluster) every stored application is ranked.
func (s *CostAPIServer) handleGetWaste(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	records, err := s.costStorage.ListLatestActuals(ctx)
	if err != nil {
		s.logger.Printf("Error listing latest cost records: %v\n", err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to list cost records"})
		return
	}

	var local map[models.AppRef]bool
	if s.applications != nil {
		apps, err := s.applications.List(ctx)
		if err != nil {
			s.logger.Printf("Error listing Argo CD Applications for the waste ranking: %v\n", err)
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]string{"error": "failed to list Argo CD applications"})
			return
		}
		local = localApplications(apps)
	}

	render.JSON(w, r, rankWaste(records, local))
}

// localApplications is the set of Applications deploying to this cluster.
func localApplications(apps []exporter.Application) map[models.AppRef]bool {
	local := make(map[models.AppRef]bool, len(apps))
	for _, app := range apps {
		if app.Destination.Local() {
			local[app.Ref] = true
		}
	}
	return local
}

// rankWaste ranks the applications measured on their own by idle cost, from records holding each
// application's latest record. With local non-nil, only the applications in it are counted.
func rankWaste(records []models.CostActual, local map[models.AppRef]bool) models.WasteRanking {
	ranking := models.WasteRanking{Apps: []models.AppWaste{}}
	for _, record := range records {
		ref := models.AppRef{Namespace: record.ApplicationNamespace, Name: record.ApplicationName}
		if local != nil && !local[ref] {
			continue
		}
		switch {
		case record.App == nil:
			ranking.NamespaceOnly++
			continue
		case record.App.Efficiency == nil:
			ranking.NoPods++
			continue
		}
		efficiency := *record.App.Efficiency
		idle := efficiency.CPUIdleCost + efficiency.RAMIdleCost
		ranking.Apps = append(ranking.Apps, models.AppWaste{
			ApplicationNamespace: record.ApplicationNamespace,
			ApplicationName:      record.ApplicationName,
			DeploymentNamespace:  record.DeploymentNamespace,
			MeasuredAt:           record.MeasuredAt,
			MonthlyCost:          record.App.TotalMonthlyCost,
			CPUCost:              record.App.OpenCostOutput.AllocationTotals.CPUCost,
			IdleCost:             idle,
			Efficiency:           efficiency,
			MeasuredHours:        record.App.OpenCostOutput.MeasuredHours,
		})
		ranking.IdleCost += idle
	}
	sort.Slice(ranking.Apps, func(i, j int) bool {
		a, b := ranking.Apps[i], ranking.Apps[j]
		if a.IdleCost != b.IdleCost {
			return a.IdleCost > b.IdleCost
		}
		if a.ApplicationNamespace != b.ApplicationNamespace {
			return a.ApplicationNamespace < b.ApplicationNamespace
		}
		return a.ApplicationName < b.ApplicationName
	})
	return ranking
}
