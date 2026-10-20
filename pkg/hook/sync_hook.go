package hook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/storage"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/validation"
)

type SyncHook struct {
	opencostCli  *opencost.Client
	costStorage  storage.CostStorage
	measureDelay time.Duration
	logger       *log.Logger
}

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

func (h *SyncHook) OnApplicationSync(ctx context.Context, notification *models.SyncNotification) error {
	h.logger.Printf("Processing sync notification for application %s/%s\n", notification.ApplicationNamespace, notification.ApplicationName)

	if err := ValidateNotification(notification); err != nil {
		return err
	}

	if !DeploysHere(notification) {
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

	// Wait for metrics to propagate
	h.logger.Printf("Waiting %v for metrics to propagate...\n", h.measureDelay)
	time.Sleep(h.measureDelay)

	// Measure actual costs from OpenCost
	actual, err := h.measureActualCosts(ctx, notification)
	if err != nil {
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

func (h *SyncHook) measureActualCosts(ctx context.Context, notification *models.SyncNotification) (*models.CostActual, error) {
	measured, err := MeasureNamespace(ctx, h.opencostCli, notification.Namespace, h.logger)
	if err != nil {
		return nil, err
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
		// namespace without pods has nothing idle.
		var idle float64
		if pods := n.pods.Efficiency(); pods != nil {
			idle = pods.CPUIdleCost + pods.RAMIdleCost
			if actual.Efficiency != nil {
				actual.Efficiency.CPUIdleCost = pods.CPUIdleCost
				actual.Efficiency.RAMIdleCost = pods.RAMIdleCost
			}
		}
		actual.IdleMonthlyCost = &idle
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
