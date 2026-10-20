// Package backfill measures, when the service starts, each Argo CD Application that has no cost record
// yet. Records are otherwise written after a sync, so without it a fresh install shows no costs until
// every application syncs again.
//
// A backfilled record is the one the sync hook writes, from the same OpenCost queries and with the
// application's share taken from the controllers in its status, but measured at install: its sync
// timestamp is empty and it is never the baseline of a change since the last sync. It is only ever a
// first record. An application that has one, even one a sync saved while the backfill ran, keeps it.
//
// Every API replica runs the backfill when it starts, one at a time: a replica waits for another's to
// finish, then measures only what is still missing, so OpenCost is not queried once per replica.
package backfill

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/hook"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/validation"
)

// lockName is the lock replicas take turns on.
const lockName = "startup-backfill"

// RunTimeout bounds one run, waiting for the lock included, so a dependency that hangs cannot keep
// the lock other replicas wait on.
const RunTimeout = 15 * time.Minute

// RetryDelays are the waits before running again while a run left Applications unmeasured: on a fresh
// install OpenCost may start alongside the service and have no data, or not answer, at first.
var RetryDelays = []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}

// Store is the storage the backfill reads and writes.
type Store interface {
	ListLatestActuals(ctx context.Context) ([]models.CostActual, error)
	SaveFirstActual(ctx context.Context, actual *models.CostActual) (bool, error)
	Lock(ctx context.Context, name string) (unlock func(), err error)
	DeleteMeasurements(ctx context.Context, app models.AppRef) (bool, error)
}

// Result counts the Applications a run saw, by what it did with them.
type Result struct {
	// Saved got their first record.
	Saved int
	// Recorded already had a record, or got one from a sync during the run.
	Recorded int
	// Remote deploy to a cluster other than the one OpenCost measures.
	Remote int
	// Removed are the Remote ones whose stored cost PruneRemote deleted: it measured a namespace of
	// this cluster, not theirs.
	Removed int
	// NoNamespace have no valid destination namespace to measure.
	NoNamespace int
	// NoData deploy to a namespace OpenCost has no data for yet.
	NoData int
	// Failed could not be measured or saved; a retry or the next start tries again.
	Failed int
}

func (r *Result) add(o Result) {
	r.Saved += o.Saved
	r.Recorded += o.Recorded
	r.Remote += o.Remote
	r.Removed += o.Removed
	r.NoNamespace += o.NoNamespace
	r.NoData += o.NoData
	r.Failed += o.Failed
}

// Backfill measures the Applications without a cost record.
type Backfill struct {
	apps        exporter.ApplicationLister
	store       Store
	client      *opencost.Client
	concurrency int
	logger      *log.Logger
	// pruneRemote deletes the stored cost of the Applications deploying to another cluster.
	pruneRemote bool
}

// New returns a backfill measuring up to concurrency namespaces at once.
func New(apps exporter.ApplicationLister, store Store, client *opencost.Client, concurrency int, logger *log.Logger) *Backfill {
	return &Backfill{apps: apps, store: store, client: client, concurrency: max(concurrency, 1), logger: logger}
}

// PruneRemote has each run also delete the stored cost of the Applications that deploy to another
// cluster. It can't be undone, so it is off unless asked for.
func (b *Backfill) PruneRemote() *Backfill {
	b.pruneRemote = true
	return b
}

