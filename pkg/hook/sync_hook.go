package hook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/storage"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/telemetry"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/validation"
)

// ErrNoData is returned for a sync whose namespace OpenCost has no data for yet, as for one created by
// the sync itself. Its cost is unknown rather than zero, so nothing is recorded.
var ErrNoData = errors.New("OpenCost has no data for the namespace yet")

// RetryDelays are the waits before measuring a sync again while OpenCost has no data for its
// namespace, the schedule the measurement at install retries on.
var RetryDelays = []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}

// maxConcurrentRetries bounds the retries measuring at once. They run outside the limit on syncs, so
// a burst of new applications never turns away the webhooks that follow.
const maxConcurrentRetries = 4

type SyncHook struct {
	opencostCli  *opencost.Client
	costStorage  storage.CostStorage
	measureDelay time.Duration
	logger       *log.Logger
	retries      *retries
}

// retries are the pending retries of syncs measured without OpenCost data, at most one per
// application. They are held in memory only: after a restart the measurement at install covers an
// application still without a record.
type retries struct {
	ctx     context.Context
	delays  []time.Duration
	observe func(result string)
	slots   chan struct{}

	mu sync.Mutex
	// syncs numbers each application's syncs in the order they arrive, so a sync that finishes after a
	// newer one started leaves no retry.
	syncs   map[models.AppRef]uint64
	pending map[models.AppRef]*pendingRetry
}

type pendingRetry struct{ cancel context.CancelFunc }

func NewSyncHook(
	opencostCli *opencost.Client,
	costStorage storage.CostStorage,
	measureDelay time.Duration,
	logger *log.Logger,
) *SyncHook {
	if measureDelay == 0 {
		measureDelay = 5 * time.Second
	}

	return &SyncHook{
		opencostCli:  opencostCli,
		costStorage:  costStorage,
		measureDelay: measureDelay,
		logger:       logger,
	}
}

// RetryWithoutData measures a sync again after each of delays while OpenCost has no data for its
// namespace, until ctx is done. A newer sync of the same application replaces its pending retry.
// observe, if not nil, gets each retry's result; the sync's own measurement is the caller's to count.
// Call it before the hook handles any sync.
func (h *SyncHook) RetryWithoutData(ctx context.Context, delays []time.Duration, observe func(result string)) {
	if observe == nil {
		observe = func(string) {}
	}
	h.retries = &retries{
		ctx:     ctx,
		delays:  delays,
		observe: observe,
		slots:   make(chan struct{}, maxConcurrentRetries),
		syncs:   map[models.AppRef]uint64{},
		pending: map[models.AppRef]*pendingRetry{},
	}
}

// MeasurementResult is the result of a sync's measurement, as telemetry counts it.
func MeasurementResult(err error) string {
	switch {
	case err == nil:
		return telemetry.MeasurementSuccess
	case errors.Is(err, ErrNoData):
		return telemetry.MeasurementNoData
	default:
		return telemetry.MeasurementError
	}
}

func (h *SyncHook) OnApplicationSync(ctx context.Context, notification *models.SyncNotification) error {
	h.logger.Printf("Processing sync notification for application %s/%s\n", notification.ApplicationNamespace, notification.ApplicationName)

	if err := ValidateNotification(notification); err != nil {
		return err
	}

	app := models.AppRef{Namespace: notification.ApplicationNamespace, Name: notification.ApplicationName}
	if !DeploysHere(notification) {
		h.beginSync(app)
		h.logger.Printf("Not measuring %s/%s: it deploys to another cluster (%s), and OpenCost measures only this one\n",
			notification.ApplicationNamespace, notification.ApplicationName, cluster(notification))
		return nil
	}

	if notification.Namespace == "" {
		notification.Namespace = notification.ApplicationNamespace
	}

	// Handle failure gracefully
	if notification.SyncResult != "Succeeded" {
		h.logger.Printf("Sync failed or unknown state for %s, skipping cost measurement\n", notification.ApplicationName)
		return nil
	}

	// This sync's measurement replaces an earlier one still waiting to retry.
	seq, replaced := h.beginSync(app)

	// Wait for metrics to propagate
	h.logger.Printf("Waiting %v for metrics to propagate...\n", h.measureDelay)
	time.Sleep(h.measureDelay)

	err := h.record(ctx, notification)
	// A sync that failed outright takes over the retry it replaced rather than leave the app unmeasured.
	if errors.Is(err, ErrNoData) || (err != nil && replaced) {
		h.scheduleRetry(app, *notification, seq) //nolint:contextcheck // a retry outlives this sync; it ends with the hook's own context
	}
	return err
}

