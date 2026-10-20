//go:build integration

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

var (
	testStorage *PostgresCostStorage
	// testDSN connects to the test database; migration tests use it to create databases of their own.
	testDSN string
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run wraps m.Run() so deferred container cleanup executes before os.Exit.
func run(m *testing.M) int {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("cost_intelligence_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}
	defer ctr.Terminate(ctx) //nolint:errcheck

	testDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("failed to get connection string: " + err.Error())
	}

	testStorage, err = NewPostgresCostStorage(testDSN)
	if err != nil {
		panic("failed to connect to test database: " + err.Error())
	}
	defer testStorage.Close()

	if err := testStorage.InitializeSchema(ctx); err != nil {
		panic("failed to initialize schema: " + err.Error())
	}

	return m.Run()
}

// myApp is the Application most tests store and read.
var myApp = models.AppRef{Namespace: "argocd", Name: "my-app"}

// truncate clears all rows between tests so each test starts with a clean slate.
func truncate(t *testing.T) {
	t.Helper()
	_, err := testStorage.db.ExecContext(context.Background(), "TRUNCATE TABLE cost_actuals, cloud_estimates, cost_daily, cost_daily_progress")
	require.NoError(t, err)
}

// ---- Schema ----

func TestInitializeSchema_IsIdempotent(t *testing.T) {
	err := testStorage.InitializeSchema(context.Background())
	assert.NoError(t, err, "running InitializeSchema a second time should not error")
}

// ---- SaveActual / GetLatestActual ----

func TestSaveAndGetLatestActual_BasicRecord(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	record := &models.CostActual{
		ID:                   "app-001",
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		SyncTimestamp:        now,
		MeasuredAt:           now,
		TotalMonthlyCost:     450.0,
		EstimateAccuracy:     0.95,
		EstimateVsActual:     10.0,
	}

	require.NoError(t, testStorage.SaveActual(context.Background(), record))

	got, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)

	assert.Equal(t, "app-001", got.ID)
	assert.Equal(t, "my-app", got.ApplicationName)
	assert.Equal(t, "argocd", got.ApplicationNamespace)
	assert.InDelta(t, 450.0, got.TotalMonthlyCost, 0.01)
	assert.Nil(t, got.Efficiency)
}

func TestSaveAndGetLatestActual_WithEfficiency(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	record := &models.CostActual{
		ID:                   "app-002",
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		SyncTimestamp:        now,
		MeasuredAt:           now,
		TotalMonthlyCost:     300.0,
		Efficiency: &models.EfficiencyMetrics{
			CPUEfficiency:     0.4,
			RAMEfficiency:     0.7,
			TotalEfficiency:   0.55,
			CPUCoresRequested: 1.0,
			CPUCoresUsed:      0.4,
			RAMBytesRequested: 536870912,
			RAMBytesUsed:      375809638,
			CPUIdleCost:       1628.1,
			RAMIdleCost:       497.7,
		},
	}

	require.NoError(t, testStorage.SaveActual(context.Background(), record))

	got, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)
	require.NotNil(t, got.Efficiency)

	assert.InDelta(t, 0.4, got.Efficiency.CPUEfficiency, 1e-6)
	assert.InDelta(t, 0.7, got.Efficiency.RAMEfficiency, 1e-6)
	assert.InDelta(t, 0.55, got.Efficiency.TotalEfficiency, 1e-6)
	assert.InDelta(t, 1.0, got.Efficiency.CPUCoresRequested, 1e-6)
	assert.InDelta(t, 536870912.0, got.Efficiency.RAMBytesRequested, 1e-6)
	assert.InDelta(t, 1628.1, got.Efficiency.CPUIdleCost, 0.01)
	assert.InDelta(t, 497.7, got.Efficiency.RAMIdleCost, 0.01)
}

func TestSaveActual_RoundTripsIdleMonthlyCost(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	idle := 212.5
	measured := &models.CostActual{
		ID: "idle-1", ApplicationName: "my-app", ApplicationNamespace: "argocd",
		SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 300.0, IdleMonthlyCost: &idle,
	}
	older := &models.CostActual{
		ID: "idle-0", ApplicationName: "my-app", ApplicationNamespace: "argocd",
		SyncTimestamp: now.Add(-time.Hour), MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 280.0,
	}
	require.NoError(t, testStorage.SaveActual(context.Background(), older))
	require.NoError(t, testStorage.SaveActual(context.Background(), measured))

	got, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)
	require.NotNil(t, got.IdleMonthlyCost)
	assert.InDelta(t, 212.5, *got.IdleMonthlyCost, 0.01)

	history, err := testStorage.GetActualsInDateRange(context.Background(), myApp, now.Add(-2*time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, history, 2)
	for _, r := range history {
		if r.ID == "idle-0" {
			assert.Nil(t, r.IdleMonthlyCost, "a record without an idle cost reads back without one")
		}
	}
}

func TestSaveActual_Upsert_UpdatesExistingRecord(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	original := &models.CostActual{
		ID:                   "app-upsert",
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		SyncTimestamp:        now,
		MeasuredAt:           now,
		TotalMonthlyCost:     100.0,
	}
	require.NoError(t, testStorage.SaveActual(context.Background(), original))

	updated := &models.CostActual{
		ID:                   "app-upsert", // same ID
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		SyncTimestamp:        now,
		MeasuredAt:           now.Add(time.Minute),
		TotalMonthlyCost:     200.0,
	}
	require.NoError(t, testStorage.SaveActual(context.Background(), updated))

	got, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)
	assert.InDelta(t, 200.0, got.TotalMonthlyCost, 0.01, "upsert should update the existing record")

	// Verify only one row exists.
	var count int
	require.NoError(t, testStorage.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM cost_actuals WHERE application_name = 'my-app'").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestGetLatestActual_NotFound(t *testing.T) {
	truncate(t)

	_, err := testStorage.GetLatestActual(context.Background(), models.AppRef{Namespace: "argocd", Name: "nonexistent-app"})
	require.ErrorIs(t, err, ErrNotFound, "so instrumentation does not count it as a failure")
	assert.Contains(t, err.Error(), "nonexistent-app")
}

