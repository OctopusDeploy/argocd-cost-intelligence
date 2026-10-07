package history

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

var webTarget = Target{App: web, Namespace: "shop", Resources: webDeploy}

func dates(days []models.DailyCost) []string {
	result := make([]string, 0, len(days))
	for _, day := range days {
		result = append(result, day.Date)
	}
	return result
}

func TestDaily_ServesRecordedDaysAndAsksOpenCostForTheRest(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.progress[web] = models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeApplication}
	require.NoError(t, store.SaveDailyCosts(context.Background(), web, "shop", models.ScopeApplication,
		[]models.DailyCost{{Date: "2026-06-01", TotalCost: 1}, {Date: "2026-10-02", TotalCost: 1}}))
	source := &fakeOpenCost{costs: costsFrom("shop", "2026-09-20", "2026-10-03", 2)}

	got, err := Daily(context.Background(), store, source, webTarget, mustDay("2026-05-01"), mustDay("2026-10-31"))

	require.NoError(t, err)
	assert.Equal(t, []string{"2026-06-01", "2026-10-02", "2026-10-03"}, dates(got))
	assert.Equal(t, []call{{"shop", "2026-10-03", "2026-10-31", models.ScopeApplication}}, source.calls,
		"OpenCost is not asked again for the days before the recorded history")
}

func TestDaily_APastRangeNeedsNoOpenCost(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.progress[web] = models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeApplication}
	require.NoError(t, store.SaveDailyCosts(context.Background(), web, "shop", models.ScopeApplication,
		[]models.DailyCost{{Date: "2026-08-31", TotalCost: 1}, {Date: "2026-09-01", TotalCost: 1}, {Date: "2026-09-30", TotalCost: 1}, {Date: "2026-10-01", TotalCost: 1}}))
	source := &fakeOpenCost{}

	got, err := Daily(context.Background(), store, source, webTarget, mustDay("2026-09-01"), mustDay("2026-09-30"))

	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-01", "2026-09-30"}, dates(got))
	assert.Empty(t, source.calls)
}

func TestDaily_AsksOpenCostForEveryDayWithoutHistoryInTheTargetsScope(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		progress *models.DailyProgress
	}{
		{"nothing recorded", nil},
		{"recorded for the whole namespace", &models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeNamespace}},
		{"recorded only before the range", &models.DailyProgress{Through: "2026-08-31", Scope: models.ScopeApplication}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeStore()
			if tt.progress != nil {
				store.progress[web] = *tt.progress
			}
			require.NoError(t, store.SaveDailyCosts(context.Background(), web, "shop", models.ScopeNamespace,
				[]models.DailyCost{{Date: "2026-09-15", TotalCost: 9}}))
			source := &fakeOpenCost{costs: costsFrom("shop", "2026-09-14", "2026-09-16", 2)}

			got, err := Daily(context.Background(), store, source, webTarget, mustDay("2026-09-01"), mustDay("2026-09-30"))

			require.NoError(t, err)
			assert.Equal(t, []call{{"shop", "2026-09-01", "2026-09-30", models.ScopeApplication}}, source.calls)
			assert.Equal(t, []string{"2026-09-14", "2026-09-15", "2026-09-16"}, dates(got))
			assert.InDelta(t, 2, got[1].TotalCost, 1e-9, "a day recorded in another scope is never served")
		})
	}
}

func TestDaily_ReportsFailures(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.progressErr = errors.New("database down")
	_, err := Daily(context.Background(), store, &fakeOpenCost{}, webTarget, mustDay("2026-09-01"), mustDay("2026-09-30"))
	require.ErrorContains(t, err, "database down")

	_, err = Daily(context.Background(), newFakeStore(), &fakeOpenCost{fail: errors.New("opencost down")}, webTarget,
		mustDay("2026-09-01"), mustDay("2026-09-30"))
	require.ErrorContains(t, err, "opencost down")
}
