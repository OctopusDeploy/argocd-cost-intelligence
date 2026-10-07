package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManagedResources_OwnsPod(t *testing.T) {
	t.Parallel()

	resources := ManagedResources{
		{Kind: "Deployment", Name: "web", Namespace: "shop"},
		{Kind: "StatefulSet", Name: "db", Namespace: "shop"},
		{Kind: "CronJob", Name: "report", Namespace: "shop"},
		{Kind: "Rollout", Name: "api", Namespace: "shop"},
		{Kind: "Pod", Name: "debug", Namespace: "shop"},
		{Kind: "DaemonSet", Name: "agent"},
	}

	tests := []struct {
		name                                 string
		namespace, kind, controller, podName string
		want                                 bool
	}{
		{"deployment", "shop", "deployment", "web", "web-7d4b8c-xk", true},
		{"kind compared case-insensitively", "shop", "Deployment", "web", "web-7d4b8c-xk", true},
		{"same name, other kind", "shop", "statefulset", "web", "web-0", false},
		{"same name, other namespace", "billing", "deployment", "web", "web-7d4b8c-xk", false},
		{"statefulset", "shop", "statefulset", "db", "db-0", true},
		{"job of the cronjob", "shop", "job", "report-29312345", "report-29312345-abcde", true},
		{"cronjob reported as itself", "shop", "cronjob", "report", "report-29312345-abcde", true},
		{"job named like the cronjob", "shop", "job", "report-weekly", "report-weekly-abcde", false},
		{"rollout reported as itself", "shop", "rollout", "api", "api-6f7c9-x", true},
		{"replicaset of the rollout", "shop", "replicaset", "api-6f7c9b8d5", "api-6f7c9b8d5-x", true},
		{"replicaset named like the rollout", "shop", "replicaset", "api-gateway", "api-gateway-x", false},
		{"bare pod", "shop", "", "", "debug", true},
		{"bare pod of another name", "shop", "", "", "debug-2", false},
		{"pod of that name with a controller", "shop", "job", "cleanup", "debug", false},
		{"resource listed without a namespace", "anywhere", "daemonset", "agent", "agent-x", true},
		{"unmanaged controller", "shop", "deployment", "argocd-redis", "argocd-redis-x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, resources.OwnsPod(tt.namespace, tt.kind, tt.controller, tt.podName))
		})
	}
}

func TestManagedResources_PodOwners(t *testing.T) {
	t.Parallel()

	resources := ManagedResources{
		{Kind: "Service", Name: "redis"},
		{Kind: "Deployment", Name: "redis"},
		{Kind: "ConfigMap", Name: "redis-config"},
		{Kind: "CronJob", Name: "test-cronjob"},
	}

	assert.Equal(t, ManagedResources{{Kind: "Deployment", Name: "redis"}, {Kind: "CronJob", Name: "test-cronjob"}}, resources.PodOwners())
	assert.Equal(t, ManagedResources{}, ManagedResources(nil).PodOwners(), "never nil")
}

func TestManagedResources_Valid(t *testing.T) {
	t.Parallel()

	assert.True(t, ManagedResources{{Kind: "Deployment", Name: "redis"}, {Kind: "Pod", Name: "a.b-c"}}.Valid())
	assert.True(t, ManagedResources{}.Valid())
	assert.False(t, ManagedResources{{Kind: "Deployment", Name: "Redis"}}.Valid())
	assert.False(t, ManagedResources{{Kind: "Deployment", Name: ""}}.Valid())
	assert.False(t, ManagedResources{{Kind: "Deployment", Name: `redis","x`}}.Valid())
}

func TestCostActual_ForApplication(t *testing.T) {
	t.Parallel()

	record := CostActual{
		ID:               "r1",
		TotalMonthlyCost: 9.55,
		Pods:             []PodCost{{Name: "argocd-redis-1"}, {Name: "redis-1"}},
		Efficiency:       &EfficiencyMetrics{CPUEfficiency: 0.02},
	}

	namespace := record.ForApplication()
	assert.Equal(t, ScopeNamespace, namespace.Scope)
	assert.InDelta(t, 9.55, namespace.TotalMonthlyCost, 1e-9)

	record.App = &AppMeasurement{
		TotalMonthlyCost: 0.63,
		OpenCostOutput:   OpenCostMetrics{AllocationTotals: AllocationTotal{TotalCost: 0.63}},
		Efficiency:       &EfficiencyMetrics{CPUEfficiency: 0.96},
		Pods:             []PodCost{{Name: "redis-1"}},
	}
	app := record.ForApplication()
	assert.Equal(t, ScopeApplication, app.Scope)
	assert.Equal(t, "r1", app.ID)
	assert.InDelta(t, 0.63, app.TotalMonthlyCost, 1e-9)
	assert.InDelta(t, 0.63, app.OpenCostOutput.AllocationTotals.TotalCost, 1e-9)
	assert.InDelta(t, 0.96, app.Efficiency.CPUEfficiency, 1e-9)
	assert.Equal(t, []PodCost{{Name: "redis-1"}}, app.Pods)
	assert.Nil(t, app.App, "the stored measurement is not sent twice")
	assert.InDelta(t, 9.55, record.TotalMonthlyCost, 1e-9, "the record itself is unchanged")
}