func TestGetLatestActual_ReturnsLatestByMeasuredAt(t *testing.T) {
	truncate(t)

	base := time.Now().UTC().Truncate(time.Second)
	records := []*models.CostActual{
		{ID: "r1", ApplicationName: "my-app", ApplicationNamespace: "argocd", SyncTimestamp: base, MeasuredAt: base.Add(-2 * time.Hour), TotalMonthlyCost: 100.0},
		{ID: "r2", ApplicationName: "my-app", ApplicationNamespace: "argocd", SyncTimestamp: base, MeasuredAt: base.Add(-1 * time.Hour), TotalMonthlyCost: 200.0},
		{ID: "r3", ApplicationName: "my-app", ApplicationNamespace: "argocd", SyncTimestamp: base, MeasuredAt: base, TotalMonthlyCost: 300.0},
	}
	for _, r := range records {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}

	got, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)
	assert.Equal(t, "r3", got.ID)
	assert.InDelta(t, 300.0, got.TotalMonthlyCost, 0.01)
}

// ---- GetActualsInDateRange ----

func TestGetActualsInDateRange_FiltersCorrectly(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	records := []*models.CostActual{
		{ID: "d1", ApplicationName: "my-app", ApplicationNamespace: "argocd", SyncTimestamp: now, MeasuredAt: now.Add(-48 * time.Hour), TotalMonthlyCost: 50.0},
		{ID: "d2", ApplicationName: "my-app", ApplicationNamespace: "argocd", SyncTimestamp: now, MeasuredAt: now.Add(-24 * time.Hour), TotalMonthlyCost: 100.0},
		{ID: "d3", ApplicationName: "my-app", ApplicationNamespace: "argocd", SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 150.0},
	}
	for _, r := range records {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}

	start := now.Add(-30 * time.Hour)
	end := now.Add(time.Hour)

	got, err := testStorage.GetActualsInDateRange(context.Background(), myApp, start, end)
	require.NoError(t, err)
	assert.Len(t, got, 2, "only records within the date range should be returned")

	ids := []string{got[0].ID, got[1].ID}
	assert.Contains(t, ids, "d2")
	assert.Contains(t, ids, "d3")
}

func TestGetActualsInDateRange_DifferentApps_Isolated(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	records := []*models.CostActual{
		{ID: "a1", ApplicationName: "app-a", ApplicationNamespace: "argocd", SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 100.0},
		{ID: "b1", ApplicationName: "app-b", ApplicationNamespace: "argocd", SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 200.0},
	}
	for _, r := range records {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}

	got, err := testStorage.GetActualsInDateRange(context.Background(), models.AppRef{Namespace: "argocd", Name: "app-a"},
		now.Add(-time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "a1", got[0].ID)
}

func TestGetActualsInDateRange_Empty(t *testing.T) {
	truncate(t)

	now := time.Now().UTC()
	got, err := testStorage.GetActualsInDateRange(context.Background(), myApp,
		now.Add(-time.Hour), now)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ---- GetNamespaceCostSummary ----

// daysAgoAt returns hour:00 UTC on the day n days before today.
func daysAgoAt(n, hour int) time.Time {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return today.AddDate(0, 0, -n).Add(time.Duration(hour) * time.Hour)
}

// saveNamespaceRecord stores a measurement of deployNS taken for app (an Application in "argocd").
func saveNamespaceRecord(t *testing.T, id, app, deployNS string, measuredAt time.Time, cost float64) {
	t.Helper()
	require.NoError(t, testStorage.SaveActual(context.Background(), &models.CostActual{
		ID: id, ApplicationName: app, ApplicationNamespace: "argocd", DeploymentNamespace: deployNS,
		SyncTimestamp: measuredAt, MeasuredAt: measuredAt, TotalMonthlyCost: cost,
	}))
}

func TestGetNamespaceCostSummary_FiltersByDeploymentNamespace(t *testing.T) {
	truncate(t)

	saveNamespaceRecord(t, "p1", "app-a", "production", daysAgoAt(2, 12), 100.0)
	saveNamespaceRecord(t, "p2", "app-a", "production", daysAgoAt(1, 12), 200.0)
	saveNamespaceRecord(t, "s1", "app-b", "staging", daysAgoAt(1, 12), 999.0)

	avg, err := testStorage.GetNamespaceCostSummary(context.Background(), "production", 30)
	require.NoError(t, err)
	assert.InDelta(t, 150.0, avg, 0.01, "average of production's days, 100 and 200; staging excluded")

	avg, err = testStorage.GetNamespaceCostSummary(context.Background(), "argocd", 30)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, avg, 1e-9, "argocd is where the Application resources live, not where anything deploys")
}

func TestGetNamespaceCostSummary_SharedNamespace_CountsEachDayOnce(t *testing.T) {
	truncate(t)

	// Three apps deploy to "shared" and synced on the same day; each record measures the whole
	// namespace. The next day only one of them synced.
	saveNamespaceRecord(t, "a1", "app-a", "shared", daysAgoAt(2, 10), 100.0)
	saveNamespaceRecord(t, "b1", "app-b", "shared", daysAgoAt(2, 10), 100.0)
	saveNamespaceRecord(t, "c1", "app-c", "shared", daysAgoAt(2, 10), 100.0)
	saveNamespaceRecord(t, "a2", "app-a", "shared", daysAgoAt(1, 10), 400.0)

	avg, err := testStorage.GetNamespaceCostSummary(context.Background(), "shared", 30)
	require.NoError(t, err)
	assert.InDelta(t, 250.0, avg, 0.01, "each day counts once: (100 + 400) / 2, not (3*100 + 400) / 4")
}

func TestGetNamespaceCostSummary_LatestMeasurementOfDayWins(t *testing.T) {
	truncate(t)

	saveNamespaceRecord(t, "early", "app-a", "production", daysAgoAt(2, 8), 40.0)
	saveNamespaceRecord(t, "late", "app-b", "production", daysAgoAt(2, 20), 100.0)
	saveNamespaceRecord(t, "next", "app-a", "production", daysAgoAt(1, 8), 300.0)

	avg, err := testStorage.GetNamespaceCostSummary(context.Background(), "production", 30)
	require.NoError(t, err)
	assert.InDelta(t, 200.0, avg, 0.01, "a day is its latest measurement: (100 + 300) / 2")
}

func TestGetNamespaceCostSummary_EmptyNamespace_ReturnsZero(t *testing.T) {
	truncate(t)

	avg, err := testStorage.GetNamespaceCostSummary(context.Background(), "nonexistent-ns", 30)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, avg, 1e-9)
}

