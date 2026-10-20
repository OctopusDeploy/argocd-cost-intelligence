package backfill

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
)

// pod is a pod OpenCost reports in a namespace, with its controller and daily cost.
type pod struct {
	name, kind, controller string
	cost                   float64
}

// fakeOpenCost serves OpenCost's allocation API for the namespaces in pods. A namespace it does not
// know has no data; one in broken answers 500. Each namespace query takes delay, and the most
// namespace queries in flight at once is kept.
type fakeOpenCost struct {
	pods   map[string][]pod
	broken map[string]bool
	delay  time.Duration

	mu        sync.Mutex
	queries   map[string]int
	inFlight  int
	maxFlight int
}

func (f *fakeOpenCost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ns := strings.TrimSuffix(strings.TrimPrefix(r.URL.Query().Get("filter"), `namespace:"`), `"`)
	f.mu.Lock()
	broken, pods := f.broken[ns], f.pods[ns]
	f.mu.Unlock()
	if broken {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	end := time.Now()
	start := end.Add(-24 * time.Hour)
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == "/allocation/summary":
		_ = json.NewEncoder(w).Encode(opencost.SummaryAllocationResponse{Code: 200})
	case r.URL.Query().Get("aggregate") == "pod":
		allocations := map[string]opencost.AllocationData{}
		for _, p := range pods {
			allocations[p.name] = opencost.AllocationData{
				Start: start, End: end, CPUCost: p.cost, TotalCost: p.cost,
				Properties: opencost.AllocationProperties{Pod: p.name, Namespace: ns, ControllerKind: p.kind, Controller: p.controller},
			}
		}
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{allocations}})
	default:
		f.mu.Lock()
		if f.queries == nil {
			f.queries = map[string]int{}
		}
		f.queries[ns]++
		f.inFlight++
		f.maxFlight = max(f.maxFlight, f.inFlight)
		f.mu.Unlock()
		time.Sleep(f.delay)
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()

		allocations := map[string]opencost.AllocationData{}
		if len(pods) > 0 {
			var total float64
			for _, p := range pods {
				total += p.cost
			}
			allocations[ns] = opencost.AllocationData{Start: start, End: end, CPUCost: total, TotalCost: total}
		}
		_ = json.NewEncoder(w).Encode(opencost.AllocationResponse{Code: 200, Data: []map[string]opencost.AllocationData{allocations}})
	}
}

// setPods gives namespace ns data from now on.
func (f *fakeOpenCost) setPods(ns string, pods ...pod) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pods[ns] = pods
}

// fakeStore keeps records in memory. listed is what ListLatestActuals returns, by default the records
// themselves; a test sets it apart to have a record saved between the list and the save.
type fakeStore struct {
	mu      sync.Mutex
	records map[models.AppRef]*models.CostActual
	listed  []models.CostActual
	lockErr error
	locked  []string
	held    bool
}

func newFakeStore(records ...*models.CostActual) *fakeStore {
	s := &fakeStore{records: map[models.AppRef]*models.CostActual{}}
	for _, r := range records {
		s.records[ref(r)] = r
	}
	return s
}

func ref(r *models.CostActual) models.AppRef {
	return models.AppRef{Namespace: r.ApplicationNamespace, Name: r.ApplicationName}
}

func (s *fakeStore) ListLatestActuals(context.Context) ([]models.CostActual, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listed != nil {
		return s.listed, nil
	}
	listed := []models.CostActual{}
	for _, r := range s.records {
		listed = append(listed, *r)
	}
	return listed, nil
}

func (s *fakeStore) SaveFirstActual(_ context.Context, actual *models.CostActual) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.held {
		return false, errors.New("saved without the lock")
	}
	if _, ok := s.records[ref(actual)]; ok {
		return false, nil
	}
	s.records[ref(actual)] = actual
	return true, nil
}

