package models

import "time"

// WasteRanking ranks the applications measured on their own by their idle cost: what their pods'
// unused CPU and memory requests cost per month. Applications measured only as their whole namespace
// are counted but never ranked, so the list compares applications with applications.
type WasteRanking struct {
	// Apps are ranked by IdleCost, the most first.
	Apps []AppWaste `json:"apps"`
	// IdleCost is the sum of the ranked applications' idle cost.
	IdleCost float64 `json:"idleCost"`
	// NamespaceOnly counts the applications whose latest record measured only their namespace.
	NamespaceOnly int `json:"namespaceOnly"`
	// NoPods counts the applications whose latest record found no pods of theirs.
	NoPods int `json:"noPods"`
}

// AppWaste is an application's idle cost from its latest record, with what a lower CPU request is
// priced from.
type AppWaste struct {
	ApplicationNamespace string    `json:"applicationNamespace"`
	ApplicationName      string    `json:"applicationName"`
	DeploymentNamespace  string    `json:"deploymentNamespace"`
	MeasuredAt           time.Time `json:"measuredAt"`
	// MonthlyCost is the application's own monthly cost.
	MonthlyCost float64 `json:"monthlyCost"`
	// CPUCost is the CPU part of MonthlyCost.
	CPUCost float64 `json:"cpuCost"`
	// IdleCost is the part of the CPU and memory cost paid for requests that went unused.
	IdleCost   float64           `json:"idleCost"`
	Efficiency EfficiencyMetrics `json:"efficiency"`
}