func TestSaveAndGetLatestActual_WithPods(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	record := &models.CostActual{
		ID:                   "app-pods",
		ApplicationName:      "my-app",
		ApplicationNamespace: "argocd",
		SyncTimestamp:        now,
		MeasuredAt:           now,
		TotalMonthlyCost:     450.0,
		Pods: []models.PodCost{
			{Name: "redis-0", Namespace: "argocd", ControllerKind: "StatefulSet", CPUCost: 270.0, RAMCost: 90.0, TotalCost: 360.0},
			{Name: "nginx-abc", Namespace: "argocd", ControllerKind: "Deployment", CPUCost: 60.0, RAMCost: 30.0, TotalCost: 90.0},
		},
	}

	require.NoError(t, testStorage.SaveActual(context.Background(), record))

	got, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)
	require.Len(t, got.Pods, 2)

	byName := make(map[string]models.PodCost)
	for _, p := range got.Pods {
		byName[p.Name] = p
	}

	redis := byName["redis-0"]
	assert.Equal(t, "StatefulSet", redis.ControllerKind)
	assert.InDelta(t, 270.0, redis.CPUCost, 0.01)
	assert.InDelta(t, 360.0, redis.TotalCost, 0.01)

	nginx := byName["nginx-abc"]
	assert.Equal(t, "Deployment", nginx.ControllerKind)
	assert.InDelta(t, 90.0, nginx.TotalCost, 0.01)
}

func TestGetNamespaceCostSummary_ExcludesOldRecords(t *testing.T) {
	truncate(t)

	saveNamespaceRecord(t, "new", "app-a", "production", daysAgoAt(1, 12), 100.0)
	saveNamespaceRecord(t, "old", "app-b", "production", daysAgoAt(8, 12), 999.0)

	avg, err := testStorage.GetNamespaceCostSummary(context.Background(), "production", 7)
	require.NoError(t, err)
	assert.InDelta(t, 100.0, avg, 0.01, "old record outside the window should be excluded")
}

// ---- ListNamespaces ----

func TestListNamespaces_CountsSharedNamespaceOnce(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	idle := func(v float64) *float64 { return &v }
	records := []*models.CostActual{
		// Both apps deploy to "shared", and each record measures the whole namespace.
		{
			ID: "s1", ApplicationName: "app-a", ApplicationNamespace: "argocd", DeploymentNamespace: "shared",
			SyncTimestamp: now, MeasuredAt: now.Add(-2 * time.Hour), TotalMonthlyCost: 100.0, IdleMonthlyCost: idle(40.0),
		},
		{
			ID: "s2", ApplicationName: "app-b", ApplicationNamespace: "argocd", DeploymentNamespace: "shared",
			SyncTimestamp: now, MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 120.0, IdleMonthlyCost: idle(55.0),
		},
		// Measured before the idle cost was.
		{
			ID: "o1", ApplicationName: "app-c", ApplicationNamespace: "argocd", DeploymentNamespace: "other",
			SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 50.0,
			Efficiency: &models.EfficiencyMetrics{CPUIdleCost: 30.0, RAMIdleCost: 10.0},
		},
	}
	for _, r := range records {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}

	got, err := testStorage.ListNamespaces(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "shared", got[0].Namespace)
	assert.InDelta(t, 120.0, got[0].TotalCost, 0.01, "a shared namespace costs its latest measurement, not the sum of its apps")
	require.NotNil(t, got[0].IdleCost)
	assert.InDelta(t, 55.0, *got[0].IdleCost, 0.01, "idle as of the latest measurement")
	assert.Equal(t, 2, got[0].AppCount)
	assert.WithinDuration(t, now.Add(-time.Hour), got[0].MeasuredAt, time.Second, "measured at the latest record")

	assert.Equal(t, "other", got[1].Namespace)
	assert.InDelta(t, 50.0, got[1].TotalCost, 0.01)
	assert.Nil(t, got[1].IdleCost, "a record from before the idle cost was measured has none, not zero")
	assert.Equal(t, 1, got[1].AppCount)
}

func TestListNamespaces_CountsAppsWhereTheyDeployNow(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	records := []*models.CostActual{
		{ID: "m1", ApplicationName: "app-a", ApplicationNamespace: "argocd", DeploymentNamespace: "old-ns", SyncTimestamp: now, MeasuredAt: now.Add(-2 * time.Hour), TotalMonthlyCost: 80.0},
		{ID: "m2", ApplicationName: "app-a", ApplicationNamespace: "argocd", DeploymentNamespace: "new-ns", SyncTimestamp: now, MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 90.0},
		{ID: "m3", ApplicationName: "app-b", ApplicationNamespace: "argocd", DeploymentNamespace: "old-ns", SyncTimestamp: now, MeasuredAt: now.Add(-3 * time.Hour), TotalMonthlyCost: 70.0},
	}
	for _, r := range records {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}

	got, err := testStorage.ListNamespaces(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "new-ns", got[0].Namespace)
	assert.InDelta(t, 90.0, got[0].TotalCost, 0.01)
	assert.Equal(t, 1, got[0].AppCount)

	assert.Equal(t, "old-ns", got[1].Namespace)
	assert.InDelta(t, 70.0, got[1].TotalCost, 0.01, "app-a's record from before it moved must not set old-ns's cost")
	assert.Equal(t, 1, got[1].AppCount)
}

