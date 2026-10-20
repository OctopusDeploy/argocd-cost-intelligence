package history

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/validation"
)

const (
	// Interval is how often the recorder runs. Most runs find nothing to do: a day is recorded once.
	Interval = time.Hour
	// settle is how long after a day ends it is recorded, so the last minutes' metrics are in.
	settle = time.Hour
	// chunkDays is the most days one OpenCost query asks for, as many as the Trends tab's default view.
	chunkDays = 30
)

// Store is the storage the recorder reads and writes.
type Store interface {
	Reader
	ListLatestActuals(ctx context.Context) ([]models.CostActual, error)
	SaveDailyCosts(ctx context.Context, app models.AppRef, namespace, scope string, days []models.DailyCost) error
	SetDailyProgress(ctx context.Context, app models.AppRef, progress models.DailyProgress) error
	PruneDailyCosts(ctx context.Context, before string) error
	DeleteMeasurements(ctx context.Context, app models.AppRef) (bool, error)
}

// Recorder stores each application's cost per UTC day once the day is over, for every application
// with a cost record: one that has not synced for weeks still runs, and still costs. It measures an
// application as its latest record does, by the controllers Argo CD listed at that sync or by its
// whole namespace.
//
// Every run asks OpenCost only for the days after the last one recorded. An application's first run,
// or its first after its scope changed, backfills instead: it walks back from yesterday a chunk at a
// time until a chunk has no cost, which is where OpenCost's data or the application begins. Days
// older than keepDays are neither asked for nor kept.
//
// With an Application lister, an application that deploys to another cluster is not recorded and
// its cost records and history are deleted: OpenCost measures only this cluster, so what was stored
// for it measured a same-named namespace here.
//
// Several replicas may run recorders at once: they store the same figures for the same days, so the
// only cost is duplicate OpenCost queries, about one per application per day once history is in.
type Recorder struct {
	store    Store
	source   Source
	keepDays int
	logger   *log.Logger
	now      func() time.Time
	// applications, when set, lists the Applications so those deploying elsewhere are skipped.
	applications exporter.ApplicationLister
	// pruneRemote deletes the stored cost of the Applications deploying elsewhere.
	pruneRemote bool
	// observe, when set, is told when each run finishes and with what error.
	observe func(at time.Time, err error)
	// wake asks Run for a run before the next tick.
	wake chan struct{}
}

// NewRecorder returns a recorder that keeps keepDays days of history.
func NewRecorder(store Store, source Source, keepDays int, logger *log.Logger) *Recorder {
	return &Recorder{store: store, source: source, keepDays: keepDays, logger: logger, now: time.Now, wake: make(chan struct{}, 1)}
}

// RecordSoon asks Run to record again as soon as its current run, if any, is over, for applications
// that got their first record outside a sync. Several calls before that run ask for one run.
func (r *Recorder) RecordSoon() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// OnRun has observe told when each run finishes and with what error, for metrics.
func (r *Recorder) OnRun(observe func(at time.Time, err error)) *Recorder {
	r.observe = observe
	return r
}

// WithApplications has each run list the Applications and skip those that deploy to another cluster.
// A run whose list fails records every application, as without a lister, and deletes nothing.
func (r *Recorder) WithApplications(applications exporter.ApplicationLister) *Recorder {
	r.applications = applications
	return r
}

// PruneRemote has each run also delete the cost records and history of the Applications that
// WithApplications finds on another cluster. It can't be undone, so it is off unless asked for.
func (r *Recorder) PruneRemote() *Recorder {
	r.pruneRemote = true
	return r
}

// Run records at once and then every Interval, or sooner when RecordSoon asks, until ctx is done.
func (r *Recorder) Run(ctx context.Context) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		err := r.RecordOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.logger.Printf("Daily cost history: %v", err)
		}
		if r.observe != nil {
			r.observe(r.now(), err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
	}
}