func (s *fakeStore) Lock(_ context.Context, name string) (func(), error) {
	if s.lockErr != nil {
		return nil, s.lockErr
	}
	s.locked = append(s.locked, name)
	s.held = true
	return func() { s.held = false }, nil
}

type fakeApps []exporter.Application

func (a fakeApps) List(context.Context) ([]exporter.Application, error) { return a, nil }

func app(name string, dest exporter.Destination, workloads ...models.ManagedResource) exporter.Application {
	return exporter.Application{Ref: models.AppRef{Namespace: "argocd", Name: name}, Destination: dest, Workloads: workloads}
}

func local(ns string) exporter.Destination {
	return exporter.Destination{Server: exporter.LocalServer, Namespace: ns}
}

func deployment(ns, name string) models.ManagedResource {
	return models.ManagedResource{Kind: "Deployment", Name: name, Namespace: ns}
}

func run(t *testing.T, oc *fakeOpenCost, store *fakeStore, concurrency int, apps ...exporter.Application) Result {
	t.Helper()
	srv := httptest.NewServer(oc)
	t.Cleanup(srv.Close)
	b := New(fakeApps(apps), store, opencost.NewClient(srv.URL, 5*time.Second, nil), concurrency, log.New(io.Discard, "", 0))
	result, err := b.Run(context.Background())
	require.NoError(t, err)
	return result
}

func TestRun_MeasuresOnlyLocalApplicationsWithoutARecord(t *testing.T) {
	t.Parallel()

	existing := &models.CostActual{ID: "old", ApplicationNamespace: "argocd", ApplicationName: "recorded", MeasuredOn: models.MeasuredOnSync}
	store := newFakeStore(existing)
	oc := &fakeOpenCost{pods: map[string][]pod{
		"shop":     {{"web-1", "deployment", "web", 2}, {"db-0", "statefulset", "db", 3}},
		"payments": {{"api-1", "deployment", "api", 1}},
		"recorded": {{"x-1", "deployment", "x", 1}},
	}}

	result := run(t, oc, store, 4,
		app("recorded", local("recorded")),
		app("web", local("shop"), deployment("shop", "web")),
		app("api", exporter.Destination{Name: exporter.LocalName, Namespace: "payments"}, deployment("payments", "api")),
		app("prod-web", exporter.Destination{Server: "https://prod.example.com", Namespace: "shop"}),
		app("prod-api", exporter.Destination{Name: "prod", Namespace: "payments"}),
		app("cluster-wide", local("")),
		app("bad-namespace", local("Not_A_Namespace")),
		app("new", local("brand-new")),
	)

	assert.Equal(t, Result{Saved: 2, Recorded: 1, Remote: 2, NoNamespace: 2, NoData: 1}, result)
	assert.ElementsMatch(t, []models.AppRef{
		{Namespace: "argocd", Name: "recorded"},
		{Namespace: "argocd", Name: "web"},
		{Namespace: "argocd", Name: "api"},
	}, refs(store))
	assert.Same(t, existing, store.records[models.AppRef{Namespace: "argocd", Name: "recorded"}], "an existing record is never replaced")
	assert.Equal(t, map[string]int{"shop": 1, "payments": 1, "brand-new": 1}, oc.queries,
		"only the namespaces of local applications without a record are measured")
	assert.Equal(t, []string{lockName}, store.locked)
	assert.False(t, store.held, "the lock is released")
}