func TestListNamespaces_SkipsRecordsWithoutDeploymentNamespace(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, testStorage.SaveActual(context.Background(), &models.CostActual{
		ID: "legacy", ApplicationName: "app-a", ApplicationNamespace: "argocd", SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 10.0,
	}))

	got, err := testStorage.ListNamespaces(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ---- Applications with the same name in different namespaces ----

// With apps-in-any-namespace, team-a:web and team-b:web are different Applications.
var (
	teamAWeb = models.AppRef{Namespace: "team-a", Name: "web"}
	teamBWeb = models.AppRef{Namespace: "team-b", Name: "web"}
)

// saveSameNamedApps stores two records for team-a:web and a newer one for team-b:web.
func saveSameNamedApps(t *testing.T, now time.Time) {
	t.Helper()
	records := []*models.CostActual{
		{ID: "a1", ApplicationName: "web", ApplicationNamespace: "team-a", DeploymentNamespace: "web-a", SyncTimestamp: now, MeasuredAt: now.Add(-2 * time.Hour), TotalMonthlyCost: 100.0},
		{ID: "a2", ApplicationName: "web", ApplicationNamespace: "team-a", DeploymentNamespace: "web-a", SyncTimestamp: now, MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 110.0},
		{ID: "b1", ApplicationName: "web", ApplicationNamespace: "team-b", DeploymentNamespace: "web-b", SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 900.0},
	}
	for _, r := range records {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}
}

func TestGetLatestActual_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	truncate(t)
	saveSameNamedApps(t, time.Now().UTC().Truncate(time.Second))

	got, err := testStorage.GetLatestActual(context.Background(), teamAWeb)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID, "team-b:web's newer record must not be returned for team-a:web")
	assert.Equal(t, "team-a", got.ApplicationNamespace)

	got, err = testStorage.GetLatestActual(context.Background(), teamBWeb)
	require.NoError(t, err)
	assert.Equal(t, "b1", got.ID)

	_, err = testStorage.GetLatestActual(context.Background(), models.AppRef{Namespace: "team-c", Name: "web"})
	assert.Error(t, err, "an Application with no records of its own gets nobody else's")
}

func TestGetActualsInDateRange_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	truncate(t)
	now := time.Now().UTC().Truncate(time.Second)
	saveSameNamedApps(t, now)

	got, err := testStorage.GetActualsInDateRange(context.Background(), teamAWeb, now.Add(-24*time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, a := range got {
		assert.Equal(t, "team-a", a.ApplicationNamespace, a.ID)
	}
}

func TestGetRecentActuals_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	truncate(t)
	saveSameNamedApps(t, time.Now().UTC().Truncate(time.Second))

	got, err := testStorage.GetRecentActuals(context.Background(), teamAWeb, 10)
	require.NoError(t, err)
	ids := make([]string, 0, len(got))
	for _, a := range got {
		ids = append(ids, a.ID)
	}
	assert.Equal(t, []string{"a2", "a1"}, ids)
}

func TestLatestActualsForNamespace_CountsSameNamedAppsSeparately(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	for _, r := range []*models.CostActual{
		{ID: "a1", ApplicationName: "web", ApplicationNamespace: "team-a", DeploymentNamespace: "shared", SyncTimestamp: now, MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 100.0},
		{ID: "b1", ApplicationName: "web", ApplicationNamespace: "team-b", DeploymentNamespace: "shared", SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 120.0},
	} {
		require.NoError(t, testStorage.SaveActual(context.Background(), r))
	}

	apps, err := testStorage.GetLatestActualsForNamespace(context.Background(), "shared")
	require.NoError(t, err)
	assert.Len(t, apps, 2, "team-a:web and team-b:web are different applications")

	namespaces, err := testStorage.ListNamespaces(context.Background())
	require.NoError(t, err)
	require.Len(t, namespaces, 1)
	assert.Equal(t, 2, namespaces[0].AppCount)
	assert.InDelta(t, 120.0, namespaces[0].TotalCost, 0.01)
}

// ---- Cloud estimates ----

func TestCloudEstimate_SameNameInAnotherNamespace_NotReturned(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, testStorage.SaveCloudEstimate(context.Background(), teamAWeb, &models.CloudServiceEstimate{
		Source: "infracost", TotalMonthlyCost: 115.0, Currency: "USD", PushedAt: now.Add(-time.Hour),
		Resources: []models.CloudServiceResource{{Name: "aws_db_instance.a", ResourceType: "aws_db_instance", MonthlyCost: 115.0}},
	}))
	require.NoError(t, testStorage.SaveCloudEstimate(context.Background(), teamBWeb, &models.CloudServiceEstimate{
		Source: "infracost", TotalMonthlyCost: 5000.0, Currency: "USD", PushedAt: now,
	}))

	got, err := testStorage.GetCloudEstimate(context.Background(), teamAWeb)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.InDelta(t, 115.0, got.TotalMonthlyCost, 0.01, "team-b:web's newer estimate must not be returned for team-a:web")
	require.Len(t, got.Resources, 1)

	got, err = testStorage.GetCloudEstimate(context.Background(), teamBWeb)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.InDelta(t, 5000.0, got.TotalMonthlyCost, 0.01)

	got, err = testStorage.GetCloudEstimate(context.Background(), models.AppRef{Namespace: "team-c", Name: "web"})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSaveCloudEstimate_UpsertsPerApplicationAndSource(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, testStorage.SaveCloudEstimate(context.Background(), teamAWeb, &models.CloudServiceEstimate{
		Source: "infracost", TotalMonthlyCost: 100.0, Currency: "USD", PushedAt: now.Add(-time.Hour),
	}))
	require.NoError(t, testStorage.SaveCloudEstimate(context.Background(), teamAWeb, &models.CloudServiceEstimate{
		Source: "infracost", TotalMonthlyCost: 150.0, Currency: "USD", PushedAt: now,
	}))

	got, err := testStorage.GetCloudEstimate(context.Background(), teamAWeb)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.InDelta(t, 150.0, got.TotalMonthlyCost, 0.01)

	var count int
	require.NoError(t, testStorage.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM cloud_estimates").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestListCloudEstimates_EveryApplicationAndSource(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	for _, e := range []struct {
		app    models.AppRef
		source string
		cost   float64
	}{{teamBWeb, "infracost", 5000}, {teamAWeb, "terracost", 90}, {teamAWeb, "infracost", 115}} {
		require.NoError(t, testStorage.SaveCloudEstimate(context.Background(), e.app, &models.CloudServiceEstimate{
			Source: e.source, TotalMonthlyCost: e.cost, Currency: "USD", PushedAt: now,
			Resources: []models.CloudServiceResource{{Name: "aws_db_instance.a", MonthlyCost: e.cost}},
		}))
	}
	// A row from before migration 005 that no application can be matched to.
	_, err := testStorage.db.ExecContext(context.Background(),
		`INSERT INTO cloud_estimates (app_namespace, app_name, source, total_monthly_cost, currency, pushed_at)
		 VALUES ('', 'web', 'infracost', 1, 'USD', $1)`, now)
	require.NoError(t, err)

	got, err := testStorage.ListCloudEstimates(context.Background())
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, teamAWeb, got[0].App)
	assert.Equal(t, "infracost", got[0].Estimate.Source)
	assert.InDelta(t, 115.0, got[0].Estimate.TotalMonthlyCost, 0.01)
	assert.Equal(t, "USD", got[0].Estimate.Currency)
	assert.True(t, now.Equal(got[0].Estimate.PushedAt))
	assert.Nil(t, got[0].Estimate.Resources, "the breakdown is not read")
	assert.Equal(t, "terracost", got[1].Estimate.Source)
	assert.Equal(t, teamBWeb, got[2].App)
}

