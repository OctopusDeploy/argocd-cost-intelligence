package rightsizing

import (
	"math"
	"sort"
	"strings"

	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/models"
)

const (
	// cpuStep and memoryStep are what suggestions are rounded up to: 10m of CPU, 16Mi of memory.
	cpuStep    = 0.01
	memoryStep = 16 * 1024 * 1024
	// windowHours is the period container allocations cover, and monthFactor projects it onto a
	// 30-day month.
	windowHours = 24.0
	monthFactor = 30.0
	// settledRange is how far above its target the VPA's upper bound may be before the recommendation
	// counts as unsettled. The VPA widens its upper bound by a factor of 1 + 1/(days of history), so it
	// starts at the VPA's maximum and comes within this of the target after about a day.
	settledRange = 3.0
)

// Recommend matches the VPAs of an application's deployment namespace to the pod-owning resources
// it manages, and prices each recommendation with what its containers cost over the last 24 hours.
// VPAs for anything else in the namespace are ignored.
//
// The change is priced the way OpenCost charges: for the larger of a container's request and its
// usage. A request lowered below what a container uses saves nothing beyond its usage.
func Recommend(namespace string, resources models.ManagedResources, vpas []VPA, containers []models.ContainerAllocation) models.Rightsizing {
	result := models.Rightsizing{Available: true, Workloads: []models.WorkloadRightsizing{}}
	for _, owner := range resources.PodOwners() {
		if owner.Namespace != "" && owner.Namespace != namespace {
			continue
		}
		owned := ownedBy(owner, namespace, containers)
		workload := models.WorkloadRightsizing{
			Kind:       owner.Kind,
			Name:       owner.Name,
			Status:     models.RightsizingNone,
			Pods:       runningPods(owned),
			Containers: []models.ContainerRightsizing{},
		}

		if vpa, ok := vpaFor(owner, vpas); ok {
			workload.UpdateMode = vpa.UpdateMode
			workload.LowConfidence = vpa.LowConfidence
			switch {
			case !vpa.Recommends():
				workload.Status = models.RightsizingManaged
			case len(vpa.Containers) == 0:
				workload.Status = models.RightsizingPending
			default:
				workload.Status = models.RightsizingRecommended
				workload.Containers = containerAdvice(vpa, owned)
				for _, c := range workload.Containers {
					workload.LowConfidence = workload.LowConfidence || c.Unsettled
				}
			}
		}

		for _, c := range workload.Containers {
			for _, change := range []float64{c.MonthlyChange.CPU, c.MonthlyChange.Memory} {
				workload.MonthlyChange += change
				if change < 0 {
					result.MonthlySaving -= change
				} else {
					result.MonthlyIncrease += change
				}
			}
		}
		result.Workloads = append(result.Workloads, workload)
	}

	sort.SliceStable(result.Workloads, func(i, j int) bool { return result.Workloads[i].Name < result.Workloads[j].Name })
	return result
}

// vpaFor finds the VPA whose target is owner.
func vpaFor(owner models.ManagedResource, vpas []VPA) (VPA, bool) {
	for _, vpa := range vpas {
		if strings.EqualFold(vpa.TargetKind, owner.Kind) && vpa.TargetName == owner.Name {
			return vpa, true
		}
	}
	return VPA{}, false
}

// ownedBy keeps the containers of the pods owner owns, as OpenCost reports their controller.
func ownedBy(owner models.ManagedResource, namespace string, containers []models.ContainerAllocation) []models.ContainerAllocation {
	owners := models.ManagedResources{owner}
	var owned []models.ContainerAllocation
	for _, c := range containers {
		if owners.OwnsPod(namespace, c.ControllerKind, c.Controller, c.Pod) {
			owned = append(owned, c)
		}
	}
	return owned
}

// runningPods is how many pods ran on average over the window, at least one if any ran: a pod
// replaced during the day counts once, not twice.
func runningPods(containers []models.ContainerAllocation) int {
	podHours := map[string]float64{}
	for _, c := range containers {
		podHours[c.Pod] = math.Max(podHours[c.Pod], c.Hours)
	}
	if len(podHours) == 0 {
		return 0
	}
	var total float64
	for _, hours := range podHours {
		total += hours
	}
	return max(1, int(math.Round(total/windowHours)))
}

// containerAdvice compares each container the VPA recommends for with its requests. A container
// that did not run in the window has no requests to compare, and is left out.
func containerAdvice(vpa VPA, owned []models.ContainerAllocation) []models.ContainerRightsizing {
	advice := []models.ContainerRightsizing{}
	for _, rec := range vpa.Containers {
		var runs []models.ContainerAllocation
		for _, c := range owned {
			if c.Container == rec.Name {
				runs = append(runs, c)
			}
		}
		if len(runs) == 0 {
			continue
		}

		// The run that lasted longest stands for the container's current requests and limit.
		longest := runs[0]
		for _, run := range runs[1:] {
			if run.Hours > longest.Hours {
				longest = run
			}
		}
		suggested := models.Resources{
			CPUCores:    roundUp(rec.Target.CPUCores, cpuStep),
			MemoryBytes: roundUp(rec.Target.MemoryBytes, memoryStep),
		}
		advice = append(advice, models.ContainerRightsizing{
			Name:             rec.Name,
			Requests:         models.Resources{CPUCores: longest.CPURequestCores, MemoryBytes: longest.RAMRequestBytes},
			MemoryLimitBytes: longest.RAMLimitBytes,
			Target:           rec.Target,
			LowerBound:       rec.LowerBound,
			UpperBound:       rec.UpperBound,
			Suggested:        suggested,
			Unsettled:        unsettled(rec),
			MonthlyChange:    monthlyChange(runs, suggested),
		})
	}
	return advice
}

// unsettled reports whether either upper bound of rec is more than settledRange times its target.
func unsettled(rec ContainerRecommendation) bool {
	return rec.UpperBound.CPUCores > settledRange*rec.Target.CPUCores ||
		rec.UpperBound.MemoryBytes > settledRange*rec.Target.MemoryBytes
}

// monthlyChange prices requesting suggested in every run of a container: OpenCost charges each run
// for the larger of its request and its usage, at the run's own price.
func monthlyChange(runs []models.ContainerAllocation, suggested models.Resources) models.ResourceCosts {
	var change models.ResourceCosts
	for _, run := range runs {
		if run.Hours <= 0 {
			continue
		}
		if run.CPUCoreHours > 0 {
			price := run.CPUCost / run.CPUCoreHours
			billed := math.Max(suggested.CPUCores, run.CPUUsageCores) * run.Hours
			change.CPU += price * (billed - run.CPUCoreHours)
		}
		if run.RAMByteHours > 0 {
			price := run.RAMCost / run.RAMByteHours
			billed := math.Max(suggested.MemoryBytes, run.RAMUsageBytes) * run.Hours
			change.Memory += price * (billed - run.RAMByteHours)
		}
	}
	change.CPU *= monthFactor
	change.Memory *= monthFactor
	return change
}

// roundUp rounds v up to a whole number of steps; 0 stays 0. The product is rounded to six decimals,
// so 12 steps of 0.01 are 0.12, not 0.12000000000000001.
func roundUp(v, step float64) float64 {
	steps := math.Ceil(v/step - 1e-9)
	return math.Round(steps*step*1e6) / 1e6
}
