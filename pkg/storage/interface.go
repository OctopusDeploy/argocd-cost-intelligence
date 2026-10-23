package storage

import (
	"context"
	"errors"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// ErrNotFound is wrapped by the errors of lookups that found nothing, which is an answer, not a
// failure.
var ErrNotFound = errors.New("not found")

// ErrNoSchema is wrapped by the errors of reads from a table the API's migrations have not created yet,
// as when the exporter starts on a fresh install before the API has run them.
var ErrNoSchema = errors.New("schema not created yet")

// CostStorage defines the interface for storing and retrieving cost data.
//
// Per-application data is keyed by models.AppRef, the Application's namespace and name: with
// apps-in-any-namespace, two Applications in different namespaces can share a name.
type CostStorage interface {
	// InitializeSchema sets up the database schema
	InitializeSchema(ctx context.Context) error

	// SaveActual stores an actual cost record
	SaveActual(ctx context.Context, actual *models.CostActual) error

	// SaveFirstActual stores actual only when its application has no committed cost record, and
	// reports whether it did. It never replaces a record; a sync saved in the same instant may be
	// stored beside it, and the later measurement is then the latest.
	SaveFirstActual(ctx context.Context, actual *models.CostActual) (bool, error)

	// SaveActualUnlessSuperseded stores actual, a sync's record, only when no later sync of its
	// application has a record, and reports whether it did. A record measured at install does not count.
	// A later sync saved in the same instant may be stored beside it.
	SaveActualUnlessSuperseded(ctx context.Context, actual *models.CostActual) (bool, error)

	// Lock waits until this process holds the named lock, shared by every replica using the same
	// database, and returns the function that releases it. The lock is released if the process dies.
	Lock(ctx context.Context, name string) (unlock func(), err error)

	// GetLatestActual retrieves the most recent cost measurement for an application
	GetLatestActual(ctx context.Context, app models.AppRef) (*models.CostActual, error)

	// GetActualsInDateRange retrieves an application's cost records within a date range
	GetActualsInDateRange(ctx context.Context, app models.AppRef, startTime, endTime time.Time) ([]models.CostActual, error)

	// GetNamespaceCostSummary returns the average monthly cost of a deployment namespace over the
	// last `days` days: each day counts once, at its latest measurement, however many apps deploy there.
	GetNamespaceCostSummary(ctx context.Context, namespace string, days int) (float64, error)

	// ListNamespaces returns each deployment namespace's cost and idle cost from its most recent
	// measurement, with the number of applications currently deploying to it. A namespace shared by
	// several applications is counted once. The idle cost is nil when that measurement did not measure it.
	ListNamespaces(ctx context.Context) ([]models.NamespaceSummary, error)

	// GetLatestActualsForNamespace returns the most recent CostActual for each
	// application in the given deployment namespace.
	GetLatestActualsForNamespace(ctx context.Context, namespace string) ([]models.CostActual, error)

	// GetRecentActuals returns up to limit recent cost records for an application,
	// ordered by measured_at DESC. Used for rolling-average anomaly detection.
	// Only ID, TotalMonthlyCost, MeasuredAt and, when the record has one, App.TotalMonthlyCost are
	// populated in the returned records.
	GetRecentActuals(ctx context.Context, app models.AppRef, limit int) ([]models.CostActual, error)

	// SaveCloudEstimate stores or replaces the latest cloud service cost estimate for an
	// application and source (e.g. "infracost"). Upserts on (app_namespace, app_name, source).
	SaveCloudEstimate(ctx context.Context, app models.AppRef, estimate *models.CloudServiceEstimate) error

	// GetCloudEstimate returns the most recently pushed cloud estimate for an application,
	// regardless of source. Returns nil, nil when no estimate exists.
	GetCloudEstimate(ctx context.Context, app models.AppRef) (*models.CloudServiceEstimate, error)

	// ListCloudEstimates returns every application's estimate from each source, without the
	// per-resource breakdown, ordered by application and source. The error wraps ErrNoSchema
	// while the table does not exist yet.
	ListCloudEstimates(ctx context.Context) ([]models.AppCloudEstimate, error)

	// ListLatestActuals returns each application's most recent cost record. Only ApplicationNamespace,
	// ApplicationName, DeploymentNamespace, MeasuredAt and App are populated.
	ListLatestActuals(ctx context.Context) ([]models.CostActual, error)

	// SaveDailyCosts stores an application's cost for each of days (UTC dates), measured in
	// namespace and scope, replacing what was stored for those days.
	SaveDailyCosts(ctx context.Context, app models.AppRef, namespace, scope string, days []models.DailyCost) error

	// GetDailyCosts returns an application's stored days in scope from from through to (UTC dates,
	// both included), oldest first.
	GetDailyCosts(ctx context.Context, app models.AppRef, scope, from, to string) ([]models.DailyCost, error)

	// GetDailyProgress returns how far an application's daily history has been recorded, or nil, nil
	// when nothing has been.
	GetDailyProgress(ctx context.Context, app models.AppRef) (*models.DailyProgress, error)

	// SetDailyProgress records how far an application's daily history has been recorded.
	SetDailyProgress(ctx context.Context, app models.AppRef, progress models.DailyProgress) error

	// PruneDailyCosts deletes every stored day before before (a UTC date).
	PruneDailyCosts(ctx context.Context, before string) error

	// DeleteMeasurements deletes an application's cost records and daily history, and reports whether
	// there were any. Its cloud estimates are kept: they come from CI, not from OpenCost.
	DeleteMeasurements(ctx context.Context, app models.AppRef) (bool, error)

	// Close closes the storage connection
	Close() error
}