// ---- Migration 005: keying by Application namespace ----

// newDatabaseAt creates an empty database, migrates it to version and returns a connection to it.
// It lets a test write rows the way an older release did before migrating them.
func newDatabaseAt(t *testing.T, version uint) (*sql.DB, *migrate.Migrate) {
	t.Helper()
	ctx := context.Background()

	name := "migration_" + strings.ToLower(strings.NewReplacer("/", "_", "-", "_").Replace(t.Name()))
	_, err := testStorage.db.ExecContext(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)

	u, err := url.Parse(testDSN)
	require.NoError(t, err)
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = testStorage.db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	m, err := newMigrator(db)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(version))
	return db, m
}

func exec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), query)
	require.NoError(t, err)
}

// namespacesByKey returns column ns of table, keyed by the key column's value.
func namespacesByKey(t *testing.T, db *sql.DB, table, key, ns string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "SELECT "+key+", "+ns+" FROM "+table) //nolint:gosec // identifiers come from the calling test, not from input
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var k, v string
		require.NoError(t, rows.Scan(&k, &v))
		got[k] = v
	}
	require.NoError(t, rows.Err())
	return got
}

const insertV4Actual = `INSERT INTO cost_actuals
	(id, application_name, application_namespace, deployment_namespace, sync_timestamp, measured_at, total_monthly_cost)
	VALUES ('%s', '%s', '%s', 'deploy', NOW(), NOW(), 1)`

func TestMigration005_SingleNamespaceInstall_BackfillsEmptyNamespaces(t *testing.T) {
	db, m := newDatabaseAt(t, 4)
	exec(t, db, fmt.Sprintf(insertV4Actual, "r1", "web", "argocd"))
	exec(t, db, fmt.Sprintf(insertV4Actual, "r2", "legacy", ""))
	exec(t, db, `INSERT INTO cloud_estimates (app_name, source) VALUES ('web', 'infracost'), ('never-synced', 'infracost')`)

	require.NoError(t, m.Up())

	assert.Equal(t, map[string]string{"r1": "argocd", "r2": "argocd"},
		namespacesByKey(t, db, "cost_actuals", "id", "application_namespace"),
		"with one Application namespace in use, rows without one belong to it")
	assert.Equal(t, map[string]string{"web": "argocd", "never-synced": "argocd"},
		namespacesByKey(t, db, "cloud_estimates", "app_name", "app_namespace"))
}

func TestMigration005_SeveralNamespaces_LeavesAmbiguousRowsUnreachable(t *testing.T) {
	db, m := newDatabaseAt(t, 4)
	exec(t, db, fmt.Sprintf(insertV4Actual, "a-web", "web", "team-a"))
	exec(t, db, fmt.Sprintf(insertV4Actual, "b-web", "web", "team-b"))
	exec(t, db, fmt.Sprintf(insertV4Actual, "a-api", "api", "team-a"))
	exec(t, db, fmt.Sprintf(insertV4Actual, "legacy", "legacy", ""))
	exec(t, db, `INSERT INTO cloud_estimates (app_name, source) VALUES
		('web', 'infracost'), ('api', 'infracost'), ('never-synced', 'infracost')`)

	require.NoError(t, m.Up())

	assert.Equal(t, map[string]string{"a-web": "team-a", "b-web": "team-b", "a-api": "team-a", "legacy": ""},
		namespacesByKey(t, db, "cost_actuals", "id", "application_namespace"),
		"a row without a namespace cannot be attributed when several are in use")
	assert.Equal(t, map[string]string{"web": "", "api": "team-a", "never-synced": ""},
		namespacesByKey(t, db, "cloud_estimates", "app_name", "app_namespace"),
		"an estimate belongs to its app's only namespace; a name used in several is ambiguous")

	// Same-named Applications can now each keep an estimate from the same source.
	store := &PostgresCostStorage{db: db}
	for _, app := range []models.AppRef{teamAWeb, teamBWeb} {
		require.NoError(t, store.SaveCloudEstimate(context.Background(), app, &models.CloudServiceEstimate{
			Source: "infracost", Currency: "USD", PushedAt: time.Now(),
		}))
	}
}

func TestMigration005_Down_KeepsNewestEstimatePerName(t *testing.T) {
	db, m := newDatabaseAt(t, 5)
	exec(t, db, `INSERT INTO cloud_estimates (app_namespace, app_name, source, total_monthly_cost, pushed_at) VALUES
		('team-a', 'web', 'infracost', 1, NOW() - INTERVAL '1 hour'),
		('team-b', 'web', 'infracost', 2, NOW())`)

	require.NoError(t, m.Migrate(4))

	var total float64
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT total_monthly_cost FROM cloud_estimates WHERE app_name = 'web'").Scan(&total))
	assert.InDelta(t, 2.0, total, 0.01)
}

// ---- Migration 006: deployment namespace index ----

// indexNames returns the names of the indexes on cost_actuals.
func indexNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		"SELECT indexname FROM pg_indexes WHERE tablename = 'cost_actuals'")
	require.NoError(t, err)
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	return names
}

func TestMigration006_IndexesDeploymentNamespace(t *testing.T) {
	db, m := newDatabaseAt(t, 6)
	names := indexNames(t, db)
	assert.Contains(t, names, "idx_cost_actuals_deployment_ns_measured")
	assert.NotContains(t, names, "idx_cost_actuals_namespace_measured")

	require.NoError(t, m.Migrate(5))
	names = indexNames(t, db)
	assert.NotContains(t, names, "idx_cost_actuals_deployment_ns_measured")
	assert.Contains(t, names, "idx_cost_actuals_namespace_measured")
}