// Run measures every Application without a cost record, and with PruneRemote deletes the stored cost
// of those that deploy to another cluster, once another replica's run, if any, is over. A namespace
// that fails is counted in Result.Failed rather than returned; the error is for what stops the
// whole run.
func (b *Backfill) Run(ctx context.Context) (Result, error) {
	unlock, err := b.store.Lock(ctx, lockName)
	if err != nil {
		return Result{}, fmt.Errorf("taking the backfill lock: %w", err)
	}
	defer unlock()

	apps, err := b.apps.List(ctx)
	if err != nil {
		return Result{}, err
	}
	recorded, err := b.store.ListLatestActuals(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("listing cost records: %w", err)
	}

	var result Result
	byNamespace, stale := pending(apps, recorded, &result)
	if !b.pruneRemote {
		stale = nil
	}
	for _, app := range stale {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		deleted, err := b.store.DeleteMeasurements(ctx, app)
		switch {
		case err != nil:
			b.logger.Printf("Backfill: deleting the stored cost of %s, which deploys to another cluster: %v", app, err)
			result.Failed++
		case deleted:
			result.Removed++
		}
	}
	namespaces := make([]string, 0, len(byNamespace))
	for ns := range byNamespace {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, b.concurrency)
	)
	for _, ns := range namespaces {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return result, ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()
			counts := b.measure(ctx, ns, byNamespace[ns])
			mu.Lock()
			result.add(counts)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return result, ctx.Err()
}

// RunWithRetries runs the backfill, then again after each of delays while a run fails or leaves
// namespaces without OpenCost data. Each run is bounded by RunTimeout and measures only what is still
// missing. report is called after each run, and not once ctx is done.
func (b *Backfill) RunWithRetries(ctx context.Context, delays []time.Duration, report func(Result, error)) {
	for attempt := 0; ; attempt++ {
		runCtx, cancel := context.WithTimeout(ctx, RunTimeout)
		result, err := b.Run(runCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		report(result, err)
		if err == nil && result.NoData == 0 && result.Failed == 0 || attempt == len(delays) {
			return
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

// pending groups the Applications to measure by the namespace they deploy to, and counts the others in
// result by why they are left out. stale are the Applications deploying to another cluster that have
// a cost record anyway, stored before such Applications were skipped.
func pending(apps []exporter.Application, recorded []models.CostActual, result *Result) (byNamespace map[string][]exporter.Application, stale []models.AppRef) {
	has := make(map[models.AppRef]bool, len(recorded))
	for _, r := range recorded {
		has[models.AppRef{Namespace: r.ApplicationNamespace, Name: r.ApplicationName}] = true
	}
	byNamespace = map[string][]exporter.Application{}
	for _, app := range apps {
		switch ns := app.Destination.Namespace; {
		case !app.Destination.Local():
			// OpenCost measures the cluster it runs in; another cluster's namespace of the same name
			// would be measured here.
			result.Remote++
			if has[app.Ref] {
				stale = append(stale, app.Ref)
			}
		case has[app.Ref]:
			result.Recorded++
		case ns == "" || !validation.IsValidNamespace(ns):
			// The sync hook falls back to the Application's own namespace, where it does not deploy.
			result.NoNamespace++
		default:
			byNamespace[ns] = append(byNamespace[ns], app)
		}
	}
	return byNamespace, stale
}

// measure measures one namespace and saves the first record of each Application deploying there.
func (b *Backfill) measure(ctx context.Context, namespace string, apps []exporter.Application) Result {
	var result Result
	measured, err := hook.MeasureNamespace(ctx, b.client, namespace, b.logger)
	if err != nil {
		b.logger.Printf("Backfill: measuring namespace %s: %v", namespace, err)
		result.Failed = len(apps)
		return result
	}
	if !measured.HasData() {
		// An empty answer is unknown cost, not zero; a later sync or start measures it.
		result.NoData = len(apps)
		return result
	}
	for _, app := range apps {
		owners := app.Workloads
		if owners == nil {
			owners = models.ManagedResources{}
		}
		actual := measured.Record(app.Ref, owners, time.Time{}, models.MeasuredOnInstall)
		saved, err := b.store.SaveFirstActual(ctx, actual)
		switch {
		case err != nil:
			b.logger.Printf("Backfill: saving %s: %v", app.Ref, err)
			result.Failed++
		case saved:
			result.Saved++
		default:
			result.Recorded++
		}
	}
	return result
}
