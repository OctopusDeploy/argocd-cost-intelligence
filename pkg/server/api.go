package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/cloudcost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/history"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/hook"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/rightsizing"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/storage"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/telemetry"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/validation"
)

const (
	// maxConcurrentSyncs caps the number of background goroutines that may be
	// processing a sync notification at any given time. Each goroutine sleeps
	// 5 s then makes up to 3 OpenCost calls and 1 DB write, so a burst of
	// uncapped requests would exhaust the connection pool.
	maxConcurrentSyncs = 20

	// maxBodyBytes limits the webhook request body to prevent memory exhaustion
	// from oversized payloads. The actual notification JSON is well under 1 KB.
	maxBodyBytes = 64 * 1024 // 64 KB

	// maxQueryDays caps the ?days= parameter on query endpoints to prevent
	// full-table-scan queries on cost_actuals.
	maxQueryDays = 365

	// maxCloudEstimateBytes limits the Infracost/Terracost JSON body size.
	// Infracost output for large Terraform projects can reach a few MB.
	maxCloudEstimateBytes = 10 * 1024 * 1024 // 10 MB

	// validCloudSources is the set of accepted ?source= values for the cloud-estimate endpoint.
	// Adding a new tool (e.g. terracost) here is the only server-side change needed for a new source.
	validCloudSources = "infracost"

	// anomaly detection constants
	anomalyHistoryLimit     = 31  // fetch up to 31 records; index 0 is current, [1:] is history
	anomalyMinRecords       = 3   // minimum historical records before anomaly detection activates
	anomalyWarningThreshold = 0.3 // >30% above rolling average → warning
	anomalyAlertThreshold   = 1.0 // >100% above rolling average → alert
)

type CostAPIServer struct {
	router      chi.Router
	syncHook    *hook.SyncHook
	costStorage storage.CostStorage
	opencostCli *opencost.Client
	logger      *log.Logger
	syncSem     chan struct{}

	// clusterScope finds the Application a caller must be allowed to get to see cluster-wide cost
	// data. Nil, or no Application found, turns cluster-wide endpoints off.
	clusterScope ClusterScope

	// unmeasuredApp is an Application a sync notification is never measured for: the chart's access
	// Application, which deploys nothing. Nil measures every one.
	unmeasuredApp *models.AppRef

	// webhookSecret is the bearer token POST /on-sync requires. Empty refuses every webhook.
	webhookSecret string

	// vpaLister reads VerticalPodAutoscalers for rightsizing. Nil turns rightsizing off.
	vpaLister rightsizing.Lister

	// applications lists the Argo CD Applications, to leave those on other clusters out of the waste
	// ranking. Nil ranks every stored application.
	applications exporter.ApplicationLister

	// metrics records the API's own metrics. Nil records nothing.
	metrics *telemetry.Metrics
}

// Option configures a CostAPIServer.
type Option func(*CostAPIServer)

// WithWebhookSecret sets the bearer token Argo CD Notifications must send to POST /on-sync.
// Without it the webhook refuses every request.
func WithWebhookSecret(secret string) Option {
	return func(s *CostAPIServer) { s.webhookSecret = secret }
}

// ClusterScope finds the Application that gates cluster-wide cost data.
type ClusterScope interface {
	// App returns the gate Application, or nil when there is none.
	App(ctx context.Context) *models.AppRef
}

// FixedClusterScope is a gate Application chosen by configuration.
type FixedClusterScope models.AppRef

// App returns the configured Application.
func (f FixedClusterScope) App(context.Context) *models.AppRef {
	app := models.AppRef(f)
	return &app
}

// WithClusterScopeApp makes cluster-wide endpoints available to callers whose requests name app,
// which Argo CD's proxy only forwards for users allowed to get it.
func WithClusterScopeApp(app models.AppRef) Option {
	return WithClusterScope(FixedClusterScope(app))
}

// WithClusterScope gates cluster-wide endpoints by the Application scope finds, when it finds one.
func WithClusterScope(scope ClusterScope) Option {
	return func(s *CostAPIServer) { s.clusterScope = scope }
}

// clusterScopeApp returns the gate Application, or nil when cluster-wide data is off.
func (s *CostAPIServer) clusterScopeApp(ctx context.Context) *models.AppRef {
	if s.clusterScope == nil {
		return nil
	}
	return s.clusterScope.App(ctx)
}