// record measures the namespace of a sync and stores its cost record.
func (h *SyncHook) record(ctx context.Context, notification *models.SyncNotification) error {
	actual, err := h.measureActualCosts(ctx, notification)
	if err != nil {
		if errors.Is(err, ErrNoData) {
			h.logger.Printf("Not recording a cost for %s/%s: OpenCost has no data for namespace %s yet\n",
				notification.ApplicationNamespace, notification.ApplicationName, notification.Namespace)
			return err
		}
		h.logger.Printf("Error measuring costs: %v\n", err)
		return fmt.Errorf("failed to measure actual costs: %w", err)
	}

	// Store the actual cost record
	if err := h.costStorage.SaveActual(ctx, actual); err != nil {
		h.logger.Printf("Error saving actual costs: %v\n", err)
		return fmt.Errorf("failed to save actual costs: %w", err)
	}

	if actual.App != nil {
		h.logger.Printf("Successfully recorded actual cost: $%.2f/month for %s ($%.2f/month for namespace %s)\n",
			actual.App.TotalMonthlyCost, actual.ApplicationName, actual.TotalMonthlyCost, actual.DeploymentNamespace)
	} else {
		h.logger.Printf("Successfully recorded actual cost: $%.2f/month for namespace %s of %s\n",
			actual.TotalMonthlyCost, actual.DeploymentNamespace, actual.ApplicationName)
	}

	return nil
}

// scheduleRetry measures a sync recorded without OpenCost data again later, replacing a retry pending
// for the same application. seq is the number beginSync gave the sync: once a newer sync of the application
// has started, it schedules nothing.
func (h *SyncHook) scheduleRetry(app models.AppRef, notification models.SyncNotification, seq uint64) {
	r := h.retries
	if r == nil || len(r.delays) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	p := &pendingRetry{cancel: cancel}
	r.mu.Lock()
	if seq < r.syncs[app] {
		r.mu.Unlock()
		cancel()
		return
	}
	if old := r.pending[app]; old != nil {
		old.cancel()
	}
	r.pending[app] = p
	r.mu.Unlock()

	h.logger.Printf("Measuring %s/%s again in %v\n", app.Namespace, app.Name, r.delays[0])
	go func() {
		defer r.finish(app, p)
		defer func() {
			if rec := recover(); rec != nil {
				h.logger.Printf("PANIC retrying the measurement of %s/%s: %v\n", app.Namespace, app.Name, rec)
			}
		}()
		h.retry(ctx, app, &notification)
	}()
}

