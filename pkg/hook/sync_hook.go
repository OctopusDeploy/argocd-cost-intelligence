package hook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/models"
	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/opencost"
	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/storage"
	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/validation"
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
		v, err := h.opencostCli.GetNamespaceCost(ctx, notification.Namespace)
		metricsCh <- metricsResult{v, err}
	}()
	go func() {
		v, err := h.opencostCli.GetNamespaceSummary(ctx, notification.Namespace)
		efficiencyCh <- efficiencyResult{v, err}
	}()
	go func() {
		v, err := h.opencostCli.GetPodAllocations(ctx, notification.Namespace)
		podCh <- podResult{v, err}
	}()

	mr := <-metricsCh
	if mr.err != nil {
		return nil, fmt.Errorf("failed to query OpenCost: %w", mr.err)
	}

	er := <-efficiencyCh
	if er.err != nil {
		h.logger.Printf("Warning: failed to query OpenCost efficiency for %s: %v\n", notification.Namespace, er.err)
	}

	pr := <-podCh
	if pr.err != nil {
		h.logger.Printf("Warning: failed to query OpenCost pod breakdown for %s: %v\n", notification.Namespace, pr.err)
	}

	actual := &models.CostActual{
		ID:                   newRecordID(),
		ApplicationName:      notification.ApplicationName,
		ApplicationNamespace: notification.ApplicationNamespace,
		DeploymentNamespace:  notification.Namespace,
		SyncTimestamp:        notification.SyncTimestamp,
		MeasuredAt:           time.Now(),
		TotalMonthlyCost:     mr.v.AllocationTotals.TotalCost,
		OpenCostOutput:       *mr.v,
		Efficiency:           er.v,
	}
	if pr.v != nil {
		actual.Pods = pr.v.Pods()
		// Without the application's resources (an older notifications template) or its pods, the
		// application is known only as its whole namespace.
		if notification.Resources != nil {
			actual.App = pr.v.ForApplication(models.ManagedResources(notification.Resources).PodOwners())
		}
	}

	// Copy peak usage from allocation into efficiency metrics
	if actual.Efficiency != nil && mr.v.PeakCPUCores > 0 {
		actual.Efficiency.CPUCoresUsedMax = mr.v.PeakCPUCores
		actual.Efficiency.RAMBytesUsedMax = mr.v.PeakRAMBytes
	}

	h.logger.Printf("Measured costs - CPU: $%.2f, Memory: $%.2f, Storage: $%.2f, GPU: $%.2f, Total: $%.2f\n",
		mr.v.AllocationTotals.CPUCost,
		mr.v.AllocationTotals.MemoryCost,
		mr.v.AllocationTotals.StorageCost,
		mr.v.AllocationTotals.GPUCost,
		mr.v.AllocationTotals.TotalCost,
	)
	if actual.Efficiency != nil {
		h.logger.Printf("Measured efficiency - CPU: %.1f%%, RAM: %.1f%%, Total: %.1f%%\n",
			actual.Efficiency.CPUEfficiency*100,
			actual.Efficiency.RAMEfficiency*100,
			actual.Efficiency.TotalEfficiency*100,
		)
	}

	return actual, nil
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