// WithUnmeasuredApp answers sync notifications for app without measuring anything.
func WithUnmeasuredApp(app models.AppRef) Option {
	return func(s *CostAPIServer) { s.unmeasuredApp = &app }
}

// WithMetrics records HTTP requests, sync webhooks and measurements on m.
func WithMetrics(m *telemetry.Metrics) Option {
	return func(s *CostAPIServer) { s.metrics = m }
}

// WithVPALister reads VerticalPodAutoscaler recommendations for /rightsizing through lister.
func WithVPALister(lister rightsizing.Lister) Option {
	return func(s *CostAPIServer) { s.vpaLister = lister }
}

// WithApplications lists the Argo CD Applications through lister, so the waste ranking leaves out
// those deploying to another cluster or deleted from Argo CD.
func WithApplications(lister exporter.ApplicationLister) Option {
	return func(s *CostAPIServer) { s.applications = lister }
}

func NewCostAPIServer(
	syncHook *hook.SyncHook,
	costStorage storage.CostStorage,
	opencostCli *opencost.Client,
	logger *log.Logger,
	opts ...Option,
) *CostAPIServer {
	s := &CostAPIServer{
		router:      chi.NewRouter(),
		syncHook:    syncHook,
		costStorage: costStorage,
		opencostCli: opencostCli,
		logger:      logger,
		syncSem:     make(chan struct{}, maxConcurrentSyncs),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.setupRoutes()
	return s
}

func (s *CostAPIServer) setupRoutes() {
	s.router.Use(s.metrics.HTTPMiddleware)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RequestID)

	// Health check endpoints — no auth so liveness/readiness probes always work.
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/ready", s.handleReady)

	// The empty chart of the access Application, fetched by Argo CD's repo server. Public: it holds
	// nothing but its own name.
	s.router.Get("/charts/index.yaml", handleAccessChartIndex)
	s.router.Get("/charts/"+accessChartFile, handleAccessChart)

	s.router.Route("/api/cost-analysis", func(r chi.Router) {
		// Webhook from Argo CD's notifications controller. It does not pass through the extension
		// proxy, so it is authenticated by the shared WEBHOOK_SECRET instead; see auth.go.
		r.With(s.requireWebhookToken).Post("/on-sync", s.handleOnSync)

		// Every other endpoint is called through Argo CD's extension proxy, which has already
		// checked that the caller may get the Application named in Argocd-Application-Name.
		// Each group below decides what that Application entitles the caller to; see auth.go.
		r.Group(func(r chi.Router) {
			r.Use(requireCaller)
			r.Get("/config", s.handleGetConfig)
			r.Get("/data-quality", s.handleGetDataQuality)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireClusterScope)
			r.Get("/namespaces", s.handleListNamespaces)
			r.Get("/namespace/{namespace}/summary", s.handleGetNamespaceSummary)
			r.Get("/namespace/{ns}/apps", s.handleGetNamespaceApps)
			r.Get("/namespace/{ns}/orphaned-pods", s.handleGetOrphanedPods)
			r.Get("/chargeback", s.handleGetChargeback)
			r.Get("/chargeback/labels", s.handleGetChargebackLabels)
			r.Get("/cluster/overhead", s.handleGetClusterOverhead)
			r.Get("/waste", s.handleGetWaste)
		})

		r.Route("/{appName}", func(r chi.Router) {
			r.Use(requireApp)
			r.Get("/actual", appScoped(s.handleGetActualCost))
			r.Post("/cloud-estimate", appScoped(s.handlePostCloudEstimate))
			r.Get("/trend", appScoped(s.handleGetCostTrend))
			r.Get("/daily", appScoped(s.handleGetDailyCosts))
			r.Get("/orphaned-pods", appScoped(s.handleGetAppOrphanedPods))
			r.Get("/rightsizing", appScoped(s.handleGetRightsizing))
		})
	})
}

func (s *CostAPIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]string{
		"status": "ok",
	})
}

func (s *CostAPIServer) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]string{
		"status": "ready",
	})
}