// retry measures notification's namespace after each delay until it records a cost or ctx is done.
// A retry keeps the sync's timestamp: its record is the sync's.
func (h *SyncHook) retry(ctx context.Context, app models.AppRef, notification *models.SyncNotification) {
	r := h.retries
	for i, delay := range r.delays {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		select {
		case r.slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		err := func() error {
			defer func() { <-r.slots }()
			return h.record(ctx, notification)
		}()
		if err == nil {
			r.observe(MeasurementResult(err))
			return
		}
		if ctx.Err() != nil {
			// Replaced by a newer sync, or shutting down: this attempt measured nothing of use.
			return
		}
		r.observe(MeasurementResult(err))
		if !errors.Is(err, ErrNoData) {
			h.logger.Printf("Retry %d of %d for %s/%s failed: %v\n", i+1, len(r.delays), app.Namespace, app.Name, err)
		}
		if i+1 < len(r.delays) {
			h.logger.Printf("Measuring %s/%s again in %v\n", app.Namespace, app.Name, r.delays[i+1])
		}
	}
	h.logger.Printf("Not measuring %s/%s again: its next sync, or the measurement at install after a restart, will\n",
		app.Namespace, app.Name)
}

// beginSync numbers a new sync of app and stops the retry pending for it, if any, which the sync
// replaces. It returns the sync's number and whether it replaced a retry.
func (h *SyncHook) beginSync(app models.AppRef) (seq uint64, replaced bool) {
	r := h.retries
	if r == nil {
		return 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncs[app]++
	if p := r.pending[app]; p != nil {
		p.cancel()
		delete(r.pending, app)
		replaced = true
	}
	return r.syncs[app], replaced
}

// finish forgets p once its retry has ended, unless a newer retry has replaced it.
func (r *retries) finish(app models.AppRef, p *pendingRetry) {
	r.mu.Lock()
	if r.pending[app] == p {
		delete(r.pending, app)
	}
	r.mu.Unlock()
	p.cancel()
}

func (h *SyncHook) measureActualCosts(ctx context.Context, notification *models.SyncNotification) (*models.CostActual, error) {
	measured, err := MeasureNamespace(ctx, h.opencostCli, notification.Namespace, h.logger)
	if err != nil {
		return nil, err
	}
	if !measured.HasData() {
		return nil, ErrNoData
	}
	// Without the application's resources (an older notifications template), the application is
	// known only as its whole namespace.
	var owners models.ManagedResources
	if notification.Resources != nil {
		owners = models.ManagedResources(notification.Resources).PodOwners()
	}
	app := models.AppRef{Namespace: notification.ApplicationNamespace, Name: notification.ApplicationName}
	return measured.Record(app, owners, notification.SyncTimestamp, models.MeasuredOnSync), nil
}

// Namespace is a deployment namespace as OpenCost measured it over the last 24 hours, from which each
// application deployed to it gets its cost record.
type Namespace struct {
	name       string
	measuredAt time.Time
	metrics    *models.OpenCostMetrics
	efficiency *models.EfficiencyMetrics
	pods       *opencost.PodAllocations
}

// MeasureNamespace queries OpenCost for a namespace's cost, efficiency and pods at once. Only the cost
// is required; without the other two a record has no efficiency or no pods.
func MeasureNamespace(ctx context.Context, client *opencost.Client, namespace string, logger *log.Logger) (*Namespace, error) {
	type metricsResult struct {
		v   *models.OpenCostMetrics
		err error
	}
	type efficiencyResult struct {
		v   *models.EfficiencyMetrics
		err error
	}
	type podResult struct {
		v   *opencost.PodAllocations
		err error
	}

	metricsCh := make(chan metricsResult, 1)
	efficiencyCh := make(chan efficiencyResult, 1)
	podCh := make(chan podResult, 1)

	go func() {
		v, err := client.GetNamespaceCost(ctx, namespace)
		metricsCh <- metricsResult{v, err}
	}()
	go func() {
		v, err := client.GetNamespaceSummary(ctx, namespace)
		efficiencyCh <- efficiencyResult{v, err}
	}()
	go func() {
		v, err := client.GetPodAllocations(ctx, namespace)
		podCh <- podResult{v, err}
	}()

	mr := <-metricsCh
	er := <-efficiencyCh
	pr := <-podCh
	if mr.err != nil {
		return nil, fmt.Errorf("failed to query OpenCost: %w", mr.err)
	}
	if er.err != nil {
		logger.Printf("Warning: failed to query OpenCost efficiency for %s: %v\n", namespace, er.err)
	}
	if pr.err != nil {
		logger.Printf("Warning: failed to query OpenCost pod breakdown for %s: %v\n", namespace, pr.err)
	}

	logger.Printf("Measured costs - CPU: $%.2f, Memory: $%.2f, Storage: $%.2f, GPU: $%.2f, Total: $%.2f\n",
		mr.v.AllocationTotals.CPUCost,
		mr.v.AllocationTotals.MemoryCost,
		mr.v.AllocationTotals.StorageCost,
		mr.v.AllocationTotals.GPUCost,
		mr.v.AllocationTotals.TotalCost,
	)
	if er.v != nil {
		logger.Printf("Measured efficiency - CPU: %.1f%%, RAM: %.1f%%, Total: %.1f%%\n",
			er.v.CPUEfficiency*100,
			er.v.RAMEfficiency*100,
			er.v.TotalEfficiency*100,
		)
	}

	return &Namespace{name: namespace, measuredAt: time.Now(), metrics: mr.v, efficiency: er.v, pods: pr.v}, nil
}

// HasData reports whether OpenCost had any data for the namespace. Without it, the namespace's cost is
// unknown rather than zero.
func (n *Namespace) HasData() bool { return n.metrics.MeasuredHours > 0 }

// Record is app's cost record from the namespace's measurement. owners are the pod-owning resources
// the application manages; with nil, the application is known only as its whole namespace. A
// measurement not taken after a sync has a zero syncTimestamp.
func (n *Namespace) Record(app models.AppRef, owners models.ManagedResources, syncTimestamp time.Time, measuredOn string) *models.CostActual {
	actual := &models.CostActual{
		ID:                   newRecordID(),
		ApplicationName:      app.Name,
		ApplicationNamespace: app.Namespace,
		DeploymentNamespace:  n.name,
		SyncTimestamp:        syncTimestamp,
		MeasuredAt:           n.measuredAt,
		MeasuredOn:           measuredOn,
		TotalMonthlyCost:     n.metrics.AllocationTotals.TotalCost,
		OpenCostOutput:       *n.metrics,
	}
	if n.efficiency != nil {
		// A copy each, as the applications of one namespace share its measurement.
		efficiency := *n.efficiency
		if n.metrics.PeakCPUCores > 0 {
			efficiency.CPUCoresUsedMax = n.metrics.PeakCPUCores
			efficiency.RAMBytesUsedMax = n.metrics.PeakRAMBytes
		}
		actual.Efficiency = &efficiency
	}
	if n.pods != nil {
		// OpenCost's summary has no idle cost, so the namespace's is measured from its pods. A
		// namespace without pods has nothing idle, unless it still has CPU or memory cost: then
		// its pods are missing from the answer and the idle cost is not measured.
		if pods := n.pods.Efficiency(); pods != nil {
			idle := pods.CPUIdleCost + pods.RAMIdleCost
			actual.IdleMonthlyCost = &idle
			if actual.Efficiency != nil {
				actual.Efficiency.CPUIdleCost = pods.CPUIdleCost
				actual.Efficiency.RAMIdleCost = pods.RAMIdleCost
			}
		} else if totals := n.metrics.AllocationTotals; totals.CPUCost+totals.MemoryCost == 0 {
			idle := 0.0
			actual.IdleMonthlyCost = &idle
		}
		actual.Pods = n.pods.Pods()
		if owners != nil {
			actual.App = n.pods.ForApplication(owners)
		}
	}
	return actual
}

// ValidateNotification checks that a notification identifies its Application and names valid
// namespaces. Records are looked up by the Application's namespace and name, so a record without
// applicationNamespace could never be read back.
func ValidateNotification(n *models.SyncNotification) error {
	if n.ApplicationName == "" {
		return errors.New("application name is required")
	}
	if n.ApplicationNamespace == "" {
		return errors.New("application namespace is required: the notifications template must send {{.app.metadata.namespace}} as applicationNamespace")
	}
	if !models.ManagedResources(n.Resources).PodOwners().Valid() {
		return errors.New("invalid resources: every pod-owning resource needs a valid Kubernetes name")
	}
	if !validation.IsValidNamespace(n.ApplicationNamespace) {
		return fmt.Errorf("invalid application namespace %q: must be a valid Kubernetes DNS label", n.ApplicationNamespace)
	}
	if n.Namespace != "" && !validation.IsValidNamespace(n.Namespace) {
		return fmt.Errorf("invalid namespace %q: must be a valid Kubernetes DNS label", n.Namespace)
	}
	return nil
}

// DeploysHere reports whether the notification's application deploys to the cluster this service
// runs in, the only one its OpenCost measures. Elsewhere, a namespace of the same name here is not the
// application's.
func DeploysHere(n *models.SyncNotification) bool {
	return exporter.Destination{Server: n.DestinationServer, Name: n.DestinationName}.Local()
}

func cluster(n *models.SyncNotification) string {
	if n.DestinationName != "" {
		return n.DestinationName
	}
	return n.DestinationServer
}

// GetLatestCostForApplication retrieves the most recent cost measurement
func (h *SyncHook) GetLatestCostForApplication(ctx context.Context, app models.AppRef) (*models.CostActual, error) {
	return h.costStorage.GetLatestActual(ctx, app)
}

// GetCostTrend retrieves cost history for an application over a period
func (h *SyncHook) GetCostTrend(ctx context.Context, app models.AppRef, days int) ([]models.CostActual, error) {
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -days)

	return h.costStorage.GetActualsInDateRange(ctx, app, startTime, endTime)
}

// newRecordID returns a cryptographically random 32-character hex ID.
// Using crypto/rand ensures the ID has no relationship to caller-controlled
// input, preventing the overwrite-via-crafted-ID attack.
func newRecordID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read always succeeds on supported platforms
	return hex.EncodeToString(b)
}
