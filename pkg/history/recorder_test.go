package history

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

var (
	web         = models.AppRef{Namespace: "argocd", Name: "web"}
	worker      = models.AppRef{Namespace: "argocd", Name: "worker"}
	webDeploy   = models.ManagedResources{{Kind: "Deployment", Name: "web", Namespace: "shop"}}
	quietLogger = log.New(io.Discard, "", 0)
)

func newTestRecorder(store Store, source Source, now string) *Recorder {
	r := NewRecorder(store, source, 400, quietLogger)
	r.setNow(now)
	return r
}

func (r *Recorder) setNow(now string) {
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		panic(err)
	}
	r.now = func() time.Time { return at }
}

func TestRecordOnce_BackfillsWhatOpenCostStillHas(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", webDeploy))
	source := &fakeOpenCost{costs: costsFrom("shop", "2026-08-20", "2026-10-03", 2)}
	recorder := newTestRecorder(store, source, "2026-10-03T12:00:00Z")

	require.NoError(t, recorder.RecordOnce(context.Background()))

	// Newest chunk first, back until a chunk without cost, which is not stored.
	assert.Equal(t, []call{
		{"shop", "2026-09-03", "2026-10-02", models.ScopeApplication},
		{"shop", "2026-08-04", "2026-09-02", models.ScopeApplication},
		{"shop", "2026-07-05", "2026-08-03", models.ScopeApplication},
	}, source.calls)
	dates := store.storedDates(web)
	assert.Equal(t, "2026-08-20", dates[0])
	assert.Equal(t, "2026-10-02", dates[len(dates)-1], "today is not over, so it is not recorded")
	assert.Len(t, dates, 44)
	assert.Equal(t, models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeApplication}, store.progress[web])
	assert.Equal(t, storedDay{
		cost:      models.DailyCost{Date: "2026-09-15", TotalCost: 2, CPUCost: 2},
		namespace: "shop",
		scope:     models.ScopeApplication,
	}, store.days[web]["2026-09-15"])
}

func TestRecordOnce_AsksOnlyForTheDaysNotYetRecorded(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", webDeploy))
	source := &fakeOpenCost{costs: costsFrom("shop", "2026-09-01", "2026-10-10", 2)}
	recorder := newTestRecorder(store, source, "2026-10-03T12:00:00Z")
	require.NoError(t, recorder.RecordOnce(context.Background()))
	source.calls = nil

	// Later the same day there is nothing to do.
	recorder.setNow("2026-10-03T23:00:00Z")
	require.NoError(t, recorder.RecordOnce(context.Background()))
	assert.Empty(t, source.calls)

	// A day is recorded an hour after it ends, so its last minutes are in.
	recorder.setNow("2026-10-04T00:30:00Z")
	require.NoError(t, recorder.RecordOnce(context.Background()))
	assert.Empty(t, source.calls)

	recorder.setNow("2026-10-04T01:00:00Z")
	require.NoError(t, recorder.RecordOnce(context.Background()))
	assert.Equal(t, []call{{"shop", "2026-10-03", "2026-10-03", models.ScopeApplication}}, source.calls)
	assert.Equal(t, "2026-10-03", store.progress[web].Through)
	assert.Contains(t, store.storedDates(web), "2026-10-03")
}

func TestRecordOnce_CatchesUpAChunkAtATime(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", webDeploy))
	store.progress[web] = models.DailyProgress{Through: "2026-08-01", Scope: models.ScopeApplication}
	source := &fakeOpenCost{costs: costsFrom("shop", "2026-08-01", "2026-10-03", 2)}
	recorder := newTestRecorder(store, source, "2026-10-03T12:00:00Z")

	require.NoError(t, recorder.RecordOnce(context.Background()))

	assert.Equal(t, []call{
		{"shop", "2026-08-02", "2026-08-31", models.ScopeApplication},
		{"shop", "2026-09-01", "2026-09-30", models.ScopeApplication},
		{"shop", "2026-10-01", "2026-10-02", models.ScopeApplication},
	}, source.calls)
	assert.Equal(t, "2026-10-02", store.progress[web].Through)
}

func TestRecordOnce_RecordsAgainWhenTheScopeChanges(t *testing.T) {
	t.Parallel()

	// The days were recorded for the whole namespace; the app's latest sync listed its controllers.
	store := newFakeStore(record(web, "shop", webDeploy))
	store.progress[web] = models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeNamespace}
	require.NoError(t, store.SaveDailyCosts(context.Background(), web, "shop", models.ScopeNamespace,
		[]models.DailyCost{{Date: "2026-10-01", TotalCost: 9}}))
	source := &fakeOpenCost{costs: costsFrom("shop", "2026-09-25", "2026-10-03", 2)}
	recorder := newTestRecorder(store, source, "2026-10-03T12:00:00Z")

	require.NoError(t, recorder.RecordOnce(context.Background()))

	require.NotEmpty(t, source.calls)
	assert.Equal(t, call{"shop", "2026-09-03", "2026-10-02", models.ScopeApplication}, source.calls[0])
	assert.Equal(t, models.ScopeApplication, store.progress[web].Scope)
	assert.Equal(t, storedDay{
		cost:      models.DailyCost{Date: "2026-10-01", TotalCost: 2, CPUCost: 2},
		namespace: "shop",
		scope:     models.ScopeApplication,
	}, store.days[web]["2026-10-01"])
}