// POST /api/cost-analysis/on-sync
// Webhook endpoint called after Argo CD application sync
func (s *CostAPIServer) handleOnSync(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var notification models.SyncNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		s.logger.Printf("Failed to decode sync notification: %v\n", err)
		s.metrics.ObserveWebhook(telemetry.WebhookInvalid)
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "invalid request body"})
		return
	}

	// Reject a notification that could never be read back here, where the notifications
	// controller logs the error, rather than in the background.
	if err := hook.ValidateNotification(&notification); err != nil {
		s.logger.Printf("Rejected sync notification %+v: %v\n", notification, err)
		s.metrics.ObserveWebhook(telemetry.WebhookInvalid)
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	// OpenCost measures only this cluster, so an application deploying elsewhere has nothing to
	// measure here. Answered before the pool, as nothing runs in the background.
	if !hook.DeploysHere(&notification) {
		s.logger.Printf("Not measuring sync notification for %s/%s: the application deploys to another cluster (server %q, name %q)\n",
			notification.ApplicationNamespace, notification.ApplicationName, notification.DestinationServer, notification.DestinationName)
		s.metrics.ObserveWebhook(telemetry.WebhookRemote)
		render.Status(r, http.StatusOK)
		render.JSON(w, r, map[string]string{"message": "not measured: the application deploys to another cluster"})
		return
	}

	// The access Application deploys nothing; measuring it would record the service's own namespace
	// under its name.
	if s.unmeasuredApp != nil && *s.unmeasuredApp == (models.AppRef{Namespace: notification.ApplicationNamespace, Name: notification.ApplicationName}) {
		s.logger.Printf("Not measuring sync notification for %s: it is the access Application, which deploys nothing\n", s.unmeasuredApp)
		s.metrics.ObserveWebhook(telemetry.WebhookIgnored)
		render.Status(r, http.StatusOK)
		render.JSON(w, r, map[string]string{"message": "not measured: the access Application deploys nothing"})
		return
	}

	// Reject the request if the goroutine pool is full rather than queuing
	// unbounded work that would exhaust DB connections and OpenCost sockets.
	select {
	case s.syncSem <- struct{}{}:
	default:
		s.metrics.ObserveWebhook(telemetry.WebhookOverloaded)
		render.Status(r, http.StatusTooManyRequests)
		render.JSON(w, r, map[string]string{"error": "too many concurrent syncs, try again later"})
		return
	}

	s.logger.Printf("Received sync notification: %+v\n", notification)
	s.metrics.ObserveWebhook(telemetry.WebhookAccepted)
	ctx := context.WithoutCancel(r.Context())
	go func() {
		defer func() { <-s.syncSem }()
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Printf("PANIC in sync goroutine for app=%s: %v\n", notification.ApplicationName, rec)
			}
		}()
		start := time.Now()
		err := s.syncHook.OnApplicationSync(ctx, &notification)
		result := hook.MeasurementResult(err)
		if result == telemetry.MeasurementError {
			// No data is expected for a namespace the sync created; the hook has logged it.
			s.logger.Printf("Error processing sync notification for app=%s: %v\n", notification.ApplicationName, err)
		}
		s.metrics.ObserveMeasurement(result, time.Since(start))
	}()

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, map[string]string{"message": "sync notification accepted for processing"})
}

// GET /api/cost-analysis/{appName}/actual
// Returns the latest actual cost for an application, with anomaly detection.
func (s *CostAPIServer) handleGetActualCost(w http.ResponseWriter, r *http.Request, app models.AppRef) {
	ctx := r.Context()
	latest, err := s.costStorage.GetLatestActual(ctx, app)
	if err != nil {
		s.logger.Printf("Error retrieving actual cost for %s: %v\n", app, err)
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{
			"error": "no cost data found for application " + app.String(),
		})
		return
	}
	actual := latest.ForApplication()

	recent, err := s.costStorage.GetRecentActuals(ctx, app, anomalyHistoryLimit)
	if err != nil {
		s.logger.Printf("Error retrieving recent actuals for anomaly detection (%s): %v\n", app, err)
		// Non-fatal: serve the actual without anomaly data rather than failing the request.
	} else {
		// Skip index 0 — it is the same record as actual (most recent, DESC order).
		history := recent
		if len(history) > 0 && history[0].ID == actual.ID {
			history = history[1:]
		}
		actual.Anomaly = computeAnomaly(actual.TotalMonthlyCost, comparableHistory(history, actual.Scope))
	}

	// Attach the latest cloud estimate if one exists — non-fatal if missing.
	cloudEst, err := s.costStorage.GetCloudEstimate(ctx, app)
	if err != nil {
		s.logger.Printf("Error retrieving cloud estimate for %s: %v\n", app, err)
	} else {
		actual.CloudEstimate = cloudEst
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, actual)
}