// ---- Application measurements ----

func TestSaveActual_RoundTripsTheApplicationsMeasurement(t *testing.T) {
	truncate(t)

	now := time.Now().UTC().Truncate(time.Second)
	controllers := models.ManagedResources{{Kind: "Deployment", Name: "redis", Namespace: "argocd"}}
	require.NoError(t, testStorage.SaveActual(context.Background(), &models.CostActual{
		ID: "app-new", ApplicationName: "my-app", ApplicationNamespace: "argocd", DeploymentNamespace: "argocd",
		SyncTimestamp: now, MeasuredAt: now, TotalMonthlyCost: 9.55,
		App: &models.AppMeasurement{
			Controllers:      controllers,
			TotalMonthlyCost: 0.63,
			OpenCostOutput:   models.OpenCostMetrics{AllocationTotals: models.AllocationTotal{TotalCost: 0.63}},
			Efficiency:       &models.EfficiencyMetrics{CPUEfficiency: 0.96, CPUIdleCost: 0.01},
			Pods:             []models.PodCost{{Name: "redis-1", Controller: "redis", TotalCost: 0.63}},
		},
	}))
	require.NoError(t, testStorage.SaveActual(context.Background(), &models.CostActual{
		ID: "app-old", ApplicationName: "my-app", ApplicationNamespace: "argocd", DeploymentNamespace: "argocd",
		SyncTimestamp: now, MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 8.54,
	}))

	latest, err := testStorage.GetLatestActual(context.Background(), myApp)
	require.NoError(t, err)
	assert.InDelta(t, 9.55, latest.TotalMonthlyCost, 0.001, "the namespace measurement is unchanged")
	require.NotNil(t, latest.App)
	assert.Equal(t, controllers, latest.App.Controllers)
	assert.InDelta(t, 0.63, latest.App.TotalMonthlyCost, 0.001)
	assert.InDelta(t, 0.96, latest.App.Efficiency.CPUEfficiency, 0.001)
	assert.Equal(t, "redis-1", latest.App.Pods[0].Name)

	inRange, err := testStorage.GetActualsInDateRange(context.Background(), myApp, now.Add(-2*time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, inRange, 2)
	assert.NotNil(t, inRange[0].App)
	assert.Nil(t, inRange[1].App, "an older record has no application measurement")

	recent, err := testStorage.GetRecentActuals(context.Background(), myApp, 10)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	require.NotNil(t, recent[0].App)
	assert.InDelta(t, 0.63, recent[0].App.TotalMonthlyCost, 0.001)
	assert.InDelta(t, 9.55, recent[0].TotalMonthlyCost, 0.001)
	assert.Nil(t, recent[1].App)
}

func TestMigration007_AddsTheApplicationMeasurementToExistingRecords(t *testing.T) {
	db, m := newDatabaseAt(t, 6)
	exec(t, db, `INSERT INTO cost_actuals (id, application_name, application_namespace, sync_timestamp, measured_at, total_monthly_cost)
		VALUES ('old', 'web', 'argocd', NOW(), NOW(), 12.5)`)

	require.NoError(t, m.Migrate(7))
	var app sql.NullString
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT app_json FROM cost_actuals WHERE id = 'old'").Scan(&app))
	assert.False(t, app.Valid, "existing records measured only the namespace")

	require.NoError(t, m.Migrate(6))
	var columns int
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'cost_actuals' AND column_name = 'app_json'").Scan(&columns))
	assert.Zero(t, columns)
}

// ---- Daily cost history ----

func TestDailyCosts_RoundTripPerApplicationScopeAndRange(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	day := func(date string, cost float64) models.DailyCost {
		return models.DailyCost{Date: date, TotalCost: cost, CPUCost: cost / 2, MemoryCost: cost / 4, NetworkCost: cost / 8, StorageCost: cost / 8}
	}
	require.NoError(t, testStorage.SaveDailyCosts(ctx, teamAWeb, "web-a", models.ScopeApplication,
		[]models.DailyCost{day("2026-09-30", 4), day("2026-09-01", 2), day("2026-08-31", 1), day("2026-10-01", 8)}))
	require.NoError(t, testStorage.SaveDailyCosts(ctx, teamBWeb, "web-b", models.ScopeApplication,
		[]models.DailyCost{day("2026-09-15", 99)}))
	require.NoError(t, testStorage.SaveDailyCosts(ctx, teamAWeb, "web-a", models.ScopeApplication, nil))

	got, err := testStorage.GetDailyCosts(ctx, teamAWeb, models.ScopeApplication, "2026-09-01", "2026-09-30")
	require.NoError(t, err)
	assert.Equal(t, []models.DailyCost{day("2026-09-01", 2), day("2026-09-30", 4)}, got,
		"oldest first, both ends included, and nothing of team-b:web's")

	other, err := testStorage.GetDailyCosts(ctx, teamAWeb, models.ScopeNamespace, "2026-09-01", "2026-09-30")
	require.NoError(t, err)
	assert.Empty(t, other, "days are served only in the scope they were recorded in")

	// Recording a day again replaces it, scope included.
	require.NoError(t, testStorage.SaveDailyCosts(ctx, teamAWeb, "web-a", models.ScopeNamespace, []models.DailyCost{day("2026-09-01", 3)}))
	got, err = testStorage.GetDailyCosts(ctx, teamAWeb, models.ScopeNamespace, "2026-09-01", "2026-09-30")
	require.NoError(t, err)
	assert.Equal(t, []models.DailyCost{day("2026-09-01", 3)}, got)
}

func TestDailyProgress_IsKeptPerApplication(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	got, err := testStorage.GetDailyProgress(ctx, teamAWeb)
	require.NoError(t, err)
	assert.Nil(t, got, "nothing recorded yet")

	require.NoError(t, testStorage.SetDailyProgress(ctx, teamAWeb, models.DailyProgress{Through: "2026-09-30", Scope: models.ScopeNamespace}))
	require.NoError(t, testStorage.SetDailyProgress(ctx, teamAWeb, models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeApplication}))

	got, err = testStorage.GetDailyProgress(ctx, teamAWeb)
	require.NoError(t, err)
	assert.Equal(t, &models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeApplication}, got)

	got, err = testStorage.GetDailyProgress(ctx, teamBWeb)
	require.NoError(t, err)
	assert.Nil(t, got, "team-b:web shares the name, not the progress")
}

