package models

// ContainerAllocation is one container of one pod over a measurement window, as OpenCost reports it.
type ContainerAllocation struct {
	Pod            string
	Container      string
	ControllerKind string
	Controller     string
	// Hours is how long the container ran in the window.
	Hours float64
	// The averages over the hours it ran: for a request or limit that did not change, its value.
	CPURequestCores float64
	CPUUsageCores   float64
	CPULimitCores   float64
	RAMRequestBytes float64
	RAMUsageBytes   float64
	RAMLimitBytes   float64
	// CPUCoreHours and RAMByteHours are what OpenCost charged for: the larger of request and usage.
	CPUCoreHours float64
	RAMByteHours float64
	CPUCost      float64
	RAMCost      float64
}

// Resources is an amount of CPU and memory.
type Resources struct {
	CPUCores    float64 `json:"cpuCores"`
	MemoryBytes float64 `json:"memoryBytes"`
}

// Rightsizing statuses of a workload.
const (
	// RightsizingRecommended: a VPA in recommendation-only mode has a recommendation for it.
	RightsizingRecommended = "recommended"
	// RightsizingLearning: a VPA in recommendation-only mode has a recommendation for it, but from too
	// little usage history to act on yet.
	RightsizingLearning = "learning"
	// RightsizingManaged: a VPA that sets the requests itself covers it.
	RightsizingManaged = "managed"
	// RightsizingPending: a VPA covers it but has no recommendation yet.
	RightsizingPending = "pending"
	// RightsizingNone: no VPA covers it.
	RightsizingNone = "none"
)

// Rightsizing is what the Vertical Pod Autoscaler recommends for an application's workloads, priced
// with OpenCost.
type Rightsizing struct {
	// Available is false when the cluster has no VPA API or the service may not read it.
	Available bool                  `json:"available"`
	Workloads []WorkloadRightsizing `json:"workloads"`
	// MonthlySaving and MonthlyIncrease add up the changes that lower and raise cost; both are positive.
	MonthlySaving   float64 `json:"monthlySaving"`
	MonthlyIncrease float64 `json:"monthlyIncrease"`
}

// WorkloadRightsizing is one of the application's pod-owning resources.
type WorkloadRightsizing struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// UpdateMode is the VPA's update mode (Off, Initial, Recreate, Auto, ...), empty without a VPA.
	UpdateMode string `json:"updateMode,omitempty"`
	// LowConfidence is set when a recommendation's upper bound is still far above its target.
	LowConfidence bool                   `json:"lowConfidence"`
	Pods          int                    `json:"pods"`
	Containers    []ContainerRightsizing `json:"containers"`
	// MonthlyChange is the cost change of applying every container's suggestion: negative saves.
	MonthlyChange float64 `json:"monthlyChange"`
}

// ContainerRightsizing compares a container's requests with what the VPA recommends.
type ContainerRightsizing struct {
	Name string `json:"name"`
	// Requests are the container's requests per pod; MemoryLimitBytes is 0 without a limit.
	Requests         Resources `json:"requests"`
	MemoryLimitBytes float64   `json:"memoryLimitBytes"`
	// Target, LowerBound and UpperBound are the VPA's; Suggested is Target rounded up to 10m of CPU and
	// 16Mi of memory.
	Target     Resources `json:"target"`
	LowerBound Resources `json:"lowerBound"`
	UpperBound Resources `json:"upperBound"`
	Suggested  Resources `json:"suggested"`
	// Unsettled is whether the VPA's upper bound is still far above its target, as it is while the VPA
	// has little history: the upper bound then says little about what the container needs.
	Unsettled bool `json:"unsettled"`
	// MonthlyChange is the cost change of requesting Suggested, per resource: negative saves.
	MonthlyChange ResourceCosts `json:"monthlyChange"`
}

// ResourceCosts is a monthly cost split between CPU and memory.
type ResourceCosts struct {
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
}