// POST /api/cost-analysis/{appName}/cloud-estimate?source=infracost
// Accepts the raw JSON output from `infracost breakdown --format json` (or future tools)
// and stores a normalised cloud service cost estimate keyed by the application's namespace and name.
func (s *CostAPIServer) handlePostCloudEstimate(w http.ResponseWriter, r *http.Request, app models.AppRef) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "infracost"
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCloudEstimateBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "failed to read request body"})
		return
	}

	var estimate *models.CloudServiceEstimate
	switch source {
	case "infracost":
		estimate, err = cloudcost.ParseInfracost(body)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "invalid infracost JSON: " + err.Error()})
			return
		}
	default:
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "unsupported source: " + source + " (supported: " + validCloudSources + ")"})
		return
	}

	if err := s.costStorage.SaveCloudEstimate(r.Context(), app, estimate); err != nil {
		s.logger.Printf("Error saving cloud estimate for %s: %v\n", app, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to save cloud estimate"})
		return
	}

	s.logger.Printf("Cloud estimate saved for app=%s source=%s total=$%.2f resources=%d\n",
		app, source, estimate.TotalMonthlyCost, len(estimate.Resources))

	render.JSON(w, r, estimate)
}

// comparableHistory returns the earlier records measured the same way as the current one, as the
// application sees them: an application's own cost is never compared with its namespace's.
func comparableHistory(history []models.CostActual, scope string) []models.CostActual {
	same := make([]models.CostActual, 0, len(history))
	for _, record := range history {
		if view := record.ForApplication(); view.Scope == scope {
			same = append(same, view)
		}
	}
	return same
}

// computeAnomaly returns a CostAnomaly when the current cost deviates significantly
// from the rolling average of historical records. Returns nil when there are
// insufficient records or no anomaly is detected.
func computeAnomaly(current float64, history []models.CostActual) *models.CostAnomaly {
	if len(history) < anomalyMinRecords {
		return nil
	}

	var sum float64
	for _, h := range history {
		sum += h.TotalMonthlyCost
	}
	avg := sum / float64(len(history))

	if avg <= 0 {
		return nil
	}

	score := (current - avg) / avg
	if score <= anomalyWarningThreshold {
		return nil
	}

	severity := "warning"
	if score > anomalyAlertThreshold {
		severity = "alert"
	}

	pct := math.Round(score * 100)
	return &models.CostAnomaly{
		Severity:    severity,
		Score:       score,
		RollingAvg:  avg,
		RecordCount: len(history),
		Message:     fmt.Sprintf("Cost is %.0f%% above the %d-record average ($%.2f/mo)", pct, len(history), avg),
	}
}

// GET /api/cost-analysis/{appName}/trend?days=30
// Returns cost trend data for an application over time
func (s *CostAPIServer) handleGetCostTrend(w http.ResponseWriter, r *http.Request, app models.AppRef) {
	daysStr := r.URL.Query().Get("days")

	days := 30
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}
	if days > maxQueryDays {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "days exceeds maximum of " + strconv.Itoa(maxQueryDays)})
		return
	}

	ctx := r.Context()
	records, err := s.syncHook.GetCostTrend(ctx, app, days)
	if err != nil {
		s.logger.Printf("Error retrieving cost trend for %s: %v\n", app, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve cost trend"})
		return
	}
	trend := make([]models.CostActual, 0, len(records))
	for _, record := range records {
		trend = append(trend, record.ForApplication())
	}

	// Calculate statistics
	var totalCost float64
	var avgCost float64
	var maxCost float64
	var minCost float64

	for i, actual := range trend {
		totalCost += actual.TotalMonthlyCost
		if actual.TotalMonthlyCost > maxCost {
			maxCost = actual.TotalMonthlyCost
		}
		if i == 0 || actual.TotalMonthlyCost < minCost {
			minCost = actual.TotalMonthlyCost
		}
	}

	if len(trend) > 0 {
		avgCost = totalCost / float64(len(trend))
	}

	response := map[string]any{
		"applicationName": app.Name,
		"trend":           trend,
		"statistics": map[string]float64{
			"average": avgCost,
			"maximum": maxCost,
			"minimum": minCost,
			"total":   totalCost,
		},
		"count": len(trend),
		"days":  days,
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, response)
}