func TestPruneDailyCosts_DeletesDaysBeforeTheCutoff(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	require.NoError(t, testStorage.SaveDailyCosts(ctx, teamAWeb, "web-a", models.ScopeApplication, []models.DailyCost{
		{Date: "2025-08-28", TotalCost: 1}, {Date: "2025-08-29", TotalCost: 1},
	}))
	require.NoError(t, testStorage.SaveDailyCosts(ctx, teamBWeb, "web-b", models.ScopeApplication, []models.DailyCost{
		{Date: "2025-01-01", TotalCost: 1},
	}))

	require.NoError(t, testStorage.PruneDailyCosts(ctx, "2025-08-29"))

	a, err := testStorage.GetDailyCosts(ctx, teamAWeb, models.ScopeApplication, "2000-01-01", "2100-01-01")
	require.NoError(t, err)
	require.Len(t, a, 1)
	assert.Equal(t, "2025-08-29", a[0].Date)
	b, err := testStorage.GetDailyCosts(ctx, teamBWeb, models.ScopeApplication, "2000-01-01", "2100-01-01")
	require.NoError(t, err)
	assert.Empty(t, b)
}

func TestDeleteMeasurements_DeletesOneApplicationsCostAndHistoryOnly(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	saveSameNamedApps(t, now)
	for _, app := range []models.AppRef{teamAWeb, teamBWeb} {
		require.NoError(t, testStorage.SaveDailyCosts(ctx, app, "web", models.ScopeApplication, []models.DailyCost{{Date: "2026-10-01", TotalCost: 1}}))
		require.NoError(t, testStorage.SetDailyProgress(ctx, app, models.DailyProgress{Through: "2026-10-01", Scope: models.ScopeApplication}))
	}
	require.NoError(t, testStorage.SaveCloudEstimate(ctx, teamAWeb, &models.CloudServiceEstimate{
		Source: "infracost", TotalMonthlyCost: 100.0, Currency: "USD", PushedAt: now,
	}))

	deleted, err := testStorage.DeleteMeasurements(ctx, teamAWeb)
	require.NoError(t, err)
	assert.True(t, deleted)

	_, err = testStorage.GetLatestActual(ctx, teamAWeb)
	require.ErrorIs(t, err, ErrNotFound)
	days, err := testStorage.GetDailyCosts(ctx, teamAWeb, models.ScopeApplication, "2000-01-01", "2100-01-01")
	require.NoError(t, err)
	assert.Empty(t, days)
	progress, err := testStorage.GetDailyProgress(ctx, teamAWeb)
	require.NoError(t, err)
	assert.Nil(t, progress)
	estimate, err := testStorage.GetCloudEstimate(ctx, teamAWeb)
	require.NoError(t, err)
	assert.NotNil(t, estimate, "cloud estimates come from CI and are kept")

	other, err := testStorage.GetLatestActual(ctx, teamBWeb)
	require.NoError(t, err)
	assert.Equal(t, "b1", other.ID, "team-b:web shares the name, not the deletion")
	days, err = testStorage.GetDailyCosts(ctx, teamBWeb, models.ScopeApplication, "2000-01-01", "2100-01-01")
	require.NoError(t, err)
	assert.Len(t, days, 1)

	deleted, err = testStorage.DeleteMeasurements(ctx, teamAWeb)
	require.NoError(t, err)
	assert.False(t, deleted, "nothing is left to delete")
}

func TestListLatestActuals_OneRecordPerApplication(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	saveSameNamedApps(t, now)
	// team-a:web moved to another namespace at its latest sync.
	require.NoError(t, testStorage.SaveActual(ctx, &models.CostActual{
		ID: "a3", ApplicationName: "web", ApplicationNamespace: "team-a", DeploymentNamespace: "web-a2",
		SyncTimestamp: now, MeasuredAt: now,
	}))
	controllers := models.ManagedResources{{Kind: "Deployment", Name: "api", Namespace: "api"}}
	require.NoError(t, testStorage.SaveActual(ctx, &models.CostActual{
		ID: "api-1", ApplicationName: "api", ApplicationNamespace: "argocd", DeploymentNamespace: "api",
		SyncTimestamp: now, MeasuredAt: now, App: &models.AppMeasurement{Controllers: controllers},
	}))
	// Unreachable: no request can name an Application without a namespace.
	require.NoError(t, testStorage.SaveActual(ctx, &models.CostActual{
		ID: "orphan", ApplicationName: "lost", DeploymentNamespace: "lost", SyncTimestamp: now, MeasuredAt: now,
	}))

	got, err := testStorage.ListLatestActuals(ctx)
	require.NoError(t, err)

	byApp := map[string]models.CostActual{}
	for _, actual := range got {
		byApp[actual.ApplicationNamespace+":"+actual.ApplicationName] = actual
	}
	require.Len(t, byApp, 3)
	assert.Len(t, got, 3)
	assert.Equal(t, "web-a2", byApp["team-a:web"].DeploymentNamespace, "an application's latest record wins")
	assert.Nil(t, byApp["team-a:web"].App)
	require.NotNil(t, byApp["argocd:api"].App)
	assert.Equal(t, controllers, byApp["argocd:api"].App.Controllers)
}

func TestMigration008_AddsTheDailyHistoryTables(t *testing.T) {
	db, m := newDatabaseAt(t, 7)
	require.NoError(t, m.Migrate(8))
	exec(t, db, `INSERT INTO cost_daily (application_namespace, application_name, day, deployment_namespace, scope,
		total_cost, cpu_cost, memory_cost, network_cost, storage_cost) VALUES ('argocd', 'web', '2026-10-01', 'web', 'application', 1, 1, 0, 0, 0)`)
	exec(t, db, `INSERT INTO cost_daily_progress (application_namespace, application_name, recorded_through, scope)
		VALUES ('argocd', 'web', '2026-10-01', 'application')`)

	require.NoError(t, m.Migrate(7))
	var tables int
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_name IN ('cost_daily', 'cost_daily_progress')").Scan(&tables))
	assert.Zero(t, tables)
}

// ---- Measurements at install ----