func TestRun_StoresTheSyncHooksRecordMarkedAsMeasuredAtInstall(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{pods: map[string][]pod{"shop": {{"web-1", "deployment", "web", 2}, {"db-0", "statefulset", "db", 3}}}}

	run(t, oc, store, 1, app("web", local("shop"), deployment("shop", "web")))

	saved := store.records[models.AppRef{Namespace: "argocd", Name: "web"}]
	require.NotNil(t, saved)
	assert.Equal(t, models.MeasuredOnInstall, saved.MeasuredOn)
	assert.True(t, saved.SyncTimestamp.IsZero(), "a measurement at install has no sync")
	assert.False(t, saved.MeasuredAt.IsZero())
	assert.NotEmpty(t, saved.ID)
	assert.Equal(t, "shop", saved.DeploymentNamespace)
	assert.InDelta(t, 150.0, saved.TotalMonthlyCost, 1e-6, "the namespace: (2+3) a day for 30 days")
	assert.Len(t, saved.Pods, 2)
	require.NotNil(t, saved.App, "the application's share is measured from its controllers")
	assert.Equal(t, models.ManagedResources{deployment("shop", "web")}, saved.App.Controllers)
	assert.InDelta(t, 60.0, saved.App.TotalMonthlyCost, 1e-6, "its own pod: 2 a day for 30 days")
}

func TestRun_AnApplicationWithoutWorkloadsIsMeasuredAsOwningNothing(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{pods: map[string][]pod{"shop": {{"web-1", "deployment", "web", 2}}}}

	run(t, oc, store, 1, app("config", local("shop")))

	saved := store.records[models.AppRef{Namespace: "argocd", Name: "config"}]
	require.NotNil(t, saved)
	require.NotNil(t, saved.App, "not the whole namespace")
	assert.Zero(t, saved.App.TotalMonthlyCost)
}

func TestRun_NeverReplacesARecordSavedDuringTheRun(t *testing.T) {
	t.Parallel()

	synced := &models.CostActual{ID: "sync", ApplicationNamespace: "argocd", ApplicationName: "web", MeasuredOn: models.MeasuredOnSync}
	store := newFakeStore(synced)
	store.listed = []models.CostActual{} // the sync landed after the backfill listed the records
	oc := &fakeOpenCost{pods: map[string][]pod{"shop": {{"web-1", "deployment", "web", 2}}}}

	result := run(t, oc, store, 1, app("web", local("shop"), deployment("shop", "web")))

	assert.Equal(t, Result{Recorded: 1}, result)
	assert.Same(t, synced, store.records[models.AppRef{Namespace: "argocd", Name: "web"}])
}

func TestRun_MeasuresEachNamespaceOnce(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{pods: map[string][]pod{"shop": {{"web-1", "deployment", "web", 2}, {"api-1", "deployment", "api", 1}}}}

	result := run(t, oc, store, 4,
		app("web", local("shop"), deployment("shop", "web")),
		app("api", local("shop"), deployment("shop", "api")),
	)

	assert.Equal(t, Result{Saved: 2}, result)
	assert.Equal(t, map[string]int{"shop": 1}, oc.queries)
	assert.InDelta(t, 60.0, store.records[models.AppRef{Namespace: "argocd", Name: "web"}].App.TotalMonthlyCost, 1e-6)
	assert.InDelta(t, 30.0, store.records[models.AppRef{Namespace: "argocd", Name: "api"}].App.TotalMonthlyCost, 1e-6)
}

func TestRun_BoundsTheNamespacesMeasuredAtOnce(t *testing.T) {
	t.Parallel()

	oc := &fakeOpenCost{pods: map[string][]pod{}, delay: 30 * time.Millisecond}
	namespaces := []string{"a", "b", "c", "d", "e", "f"}
	apps := make([]exporter.Application, 0, len(namespaces))
	for _, ns := range namespaces {
		oc.pods[ns] = []pod{{ns + "-1", "deployment", ns, 1}}
		apps = append(apps, app(ns, local(ns), deployment(ns, ns)))
	}

	result := run(t, oc, newFakeStore(), 2, apps...)

	assert.Equal(t, 6, result.Saved)
	assert.Equal(t, 2, oc.maxFlight)
}

func TestRun_AFailedNamespaceDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{
		pods:   map[string][]pod{"shop": {{"web-1", "deployment", "web", 2}}},
		broken: map[string]bool{"payments": true},
	}

	result := run(t, oc, store, 1,
		app("api", local("payments"), deployment("payments", "api")),
		app("web", local("shop"), deployment("shop", "web")),
	)

	assert.Equal(t, Result{Saved: 1, Failed: 1}, result)
	assert.Equal(t, []models.AppRef{{Namespace: "argocd", Name: "web"}}, refs(store))
}