// GET /api/cost-analysis/namespace/{namespace}/summary?days=30
// Returns the average daily cost of a deployment namespace
func (s *CostAPIServer) handleGetNamespaceSummary(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	if !validation.IsValidNamespace(namespace) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "invalid namespace: " + namespace})
		return
	}
	daysStr := r.URL.Query().Get("days")

	days := 30
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}
	if days > maxQueryDays {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "days exceeds maximum of " + strconv.Itoa(maxQueryDays)})
		return
	}

	ctx := r.Context()
	avgCost, err := s.costStorage.GetNamespaceCostSummary(ctx, namespace, days)
	if err != nil {
		s.logger.Printf("Error retrieving namespace summary for %s: %v\n", namespace, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve namespace summary"})
		return
	}

	response := map[string]any{
		"namespace":   namespace,
		"averageCost": avgCost,
		"days":        days,
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, response)
}

// appTarget returns where app's costs are measured, from its latest stored record: the namespace it
// deploys to and, when they were recorded, the resources it manages there (nil otherwise, meaning the
// whole namespace). Both always come from the record, never the caller, so an app-scoped request can
// only reach its own application's namespace. On failure it writes the response and returns false.
func (s *CostAPIServer) appTarget(w http.ResponseWriter, r *http.Request, app models.AppRef) (string, models.ManagedResources, bool) {
	latest, err := s.costStorage.GetLatestActual(r.Context(), app)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "no cost data found for application " + app.String()})
		return "", nil, false
	}

	namespace := latest.DeploymentNamespace
	if namespace == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "namespace could not be determined — trigger a sync first"})
		return "", nil, false
	}

	if !validation.IsValidNamespace(namespace) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "invalid namespace: " + namespace})
		return "", nil, false
	}

	return namespace, latest.MeasuredResources(), true
}

// GET /api/cost-analysis/{appName}/daily?days=30
// GET /api/cost-analysis/{appName}/daily?from=2026-09-01&to=2026-09-30
// Returns a day-by-day cost time series for the application: its own controllers when the latest
// sync recorded them, its whole deployment namespace otherwise. The series covers the last ?days=
// days, today included, or the UTC days ?from= through ?to=, both included. Days the history
// recorder has stored come from storage; OpenCost is asked only for the days after them.
func (s *CostAPIServer) handleGetDailyCosts(w http.ResponseWriter, r *http.Request, app models.AppRef) {
	from, to, days, rangeErr := dailyRange(r.URL.Query(), time.Now())
	if rangeErr != "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": rangeErr})
		return
	}

	namespace, resources, ok := s.appTarget(w, r, app)
	if !ok {
		return
	}

	target := history.Target{App: app, Namespace: namespace, Resources: resources}
	dailyCosts, err := history.Daily(r.Context(), s.costStorage, s.opencostCli, target, from, to)
	if err != nil {
		s.logger.Printf("Error retrieving daily costs for %s (namespace %s): %v\n", app, namespace, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve daily costs"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]any{
		"applicationName": app.Name,
		"namespace":       namespace,
		"scope":           models.ScopeOf(resources),
		"days":            days,
		"dailyCosts":      dailyCosts,
	})
}

// dailyRange reads the UTC days a /daily request asks for, both included: ?from= through ?to=
// (YYYY-MM-DD) when given, the last ?days= days up to today otherwise. It returns the number of days
// covered, or a message saying why the request is invalid.
func dailyRange(query url.Values, now time.Time) (from, to time.Time, days int, errMsg string) {
	today := now.UTC().Truncate(24 * time.Hour)
	fromStr, toStr := query.Get("from"), query.Get("to")
	if fromStr == "" && toStr == "" {
		days = 30
		if d, err := strconv.Atoi(query.Get("days")); err == nil && d > 0 {
			days = d
		}
		if days > maxQueryDays {
			return from, to, 0, "days exceeds maximum of " + strconv.Itoa(maxQueryDays)
		}
		return today.AddDate(0, 0, -(days - 1)), today, days, ""
	}

	if fromStr == "" || toStr == "" {
		return from, to, 0, "from and to must be given together"
	}
	from, errFrom := time.Parse(time.DateOnly, fromStr)
	to, errTo := time.Parse(time.DateOnly, toStr)
	if errFrom != nil || errTo != nil {
		return from, to, 0, "from and to must be dates in the form YYYY-MM-DD"
	}
	if to.Before(from) {
		return from, to, 0, "from must not be after to"
	}
	if from.After(today) {
		return from, to, 0, "from must not be in the future"
	}
	days = int(to.Sub(from).Hours()/24) + 1
	if days > maxQueryDays {
		return from, to, 0, "the range exceeds the maximum of " + strconv.Itoa(maxQueryDays) + " days"
	}
	return from, to, days, ""
}