func TestRecordOnce_MeasuresTheWholeNamespaceWithoutControllers(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", nil))
	source := &fakeOpenCost{costs: costsFrom("shop", "2026-10-01", "2026-10-03", 2)}

	require.NoError(t, newTestRecorder(store, source, "2026-10-03T12:00:00Z").RecordOnce(context.Background()))

	assert.Equal(t, models.ScopeNamespace, source.calls[0].scope)
	assert.Equal(t, "shop", source.calls[0].namespace)
	assert.Equal(t, "shop", store.days[web]["2026-10-01"].namespace, "each day notes the namespace it measured")
	assert.Equal(t, models.DailyProgress{Through: "2026-10-02", Scope: models.ScopeNamespace}, store.progress[web])
}

func TestRecordOnce_AnOpenCostFailureIsRetriedOnTheNextRun(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", webDeploy), record(worker, "jobs", nil))
	source := &fakeOpenCost{fail: errors.New("opencost unavailable")}
	recorder := newTestRecorder(store, source, "2026-10-03T12:00:00Z")

	err := recorder.RecordOnce(context.Background())
	require.ErrorContains(t, err, "argocd:web: opencost unavailable")
	require.ErrorContains(t, err, "argocd:worker: opencost unavailable", "one app's failure does not stop the others")
	assert.Empty(t, store.progress, "a backfill cut short is not marked recorded")

	source.fail = nil
	source.costs = costsFrom("shop", "2026-10-01", "2026-10-03", 2)
	require.NoError(t, recorder.RecordOnce(context.Background()))
	assert.Equal(t, []string{"2026-10-01", "2026-10-02"}, store.storedDates(web))
}

func TestRecordOnce_KeepsNothingOlderThanKeepDays(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", webDeploy))
	store.progress[web] = models.DailyProgress{Through: "2024-01-01", Scope: models.ScopeApplication}
	require.NoError(t, store.SaveDailyCosts(context.Background(), web, "shop", models.ScopeApplication,
		[]models.DailyCost{{Date: "2025-08-28", TotalCost: 1}, {Date: "2025-08-29", TotalCost: 1}}))
	source := &fakeOpenCost{}
	recorder := newTestRecorder(store, source, "2026-10-03T12:00:00Z")

	require.NoError(t, recorder.RecordOnce(context.Background()))

	// 400 days before 2026-10-03.
	assert.Equal(t, "2025-08-29", store.prunedTo)
	assert.Equal(t, []string{"2025-08-29"}, store.storedDates(web))
	assert.Equal(t, "2025-08-29", source.calls[0].from, "a long outage resumes from the oldest day kept")
	assert.Equal(t, "2025-09-27", source.calls[0].to, "30 days at a time")
}

func TestRecordOnce_SkipsRecordsWithoutAUsableNamespace(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "", webDeploy), record(worker, "Not_A_Namespace", nil))
	source := &fakeOpenCost{}

	require.NoError(t, newTestRecorder(store, source, "2026-10-03T12:00:00Z").RecordOnce(context.Background()))

	assert.Empty(t, source.calls)
	assert.Empty(t, store.progress)
}

func TestRecordOnce_StoresOnlyTheDaysItAskedFor(t *testing.T) {
	t.Parallel()

	store := newFakeStore(record(web, "shop", webDeploy))
	store.progress[web] = models.DailyProgress{Through: "2026-10-01", Scope: models.ScopeApplication}
	source := &extraDaySource{fakeOpenCost{costs: costsFrom("shop", "2026-10-01", "2026-10-03", 2)}}

	require.NoError(t, newTestRecorder(store, source, "2026-10-03T12:00:00Z").RecordOnce(context.Background()))

	assert.Equal(t, []string{"2026-10-02"}, store.storedDates(web))
}

// extraDaySource also returns the day after the range, which is still being measured.
type extraDaySource struct{ fakeOpenCost }

func (o *extraDaySource) GetDailyTimeSeries(
	ctx context.Context, namespace string, from, to time.Time, resources models.ManagedResources,
) ([]models.DailyCost, error) {
	return o.fakeOpenCost.GetDailyTimeSeries(ctx, namespace, from, to.AddDate(0, 0, 1), resources)
}

func TestRun_RecordsAtOnceAndStopsWithItsContext(t *testing.T) {
	t.Parallel()

	source := &signalSource{called: make(chan struct{}, 1)}
	recorder := newTestRecorder(newFakeStore(record(web, "shop", webDeploy)), source, "2026-10-03T12:00:00Z")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		recorder.Run(ctx)
		close(done)
	}()
	select {
	case <-source.called:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not record at once")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}

// signalSource reports its first query on called.
func TestRun_TellsTheObserverWhenEachRunEnds(t *testing.T) {
	t.Parallel()

	type run struct {
		at  time.Time
		err error
	}
	runs := make(chan run, 1)
	failing := &failingSource{err: errors.New("opencost returned status 500")}
	recorder := newTestRecorder(newFakeStore(record(web, "shop", webDeploy)), failing, "2026-10-03T12:00:00Z").
		OnRun(func(at time.Time, err error) { runs <- run{at, err} })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go recorder.Run(ctx)

	select {
	case got := <-runs:
		assert.Equal(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), got.at.UTC())
		require.ErrorIs(t, got.err, failing.err)
	case <-time.After(5 * time.Second):
		t.Fatal("the observer was not told about the first run")
	}
}

// failingSource fails every request.
type failingSource struct{ err error }

func (o *failingSource) GetDailyTimeSeries(context.Context, string, time.Time, time.Time, models.ManagedResources) ([]models.DailyCost, error) {
	return nil, o.err
}

type signalSource struct{ called chan struct{} }

func (o *signalSource) GetDailyTimeSeries(context.Context, string, time.Time, time.Time, models.ManagedResources) ([]models.DailyCost, error) {
	select {
	case o.called <- struct{}{}:
	default:
	}
	return nil, nil
}