func TestRun_WithoutTheLockMeasuresNothing(t *testing.T) {
	t.Parallel()

	var queries atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { queries.Add(1) }))
	t.Cleanup(srv.Close)
	store := newFakeStore()
	store.lockErr = context.DeadlineExceeded
	b := New(fakeApps{app("web", local("shop"))}, store, opencost.NewClient(srv.URL, time.Second, nil), 1, log.New(io.Discard, "", 0))

	_, err := b.Run(context.Background())

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, queries.Load())
	assert.Empty(t, store.records)
}

func newTestBackfill(t *testing.T, oc *fakeOpenCost, store *fakeStore, apps ...exporter.Application) *Backfill {
	t.Helper()
	srv := httptest.NewServer(oc)
	t.Cleanup(srv.Close)
	return New(fakeApps(apps), store, opencost.NewClient(srv.URL, 5*time.Second, nil), 1, log.New(io.Discard, "", 0))
}

func TestRunWithRetries_RunsAgainUntilOpenCostHasData(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{pods: map[string][]pod{}}
	b := newTestBackfill(t, oc, store, app("web", local("shop"), deployment("shop", "web")))

	var results []Result
	b.RunWithRetries(context.Background(), []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}, func(r Result, err error) {
		require.NoError(t, err)
		results = append(results, r)
		if len(results) == 2 {
			oc.setPods("shop", pod{"web-1", "deployment", "web", 2})
		}
	})

	assert.Equal(t, []Result{{NoData: 1}, {NoData: 1}, {Saved: 1}}, results, "stops once nothing is left")
	assert.Equal(t, []models.AppRef{{Namespace: "argocd", Name: "web"}}, refs(store))
}

func TestRunWithRetries_GivesUpAfterTheLastDelay(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{broken: map[string]bool{"shop": true}}
	b := newTestBackfill(t, oc, store, app("web", local("shop"), deployment("shop", "web")))

	var results []Result
	b.RunWithRetries(context.Background(), []time.Duration{time.Millisecond, time.Millisecond}, func(r Result, err error) {
		require.NoError(t, err)
		results = append(results, r)
	})

	assert.Equal(t, []Result{{Failed: 1}, {Failed: 1}, {Failed: 1}}, results)
}

func TestRunWithRetries_RetriesARunThatCouldNotStart(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.lockErr = errors.New("database unavailable")
	oc := &fakeOpenCost{pods: map[string][]pod{"shop": {{"web-1", "deployment", "web", 2}}}}
	b := newTestBackfill(t, oc, store, app("web", local("shop"), deployment("shop", "web")))

	var errs []error
	b.RunWithRetries(context.Background(), []time.Duration{time.Millisecond}, func(_ Result, err error) {
		errs = append(errs, err)
		store.lockErr = nil
	})

	require.Len(t, errs, 2)
	require.Error(t, errs[0])
	require.NoError(t, errs[1])
	assert.Len(t, store.records, 1)
}

func TestRunWithRetries_StopsWithoutReportingOnceCancelled(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	oc := &fakeOpenCost{pods: map[string][]pod{}}
	b := newTestBackfill(t, oc, store, app("web", local("shop"), deployment("shop", "web")))
	ctx, cancel := context.WithCancel(context.Background())

	reports := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.RunWithRetries(ctx, []time.Duration{time.Hour}, func(Result, error) {
			reports++
			cancel()
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting to retry after the context was cancelled")
	}
	assert.Equal(t, 1, reports)
}

func refs(s *fakeStore) []models.AppRef {
	out := make([]models.AppRef, 0, len(s.records))
	for r := range s.records {
		out = append(out, r)
	}
	return out
}