// GET /api/cost-analysis/namespaces
// Returns one row per distinct deployment namespace with aggregate cost and app count.
func (s *CostAPIServer) handleListNamespaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespaces, err := s.costStorage.ListNamespaces(ctx)
	if err != nil {
		s.logger.Printf("Error listing namespaces: %v\n", err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to list namespaces"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]any{"namespaces": namespaces})
}

// GET /api/cost-analysis/namespace/{ns}/apps
// Returns the latest cost record for each application in the given deployment namespace.
func (s *CostAPIServer) handleGetNamespaceApps(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	ctx := r.Context()

	apps, err := s.costStorage.GetLatestActualsForNamespace(ctx, ns)
	if err != nil {
		s.logger.Printf("Error retrieving apps for namespace %s: %v\n", ns, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve namespace apps"})
		return
	}

	if len(apps) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "no cost data found for namespace " + ns})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]any{"apps": apps})
}

// GET /api/cost-analysis/namespace/{ns}/orphaned-pods
// Returns pods that have allocated resources but zero CPU and RAM usage — likely evicted,
// stuck in Pending, or from completed Jobs still holding PVs or load balancers.
func (s *CostAPIServer) handleGetOrphanedPods(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	if !validation.IsValidNamespace(ns) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "invalid namespace: " + ns})
		return
	}
	s.writeOrphanedPods(w, r, ns, nil)
}

// GET /api/cost-analysis/{appName}/rightsizing
// Returns what the Vertical Pod Autoscaler recommends for the application's controllers, compared with
// their containers' requests and priced with OpenCost. available is false when the cluster has no VPA
// or the service may not read it. Applications whose latest record has no controllers get no
// workloads: which VPAs are theirs is not known.
func (s *CostAPIServer) handleGetRightsizing(w http.ResponseWriter, r *http.Request, app models.AppRef) {
	namespace, resources, ok := s.appTarget(w, r, app)
	if !ok {
		return
	}
	unavailable := models.Rightsizing{Workloads: []models.WorkloadRightsizing{}}
	if s.vpaLister == nil {
		render.JSON(w, r, unavailable)
		return
	}

	vpas, err := s.vpaLister.List(r.Context(), namespace)
	if errors.Is(err, rightsizing.ErrUnavailable) {
		render.JSON(w, r, unavailable)
		return
	}
	if err != nil {
		s.logger.Printf("Error listing VerticalPodAutoscalers for %s (namespace %s): %v\n", app, namespace, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to read VerticalPodAutoscaler recommendations"})
		return
	}

	var containers []models.ContainerAllocation
	if len(vpas) > 0 && resources != nil {
		if containers, err = s.opencostCli.GetContainerAllocations(r.Context(), namespace); err != nil {
			s.logger.Printf("Error retrieving container allocations for %s (namespace %s): %v\n", app, namespace, err)
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": "failed to retrieve container costs"})
			return
		}
	}
	render.JSON(w, r, rightsizing.Recommend(namespace, resources, vpas, containers))
}

// GET /api/cost-analysis/{appName}/orphaned-pods
// Returns the application's orphaned pods: those its controllers own when the latest sync recorded
// them, every one in its deployment namespace otherwise.
func (s *CostAPIServer) handleGetAppOrphanedPods(w http.ResponseWriter, r *http.Request, app models.AppRef) {
	ns, resources, ok := s.appTarget(w, r, app)
	if !ok {
		return
	}
	s.writeOrphanedPods(w, r, ns, resources)
}

// writeOrphanedPods writes a namespace's orphaned pods, only those the resources own unless they are
// nil.
func (s *CostAPIServer) writeOrphanedPods(w http.ResponseWriter, r *http.Request, ns string, resources models.ManagedResources) {
	all, err := s.opencostCli.GetOrphanedPods(r.Context(), ns)
	if err != nil {
		s.logger.Printf("Error retrieving orphaned pods for namespace %s: %v\n", ns, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve orphaned pods"})
		return
	}
	pods := all
	if resources != nil {
		pods = []models.OrphanedPod{}
		for _, p := range all {
			if resources.OwnsPod(p.Namespace, p.ControllerKind, p.Controller, p.Name) {
				pods = append(pods, p)
			}
		}
	}

	var totalWasted float64
	for _, p := range pods {
		totalWasted += p.TotalCost
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]any{
		"orphanedPods":    pods,
		"totalWastedCost": totalWasted,
	})
}

// GET /api/cost-analysis/chargeback?label=<key>&namespace=<ns>
// Returns per-label-value cost breakdown grouped by the given label key.
// namespace is optional; when omitted, results span all namespaces.
func (s *CostAPIServer) handleGetChargeback(w http.ResponseWriter, r *http.Request) {
	labelKey := r.URL.Query().Get("label")
	if !validation.IsValidLabelKey(labelKey) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "missing or invalid label key"})
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace != "" && !validation.IsValidNamespace(namespace) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "invalid namespace: " + namespace})
		return
	}

	entries, err := s.opencostCli.GetChargeback(r.Context(), labelKey, namespace)
	if err != nil {
		s.logger.Printf("Error retrieving chargeback for label=%s namespace=%s: %v\n", labelKey, namespace, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve chargeback data"})
		return
	}

	var total float64
	for _, e := range entries {
		total += e.TotalCost
	}

	w.Header().Set("Content-Type", "application/json")
	render.JSON(w, r, map[string]any{
		"labelKey":  labelKey,
		"namespace": namespace,
		"entries":   entries,
		"totalCost": total,
	})
}