func installRecord(id string, measuredAt time.Time) *models.CostActual {
	return &models.CostActual{
		ID: id, ApplicationName: "my-app", ApplicationNamespace: "argocd", DeploymentNamespace: "shop",
		MeasuredAt: measuredAt, MeasuredOn: models.MeasuredOnInstall, TotalMonthlyCost: 100,
		App: &models.AppMeasurement{Controllers: models.ManagedResources{{Kind: "Deployment", Name: "web"}}},
	}
}

func TestSaveFirstActual_StoresAMeasurementAtInstallWithoutASync(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	record := installRecord("install-1", now)
	idle := 12.5
	record.IdleMonthlyCost = &idle
	saved, err := testStorage.SaveFirstActual(ctx, record)
	require.NoError(t, err)
	assert.True(t, saved)

	got, err := testStorage.GetLatestActual(ctx, myApp)
	require.NoError(t, err)
	assert.Equal(t, "install-1", got.ID)
	assert.Equal(t, models.MeasuredOnInstall, got.MeasuredOn)
	assert.True(t, got.SyncTimestamp.IsZero())
	assert.Equal(t, now, got.MeasuredAt.UTC())
	var stored time.Time
	require.NoError(t, testStorage.db.QueryRowContext(ctx, "SELECT sync_timestamp FROM cost_actuals WHERE id = 'install-1'").Scan(&stored))
	assert.Equal(t, now, stored.UTC(), "the measurement time is stored in place of a sync, so older replicas can read the row")
	require.NotNil(t, got.App)
	assert.Equal(t, models.ManagedResources{{Kind: "Deployment", Name: "web"}}, got.App.Controllers)
	require.NotNil(t, got.IdleMonthlyCost)
	assert.InDelta(t, 12.5, *got.IdleMonthlyCost, 0.01)

	inRange, err := testStorage.GetActualsInDateRange(ctx, myApp, now.Add(-time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, inRange, 1)
	assert.Equal(t, models.MeasuredOnInstall, inRange[0].MeasuredOn)
	assert.True(t, inRange[0].SyncTimestamp.IsZero())

	inNamespace, err := testStorage.GetLatestActualsForNamespace(ctx, "shop")
	require.NoError(t, err)
	require.Len(t, inNamespace, 1)
	assert.Equal(t, models.MeasuredOnInstall, inNamespace[0].MeasuredOn)
}

func TestSaveFirstActual_NeverReplacesARecord(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, testStorage.SaveActual(ctx, &models.CostActual{
		ID: "sync-1", ApplicationName: "my-app", ApplicationNamespace: "argocd", DeploymentNamespace: "shop",
		SyncTimestamp: now.Add(-time.Hour), MeasuredAt: now.Add(-time.Hour), TotalMonthlyCost: 50,
	}))

	saved, err := testStorage.SaveFirstActual(ctx, installRecord("install-1", now))
	require.NoError(t, err)
	assert.False(t, saved)

	got, err := testStorage.GetLatestActual(ctx, myApp)
	require.NoError(t, err)
	assert.Equal(t, "sync-1", got.ID)
	assert.Equal(t, models.MeasuredOnSync, got.MeasuredOn, "a record saved without saying is a sync's")
	assert.InDelta(t, 50.0, got.TotalMonthlyCost, 0.01)

	// The same name in another namespace is another application.
	other := installRecord("install-2", now)
	other.ApplicationNamespace = "team-a"
	saved, err = testStorage.SaveFirstActual(ctx, other)
	require.NoError(t, err)
	assert.True(t, saved)
}

func TestLock_IsHeldByOneSessionAtATime(t *testing.T) {
	ctx := context.Background()
	unlock, err := testStorage.Lock(ctx, "test-lock")
	require.NoError(t, err)

	waiting, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err = testStorage.Lock(waiting, "test-lock")
	require.Error(t, err, "a second holder waits until its context ends")

	other, err := testStorage.Lock(ctx, "another-lock")
	require.NoError(t, err, "locks of other names are independent")
	other()

	unlock()
	again, err := testStorage.Lock(ctx, "test-lock")
	require.NoError(t, err)
	again()
}

func TestMigration009_AddsMeasuredOn(t *testing.T) {
	db, m := newDatabaseAt(t, 8)
	exec(t, db, `INSERT INTO cost_actuals (id, application_name, application_namespace, sync_timestamp, measured_at, total_monthly_cost)
		VALUES ('old', 'web', 'argocd', '2026-10-01 10:00:00', '2026-10-01 10:05:00', 1)`)

	require.NoError(t, m.Migrate(9))
	var measuredOn string
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT measured_on FROM cost_actuals WHERE id = 'old'").Scan(&measuredOn))
	assert.Equal(t, models.MeasuredOnSync, measuredOn, "existing records were measured on a sync")
	exec(t, db, `INSERT INTO cost_actuals (id, application_name, application_namespace, sync_timestamp, measured_at, total_monthly_cost, measured_on)
		VALUES ('install', 'api', 'argocd', '2026-10-02 10:00:00', '2026-10-02 10:00:00', 1, 'install')`)

	require.NoError(t, m.Migrate(8))
	var count int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM cost_actuals").Scan(&count))
	assert.Equal(t, 2, count, "down keeps every record")
}

func TestMigration010_AddsIdleMonthlyCost(t *testing.T) {
	db, m := newDatabaseAt(t, 9)
	exec(t, db, `INSERT INTO cost_actuals (id, application_name, application_namespace, sync_timestamp, measured_at, total_monthly_cost)
		VALUES ('old', 'web', 'argocd', '2026-10-01 10:00:00', '2026-10-01 10:05:00', 1)`)

	require.NoError(t, m.Migrate(10))
	var idle sql.NullFloat64
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT idle_monthly_cost FROM cost_actuals WHERE id = 'old'").Scan(&idle))
	assert.False(t, idle.Valid, "an existing record's idle cost was never measured, so it is not zero")
	exec(t, db, `INSERT INTO cost_actuals (id, application_name, application_namespace, sync_timestamp, measured_at, total_monthly_cost, idle_monthly_cost)
		VALUES ('new', 'api', 'argocd', '2026-10-02 10:00:00', '2026-10-02 10:00:00', 1, 0.5)`)

	require.NoError(t, m.Migrate(9))
	var count int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM cost_actuals").Scan(&count))
	assert.Equal(t, 2, count, "down keeps every record")
}
