package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

type PostgresCostStorage struct {
	db *sql.DB
}

// Option customises the connection pool of a PostgresCostStorage.
type Option func(*sql.DB)

// WithPool limits the connection pool. A value of zero or less leaves that limit at
// database/sql's default.
func WithPool(maxOpen, maxIdle int) Option {
	return func(db *sql.DB) {
		if maxOpen > 0 {
			db.SetMaxOpenConns(maxOpen)
		}
		if maxIdle > 0 {
			db.SetMaxIdleConns(maxIdle)
		}
	}
}

const (
	// connectTimeout is how long a starting service waits for the database. Pods of a new
	// install start at the same time as PostgreSQL, whose Service name does not resolve yet.
	connectTimeout  = 60 * time.Second
	connectInterval = 2 * time.Second
)

type pinger interface {
	PingContext(ctx context.Context) error
}

// pingUntilReady pings every interval until the database answers or ctx ends. It returns the
// last error that was not caused by ctx ending, so a ping cut short by the deadline does not
// hide why the database was unreachable.
func pingUntilReady(ctx context.Context, db pinger, interval time.Duration) error {
	var last error
	for attempt := 1; ; attempt++ {
		err := db.PingContext(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() == nil || last == nil {
			last = err
		}
		if attempt == 1 {
			log.Printf("Waiting for the database: %v", err)
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(interval):
		}
	}
}

func NewPostgresCostStorage(dsn string, opts ...Option) (*PostgresCostStorage, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	for _, opt := range opts {
		opt(db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := pingUntilReady(ctx, db, connectInterval); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresCostStorage{db: db}, nil
}

// lockClass is the first key of every advisory lock this service takes. PostgreSQL keeps two-key
// advisory locks apart from one-key ones, which is what the migrations take.
const lockClass = 0x636f7374 // "cost"

// Lock waits until this process holds the named lock, which every replica sharing the database
// contends for, and returns the function that releases it. The lock lives in a database session of its
// own, so it is released when the process dies too.
func (p *PostgresCostStorage) Lock(ctx context.Context, name string) (func(), error) {
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open a session for lock %q: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, hashtext($2))`, lockClass, name); err != nil {
		closeSession(conn)
		return nil, fmt.Errorf("failed to take lock %q: %w", name, err)
	}
	return func() { closeSession(conn) }, nil
}

// closeSession closes the connection rather than returning it to the pool: ending the session releases
// every lock it holds, including one a cancelled wait may have taken anyway.
func closeSession(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func (p *PostgresCostStorage) InitializeSchema(_ context.Context) error {
	if err := runMigrations(p.db); err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}
	return nil
}

func (p *PostgresCostStorage) SaveActual(ctx context.Context, actual *models.CostActual) error {
	query := `
	INSERT INTO cost_actuals
	(id, application_name, application_namespace, deployment_namespace, sync_timestamp, measured_at,
	 total_monthly_cost, estimate_accuracy, estimate_vs_actual,
	 resources_json, opencost_output_json, efficiency_json, pods_json, app_json, measured_on)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	ON CONFLICT (id) DO UPDATE SET
		total_monthly_cost = EXCLUDED.total_monthly_cost,
		estimate_accuracy = EXCLUDED.estimate_accuracy,
		estimate_vs_actual = EXCLUDED.estimate_vs_actual,
		resources_json = EXCLUDED.resources_json,
		opencost_output_json = EXCLUDED.opencost_output_json,
		efficiency_json = EXCLUDED.efficiency_json,
		pods_json = EXCLUDED.pods_json,
		app_json = EXCLUDED.app_json,
		deployment_namespace = EXCLUDED.deployment_namespace,
		measured_at = EXCLUDED.measured_at
	`

	args, err := actualColumns(actual)
	if err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to save actual cost: %w", err)
	}

	return nil
}

// SaveFirstActual stores actual only when its application has no committed cost record, checked and
// inserted in one statement. It reports whether it stored it.
func (p *PostgresCostStorage) SaveFirstActual(ctx context.Context, actual *models.CostActual) (bool, error) {
	// Parameters in a SELECT list have no column to take their type from, so each is cast.
	query := `
	INSERT INTO cost_actuals
	(id, application_name, application_namespace, deployment_namespace, sync_timestamp, measured_at,
	 total_monthly_cost, estimate_accuracy, estimate_vs_actual,
	 resources_json, opencost_output_json, efficiency_json, pods_json, app_json, measured_on)
	SELECT $1::varchar, $2::varchar, $3::varchar, $4::varchar, $5::timestamp, $6::timestamp,
	       $7::numeric, $8::numeric, $9::numeric,
	       $10::jsonb, $11::jsonb, $12::jsonb, $13::jsonb, $14::jsonb, $15::varchar
	WHERE NOT EXISTS (
	    SELECT 1 FROM cost_actuals WHERE application_namespace = $3 AND application_name = $2
	)
	`

	args, err := actualColumns(actual)
	if err != nil {
		return false, err
	}
	result, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("failed to save first actual cost: %w", err)
	}
	stored, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to save first actual cost: %w", err)
	}
	return stored == 1, nil
}

// actualColumns returns actual's values in the column order of the cost_actuals inserts. A record that
// does not say what measured it was measured on a sync. A measurement taken at install has no sync
// timestamp; its measurement time is stored in its place, so the column stays NOT NULL and a replica
// that predates measured_on can still read the row during a rolling upgrade.
func actualColumns(actual *models.CostActual) ([]any, error) {
	resourcesJSON, err := json.Marshal(actual.Resources)
	if err != nil {
		return nil, fmt.Errorf("failed to encode resources: %w", err)
	}
	opencostJSON, err := json.Marshal(actual.OpenCostOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to encode OpenCost output: %w", err)
	}
	efficiencyJSON, err := json.Marshal(actual.Efficiency)
	if err != nil {
		return nil, fmt.Errorf("failed to encode efficiency: %w", err)
	}
	podsJSON, err := json.Marshal(actual.Pods)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pods: %w", err)
	}
	var appJSON sql.NullString
	if actual.App != nil {
		encoded, err := json.Marshal(actual.App)
		if err != nil {
			return nil, fmt.Errorf("failed to encode application measurement: %w", err)
		}
		appJSON = sql.NullString{String: string(encoded), Valid: true}
	}
	syncTimestamp := actual.SyncTimestamp
	if syncTimestamp.IsZero() {
		syncTimestamp = actual.MeasuredAt
	}
	measuredOn := actual.MeasuredOn
	if measuredOn == "" {
		measuredOn = models.MeasuredOnSync
	}

	return []any{
		actual.ID,
		actual.ApplicationName,
		actual.ApplicationNamespace,
		actual.DeploymentNamespace,
		syncTimestamp,
		actual.MeasuredAt,
		actual.TotalMonthlyCost,
		actual.EstimateAccuracy,
		actual.EstimateVsActual,
		string(resourcesJSON),
		string(opencostJSON),
		string(efficiencyJSON),
		string(podsJSON),
		appJSON,
		measuredOn,
	}, nil
}

func (p *PostgresCostStorage) GetLatestActual(ctx context.Context, app models.AppRef) (*models.CostActual, error) {
	query := `
	SELECT id, application_name, application_namespace, deployment_namespace, sync_timestamp, measured_on, measured_at,
		   total_monthly_cost, estimate_accuracy, estimate_vs_actual,
		   resources_json, opencost_output_json, efficiency_json, pods_json, app_json
	FROM cost_actuals
	WHERE application_namespace = $1 AND application_name = $2
	ORDER BY measured_at DESC
	LIMIT 1
	`

	var actual models.CostActual
	var resourcesJSON, opencostJSON, efficiencyJSON, podsJSON, appJSON sql.NullString

	err := p.db.QueryRowContext(ctx, query, app.Namespace, app.Name).Scan(
		&actual.ID,
		&actual.ApplicationName,
		&actual.ApplicationNamespace,
		&actual.DeploymentNamespace,
		&actual.SyncTimestamp,
		&actual.MeasuredOn,
		&actual.MeasuredAt,
		&actual.TotalMonthlyCost,
		&actual.EstimateAccuracy,
		&actual.EstimateVsActual,
		&resourcesJSON,
		&opencostJSON,
		&efficiencyJSON,
		&podsJSON,
		&appJSON,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no cost actual found for application %s: %w", app, ErrNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query cost actual: %w", err)
	}
	clearInstallSync(&actual)

	if resourcesJSON.Valid {
		if err := json.Unmarshal([]byte(resourcesJSON.String), &actual.Resources); err != nil {
			return nil, fmt.Errorf("failed to decode resources JSON: %w", err)
		}
	}
	if opencostJSON.Valid {
		if err := json.Unmarshal([]byte(opencostJSON.String), &actual.OpenCostOutput); err != nil {
			return nil, fmt.Errorf("failed to decode opencost JSON: %w", err)
		}
	}
	if efficiencyJSON.Valid && efficiencyJSON.String != "null" {
		actual.Efficiency = &models.EfficiencyMetrics{}
		if err := json.Unmarshal([]byte(efficiencyJSON.String), actual.Efficiency); err != nil {
			return nil, fmt.Errorf("failed to decode efficiency JSON: %w", err)
		}
	}
	if podsJSON.Valid && podsJSON.String != "null" {
		if err := json.Unmarshal([]byte(podsJSON.String), &actual.Pods); err != nil {
			return nil, fmt.Errorf("failed to decode pods JSON: %w", err)
		}
	}
	if err := decodeAppMeasurement(appJSON, &actual); err != nil {
		return nil, err
	}

	return &actual, nil
}

// clearInstallSync zeroes the sync timestamp of a measurement taken at install: the column holds its
// measurement time, but it follows no sync.
func clearInstallSync(actual *models.CostActual) {
	if actual.MeasuredOn == models.MeasuredOnInstall {
		actual.SyncTimestamp = time.Time{}
	}
}

// decodeAppMeasurement sets actual.App from the app_json column, leaving it nil when the record has
// none.
func decodeAppMeasurement(appJSON sql.NullString, actual *models.CostActual) error {
	if !appJSON.Valid || appJSON.String == "null" {
		return nil
	}
	actual.App = &models.AppMeasurement{}
	if err := json.Unmarshal([]byte(appJSON.String), actual.App); err != nil {
		return fmt.Errorf("failed to decode application measurement JSON: %w", err)
	}
	return nil
}

func (p *PostgresCostStorage) GetActualsInDateRange(ctx context.Context, app models.AppRef, startTime, endTime time.Time) ([]models.CostActual, error) {
	query := `
	SELECT id, application_name, application_namespace, deployment_namespace, sync_timestamp, measured_on, measured_at,
		   total_monthly_cost, estimate_accuracy, estimate_vs_actual,
		   resources_json, opencost_output_json, efficiency_json, pods_json, app_json
	FROM cost_actuals
	WHERE application_namespace = $1 AND application_name = $2 AND measured_at BETWEEN $3 AND $4
	ORDER BY measured_at DESC
	`

	rows, err := p.db.QueryContext(ctx, query, app.Namespace, app.Name, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query cost actuals: %w", err)
	}
	defer rows.Close()

	var actuals []models.CostActual
	for rows.Next() {
		var actual models.CostActual
		var resourcesJSON, opencostJSON, efficiencyJSON, podsJSON, appJSON sql.NullString

		if err := rows.Scan(
			&actual.ID,
			&actual.ApplicationName,
			&actual.ApplicationNamespace,
			&actual.DeploymentNamespace,
			&actual.SyncTimestamp,
			&actual.MeasuredOn,
			&actual.MeasuredAt,
			&actual.TotalMonthlyCost,
			&actual.EstimateAccuracy,
			&actual.EstimateVsActual,
			&resourcesJSON,
			&opencostJSON,
			&efficiencyJSON,
			&podsJSON,
			&appJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan cost actual: %w", err)
		}
		clearInstallSync(&actual)

		if resourcesJSON.Valid {
			if err := json.Unmarshal([]byte(resourcesJSON.String), &actual.Resources); err != nil {
				return nil, fmt.Errorf("failed to decode resources JSON: %w", err)
			}
		}
		if opencostJSON.Valid {
			if err := json.Unmarshal([]byte(opencostJSON.String), &actual.OpenCostOutput); err != nil {
				return nil, fmt.Errorf("failed to decode opencost JSON: %w", err)
			}
		}
		if efficiencyJSON.Valid && efficiencyJSON.String != "null" {
			actual.Efficiency = &models.EfficiencyMetrics{}
			if err := json.Unmarshal([]byte(efficiencyJSON.String), actual.Efficiency); err != nil {
				return nil, fmt.Errorf("failed to decode efficiency JSON: %w", err)
			}
		}
		if podsJSON.Valid && podsJSON.String != "null" {
			if err := json.Unmarshal([]byte(podsJSON.String), &actual.Pods); err != nil {
				return nil, fmt.Errorf("failed to decode pods JSON: %w", err)
			}
		}
		if err := decodeAppMeasurement(appJSON, &actual); err != nil {
			return nil, err
		}

		actuals = append(actuals, actual)
	}

	return actuals, rows.Err()
}

func (p *PostgresCostStorage) GetNamespaceCostSummary(ctx context.Context, namespace string, days int) (float64, error) {
	// Every record measures its app's whole deployment namespace, so the apps sharing a namespace
	// each record the same cost. Averaging every record would weight a day by how many apps synced
	// on it. Each day counts once instead, at its latest measurement (OpenCost's 24h window makes
	// that the day's best reading), and the summary is the average over those days.
	query := `
	WITH daily AS (
	    SELECT DISTINCT ON (date_trunc('day', measured_at)) total_monthly_cost
	    FROM cost_actuals
	    WHERE deployment_namespace = $1 AND measured_at > NOW() - INTERVAL '1 day' * $2::INTEGER
	    ORDER BY date_trunc('day', measured_at), measured_at DESC, id
	)
	SELECT COALESCE(AVG(total_monthly_cost), 0) FROM daily
	`

	var avgCost float64
	err := p.db.QueryRowContext(ctx, query, namespace, days).Scan(&avgCost)
	if err != nil {
		return 0, fmt.Errorf("failed to get namespace cost summary: %w", err)
	}

	return avgCost, nil
}

func (p *PostgresCostStorage) ListNamespaces(ctx context.Context) ([]models.NamespaceSummary, error) {
	// Every record measures its app's whole deployment namespace, so a namespace costs what its
	// most recent measurement says; summing the records of apps that share it would count its
	// pods once per app. Each app counts by its latest record only, so an app that moved
	// namespaces is not counted where it used to be. An app is its Application namespace and name.
	query := `
	WITH latest_per_app AS (
	    SELECT DISTINCT ON (application_namespace, application_name)
	        application_namespace, application_name, deployment_namespace, total_monthly_cost,
	        efficiency_json, measured_at
	    FROM cost_actuals
	    WHERE deployment_namespace <> ''
	    ORDER BY application_namespace, application_name, measured_at DESC
	),
	latest_per_namespace AS (
	    SELECT DISTINCT ON (deployment_namespace)
	        deployment_namespace, total_monthly_cost, efficiency_json, measured_at
	    FROM latest_per_app
	    ORDER BY deployment_namespace, measured_at DESC, application_namespace, application_name
	)
	SELECT
	    ns.deployment_namespace,
	    ns.total_monthly_cost,
	    COALESCE((ns.efficiency_json->>'cpuIdleCost')::float8, 0)
	        + COALESCE((ns.efficiency_json->>'ramIdleCost')::float8, 0) AS idle_cost,
	    apps.app_count,
	    ns.measured_at
	FROM latest_per_namespace ns
	JOIN (
	    SELECT deployment_namespace, COUNT(*) AS app_count
	    FROM latest_per_app
	    GROUP BY deployment_namespace
	) apps USING (deployment_namespace)
	ORDER BY ns.total_monthly_cost DESC, ns.deployment_namespace
	`

	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}
	defer rows.Close()

	result := []models.NamespaceSummary{}
	for rows.Next() {
		var ns models.NamespaceSummary
		if err := rows.Scan(&ns.Namespace, &ns.TotalCost, &ns.IdleCost, &ns.AppCount, &ns.MeasuredAt); err != nil {
			return nil, fmt.Errorf("failed to scan namespace row: %w", err)
		}
		result = append(result, ns)
	}

	return result, rows.Err()
}

func (p *PostgresCostStorage) GetLatestActualsForNamespace(ctx context.Context, namespace string) ([]models.CostActual, error) {
	query := `
	SELECT DISTINCT ON (application_namespace, application_name)
	    id, application_name, application_namespace, deployment_namespace,
	    sync_timestamp, measured_on, measured_at, total_monthly_cost,
	    efficiency_json, pods_json, opencost_output_json
	FROM cost_actuals
	WHERE deployment_namespace = $1
	ORDER BY application_namespace, application_name, measured_at DESC
	`

	rows, err := p.db.QueryContext(ctx, query, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to query actuals for namespace: %w", err)
	}
	defer rows.Close()

	result := []models.CostActual{}
	for rows.Next() {
		var actual models.CostActual
		var efficiencyJSON, podsJSON, opencostJSON sql.NullString

		if err := rows.Scan(
			&actual.ID,
			&actual.ApplicationName,
			&actual.ApplicationNamespace,
			&actual.DeploymentNamespace,
			&actual.SyncTimestamp,
			&actual.MeasuredOn,
			&actual.MeasuredAt,
			&actual.TotalMonthlyCost,
			&efficiencyJSON,
			&podsJSON,
			&opencostJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan actual row: %w", err)
		}
		clearInstallSync(&actual)

		if efficiencyJSON.Valid && efficiencyJSON.String != "null" {
			actual.Efficiency = &models.EfficiencyMetrics{}
			if err := json.Unmarshal([]byte(efficiencyJSON.String), actual.Efficiency); err != nil {
				return nil, fmt.Errorf("failed to decode efficiency JSON: %w", err)
			}
		}
		if podsJSON.Valid && podsJSON.String != "null" {
			if err := json.Unmarshal([]byte(podsJSON.String), &actual.Pods); err != nil {
				return nil, fmt.Errorf("failed to decode pods JSON: %w", err)
			}
		}
		if opencostJSON.Valid {
			if err := json.Unmarshal([]byte(opencostJSON.String), &actual.OpenCostOutput); err != nil {
				return nil, fmt.Errorf("failed to decode opencost JSON: %w", err)
			}
		}

		result = append(result, actual)
	}

	return result, rows.Err()
}

func (p *PostgresCostStorage) GetRecentActuals(ctx context.Context, app models.AppRef, limit int) ([]models.CostActual, error) {
	query := `
	SELECT id, total_monthly_cost, measured_at, (app_json->>'totalMonthlyCost')::float8
	FROM cost_actuals
	WHERE application_namespace = $1 AND application_name = $2
	ORDER BY measured_at DESC
	LIMIT $3
	`

	rows, err := p.db.QueryContext(ctx, query, app.Namespace, app.Name, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent actuals: %w", err)
	}
	defer rows.Close()

	var result []models.CostActual
	for rows.Next() {
		var a models.CostActual
		var appCost sql.NullFloat64
		if err := rows.Scan(&a.ID, &a.TotalMonthlyCost, &a.MeasuredAt, &appCost); err != nil {
			return nil, fmt.Errorf("failed to scan recent actual row: %w", err)
		}
		if appCost.Valid {
			a.App = &models.AppMeasurement{TotalMonthlyCost: appCost.Float64}
		}
		result = append(result, a)
	}

	return result, rows.Err()
}

func (p *PostgresCostStorage) SaveCloudEstimate(ctx context.Context, app models.AppRef, estimate *models.CloudServiceEstimate) error {
	resourcesJSON, err := json.Marshal(estimate.Resources)
	if err != nil {
		return fmt.Errorf("failed to encode cloud estimate resources: %w", err)
	}

	query := `
	INSERT INTO cloud_estimates (app_namespace, app_name, source, total_monthly_cost, currency, resources_json, pushed_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (app_namespace, app_name, source) DO UPDATE SET
		total_monthly_cost = EXCLUDED.total_monthly_cost,
		currency           = EXCLUDED.currency,
		resources_json     = EXCLUDED.resources_json,
		pushed_at          = EXCLUDED.pushed_at
	`
	_, err = p.db.ExecContext(ctx, query,
		app.Namespace,
		app.Name,
		estimate.Source,
		estimate.TotalMonthlyCost,
		estimate.Currency,
		string(resourcesJSON),
		estimate.PushedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save cloud estimate: %w", err)
	}
	return nil
}

func (p *PostgresCostStorage) GetCloudEstimate(ctx context.Context, app models.AppRef) (*models.CloudServiceEstimate, error) {
	query := `
	SELECT source, total_monthly_cost, currency, resources_json, pushed_at
	FROM cloud_estimates
	WHERE app_namespace = $1 AND app_name = $2
	ORDER BY pushed_at DESC
	LIMIT 1
	`

	var est models.CloudServiceEstimate
	var resourcesJSON sql.NullString

	err := p.db.QueryRowContext(ctx, query, app.Namespace, app.Name).Scan(
		&est.Source,
		&est.TotalMonthlyCost,
		&est.Currency,
		&resourcesJSON,
		&est.PushedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no record is a valid answer, documented on the interface
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query cloud estimate: %w", err)
	}

	if resourcesJSON.Valid && resourcesJSON.String != "null" {
		if err := json.Unmarshal([]byte(resourcesJSON.String), &est.Resources); err != nil {
			return nil, fmt.Errorf("failed to decode cloud estimate resources JSON: %w", err)
		}
	}

	return &est, nil
}

func (p *PostgresCostStorage) ListCloudEstimates(ctx context.Context) ([]models.AppCloudEstimate, error) {
	// A row without an application namespace cannot be reached by any request (see migration 005).
	query := `
	SELECT app_namespace, app_name, source, total_monthly_cost, currency, pushed_at
	FROM cloud_estimates
	WHERE app_namespace <> ''
	ORDER BY app_namespace, app_name, source
	`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list cloud estimates: %w", err)
	}
	defer rows.Close()

	estimates := []models.AppCloudEstimate{}
	for rows.Next() {
		var e models.AppCloudEstimate
		if err := rows.Scan(&e.App.Namespace, &e.App.Name, &e.Estimate.Source, &e.Estimate.TotalMonthlyCost,
			&e.Estimate.Currency, &e.Estimate.PushedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cloud estimate row: %w", err)
		}
		estimates = append(estimates, e)
	}
	return estimates, rows.Err()
}

func (p *PostgresCostStorage) ListLatestActuals(ctx context.Context) ([]models.CostActual, error) {
	// A row without an application namespace cannot be reached by any request (see migration 005).
	query := `
	SELECT DISTINCT ON (application_namespace, application_name)
	    application_namespace, application_name, deployment_namespace, measured_at, app_json
	FROM cost_actuals
	WHERE application_namespace <> ''
	ORDER BY application_namespace, application_name, measured_at DESC
	`

	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list latest actuals: %w", err)
	}
	defer rows.Close()

	result := []models.CostActual{}
	for rows.Next() {
		var actual models.CostActual
		var appJSON sql.NullString
		if err := rows.Scan(&actual.ApplicationNamespace, &actual.ApplicationName, &actual.DeploymentNamespace,
			&actual.MeasuredAt, &appJSON); err != nil {
			return nil, fmt.Errorf("failed to scan latest actual row: %w", err)
		}
		if err := decodeAppMeasurement(appJSON, &actual); err != nil {
			return nil, err
		}
		result = append(result, actual)
	}

	return result, rows.Err()
}

func (p *PostgresCostStorage) SaveDailyCosts(
	ctx context.Context, app models.AppRef, namespace, scope string, days []models.DailyCost,
) error {
	if len(days) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin daily costs transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	query := `
	INSERT INTO cost_daily
	(application_namespace, application_name, day, deployment_namespace, scope,
	 total_cost, cpu_cost, memory_cost, network_cost, storage_cost, recorded_at)
	VALUES ($1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, NOW())
	ON CONFLICT (application_namespace, application_name, day) DO UPDATE SET
		deployment_namespace = EXCLUDED.deployment_namespace,
		scope = EXCLUDED.scope,
		total_cost = EXCLUDED.total_cost,
		cpu_cost = EXCLUDED.cpu_cost,
		memory_cost = EXCLUDED.memory_cost,
		network_cost = EXCLUDED.network_cost,
		storage_cost = EXCLUDED.storage_cost,
		recorded_at = EXCLUDED.recorded_at
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare daily cost insert: %w", err)
	}
	defer stmt.Close()

	for _, day := range days {
		if _, err := stmt.ExecContext(ctx, app.Namespace, app.Name, day.Date, namespace, scope,
			day.TotalCost, day.CPUCost, day.MemoryCost, day.NetworkCost, day.StorageCost); err != nil {
			return fmt.Errorf("failed to save daily cost for %s: %w", day.Date, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit daily costs: %w", err)
	}
	return nil
}

func (p *PostgresCostStorage) GetDailyCosts(
	ctx context.Context, app models.AppRef, scope, from, to string,
) ([]models.DailyCost, error) {
	query := `
	SELECT to_char(day, 'YYYY-MM-DD'), total_cost, cpu_cost, memory_cost, network_cost, storage_cost
	FROM cost_daily
	WHERE application_namespace = $1 AND application_name = $2 AND scope = $3
	  AND day BETWEEN $4::date AND $5::date
	ORDER BY day
	`

	rows, err := p.db.QueryContext(ctx, query, app.Namespace, app.Name, scope, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily costs: %w", err)
	}
	defer rows.Close()

	result := []models.DailyCost{}
	for rows.Next() {
		var day models.DailyCost
		if err := rows.Scan(&day.Date, &day.TotalCost, &day.CPUCost, &day.MemoryCost, &day.NetworkCost, &day.StorageCost); err != nil {
			return nil, fmt.Errorf("failed to scan daily cost row: %w", err)
		}
		result = append(result, day)
	}

	return result, rows.Err()
}

func (p *PostgresCostStorage) GetDailyProgress(ctx context.Context, app models.AppRef) (*models.DailyProgress, error) {
	query := `
	SELECT to_char(recorded_through, 'YYYY-MM-DD'), scope
	FROM cost_daily_progress
	WHERE application_namespace = $1 AND application_name = $2
	`

	var progress models.DailyProgress
	err := p.db.QueryRowContext(ctx, query, app.Namespace, app.Name).Scan(&progress.Through, &progress.Scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no record is a valid answer, documented on the interface
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query daily progress: %w", err)
	}
	return &progress, nil
}

func (p *PostgresCostStorage) SetDailyProgress(ctx context.Context, app models.AppRef, progress models.DailyProgress) error {
	query := `
	INSERT INTO cost_daily_progress (application_namespace, application_name, recorded_through, scope, updated_at)
	VALUES ($1, $2, $3::date, $4, NOW())
	ON CONFLICT (application_namespace, application_name) DO UPDATE SET
		recorded_through = EXCLUDED.recorded_through,
		scope = EXCLUDED.scope,
		updated_at = EXCLUDED.updated_at
	`

	if _, err := p.db.ExecContext(ctx, query, app.Namespace, app.Name, progress.Through, progress.Scope); err != nil {
		return fmt.Errorf("failed to save daily progress: %w", err)
	}
	return nil
}

func (p *PostgresCostStorage) PruneDailyCosts(ctx context.Context, before string) error {
	if _, err := p.db.ExecContext(ctx, `DELETE FROM cost_daily WHERE day < $1::date`, before); err != nil {
		return fmt.Errorf("failed to prune daily costs: %w", err)
	}
	return nil
}

func (p *PostgresCostStorage) DeleteMeasurements(ctx context.Context, app models.AppRef) (bool, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin delete transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	var deleted int64
	for _, query := range []string{
		`DELETE FROM cost_actuals WHERE application_namespace = $1 AND application_name = $2`,
		`DELETE FROM cost_daily WHERE application_namespace = $1 AND application_name = $2`,
		`DELETE FROM cost_daily_progress WHERE application_namespace = $1 AND application_name = $2`,
	} {
		res, err := tx.ExecContext(ctx, query, app.Namespace, app.Name)
		if err != nil {
			return false, fmt.Errorf("failed to delete the measurements of %s: %w", app, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("failed to count the deleted measurements of %s: %w", app, err)
		}
		deleted += n
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit delete of %s: %w", app, err)
	}
	return deleted > 0, nil
}

func (p *PostgresCostStorage) Close() error {
	return p.db.Close()
}