// RecordOnce records every application's days that are over and not yet recorded, then prunes days
// older than keepDays. A failure for one application does not stop the others; it is retried on the
// next run.
func (r *Recorder) RecordOnce(ctx context.Context) error {
	now := r.now().UTC()
	last := utcDay(now.Add(-settle)).AddDate(0, 0, -1)
	earliest := utcDay(now).AddDate(0, 0, -r.keepDays)

	var errs []error
	remote, err := r.remoteApplications(ctx)
	if err != nil {
		// History keeps being recorded without the list, as before Applications were listed; only
		// the deletes wait for a run that can list.
		errs = append(errs, err)
	}
	records, err := r.store.ListLatestActuals(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	// Deleting before the measuring, which can take minutes, keeps the list fresh for the deletes.
	for i := range records {
		app := models.AppRef{Namespace: records[i].ApplicationNamespace, Name: records[i].ApplicationName}
		if r.pruneRemote && remote[app] {
			if err := r.forget(ctx, app); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", app, err))
			}
		}
	}
	for i := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		app := models.AppRef{Namespace: records[i].ApplicationNamespace, Name: records[i].ApplicationName}
		if remote[app] {
			continue
		}
		if err := r.recordApp(ctx, &records[i], earliest, last); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", app, err))
		}
	}
	if err := r.store.PruneDailyCosts(ctx, formatDay(earliest)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// remoteApplications returns the Applications that deploy to another cluster, or none without a
// lister. An Application missing from the list, one deleted since its last record, is not among them.
func (r *Recorder) remoteApplications(ctx context.Context) (map[models.AppRef]bool, error) {
	remote := map[models.AppRef]bool{}
	if r.applications == nil {
		return remote, nil
	}
	apps, err := r.applications.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list applications: %w", err)
	}
	for _, app := range apps {
		if !app.Destination.Local() {
			remote[app.Ref] = true
		}
	}
	return remote, nil
}

// forget deletes the cost records and history of an application that deploys to another cluster.
func (r *Recorder) forget(ctx context.Context, app models.AppRef) error {
	deleted, err := r.store.DeleteMeasurements(ctx, app)
	if err != nil {
		return err
	}
	if deleted {
		r.logger.Printf("Deleted the stored cost of %s: it deploys to another cluster, which is not measured", app)
	}
	return nil
}

// recordApp records one application's days from earliest through last.
func (r *Recorder) recordApp(ctx context.Context, record *models.CostActual, earliest, last time.Time) error {
	namespace := record.DeploymentNamespace
	if namespace == "" || !validation.IsValidNamespace(namespace) {
		return nil
	}
	target := Target{
		App:       models.AppRef{Namespace: record.ApplicationNamespace, Name: record.ApplicationName},
		Namespace: namespace,
		Resources: record.MeasuredResources(),
	}
	scope := models.ScopeOf(target.Resources)

	progress, err := r.store.GetDailyProgress(ctx, target.App)
	if err != nil {
		return err
	}
	if progress == nil || progress.Scope != scope {
		return r.backfill(ctx, target, scope, earliest, last)
	}

	through, err := time.Parse(time.DateOnly, progress.Through)
	if err != nil {
		return fmt.Errorf("invalid recorded day %q: %w", progress.Through, err)
	}
	from := through.AddDate(0, 0, 1)
	if from.Before(earliest) {
		from = earliest
	}
	for start := from; !start.After(last); start = start.AddDate(0, 0, chunkDays) {
		end := minDay(start.AddDate(0, 0, chunkDays-1), last)
		days, err := r.fetch(ctx, target, start, end)
		if err != nil {
			return err
		}
		if err := r.store.SaveDailyCosts(ctx, target.App, target.Namespace, scope, days); err != nil {
			return err
		}
		if err := r.store.SetDailyProgress(ctx, target.App, models.DailyProgress{Through: formatDay(end), Scope: scope}); err != nil {
			return err
		}
	}
	return nil
}

// backfill records the days OpenCost still has, newest chunk first, and only then marks them
// recorded: a backfill cut short starts over on the next run instead of leaving a hole.
func (r *Recorder) backfill(ctx context.Context, target Target, scope string, earliest, last time.Time) error {
	for end := last; !end.Before(earliest); end = end.AddDate(0, 0, -chunkDays) {
		start := maxDay(end.AddDate(0, 0, -(chunkDays-1)), earliest)
		days, err := r.fetch(ctx, target, start, end)
		if err != nil {
			return err
		}
		if !hasCost(days) {
			break
		}
		if err := r.store.SaveDailyCosts(ctx, target.App, target.Namespace, scope, days); err != nil {
			return err
		}
	}
	return r.store.SetDailyProgress(ctx, target.App, models.DailyProgress{Through: formatDay(last), Scope: scope})
}

// fetch asks OpenCost for the target's days from start through end.
func (r *Recorder) fetch(ctx context.Context, target Target, start, end time.Time) ([]models.DailyCost, error) {
	days, err := r.source.GetDailyTimeSeries(ctx, target.Namespace, start, end, target.Resources)
	if err != nil {
		return nil, err
	}
	return within(days, start, end), nil
}

// hasCost reports whether any of days cost anything. Before OpenCost has data for a namespace it
// reports its days at exactly $0.
func hasCost(days []models.DailyCost) bool {
	for _, day := range days {
		if day.TotalCost != 0 {
			return true
		}
	}
	return false
}

func minDay(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxDay(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