// GET /api/cost-analysis/chargeback/labels?namespace=<ns>
// Returns the label keys pods carried over the chargeback window, for the label picker.
// namespace is optional; when omitted, the keys span all namespaces.
func (s *CostAPIServer) handleGetChargebackLabels(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace != "" && !validation.IsValidNamespace(namespace) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "invalid namespace: " + namespace})
		return
	}

	keys, err := s.opencostCli.GetLabelKeys(r.Context(), namespace)
	if err != nil {
		s.logger.Printf("Error retrieving label keys for namespace=%s: %v\n", namespace, err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve label keys"})
		return
	}

	render.JSON(w, r, map[string]any{
		"namespace": namespace,
		"labelKeys": keys,
	})
}

// GET /api/cost-analysis/cluster/overhead
// Returns the cluster control-plane fee (EKS/GKE/AKS) from OpenCost /assets.
// Returns controlPlaneCost=0 for self-managed clusters — never an error.
func (s *CostAPIServer) handleGetClusterOverhead(w http.ResponseWriter, r *http.Request) {
	overhead, err := s.opencostCli.GetClusterOverhead(r.Context())
	if err != nil {
		s.logger.Printf("Error retrieving cluster overhead: %v\n", err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to retrieve cluster overhead"})
		return
	}
	render.JSON(w, r, overhead)
}

// GET /api/cost-analysis/config
// Tells the UI which Application gates cluster-wide cost data, so it can send that Application
// in its proxy headers. clusterScopeApp is null when cluster-wide data is turned off.
func (s *CostAPIServer) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]any{"clusterScopeApp": s.clusterScopeApp(r.Context())})
}

// GET /api/cost-analysis/data-quality
// Returns a DataQuality struct indicating whether OpenCost is using real
// cloud-billing prices or on-demand defaults. Always returns 200 — the
// pricingSource field ("real" | "estimated" | "unknown") carries the signal.
func (s *CostAPIServer) handleGetDataQuality(w http.ResponseWriter, r *http.Request) {
	quality, err := s.opencostCli.GetDataQuality(r.Context())
	if err != nil {
		s.logger.Printf("Error retrieving data quality: %v\n", err)
		render.JSON(w, r, &models.DataQuality{PricingSource: "unknown", Message: err.Error()})
		return
	}
	render.JSON(w, r, quality)
}

func (s *CostAPIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
