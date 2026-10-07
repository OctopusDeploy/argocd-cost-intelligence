package exporter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// application is an Argo CD Application as the API returns it, with status.resources entries of
// kind/name or kind/name/namespace.
func application(namespace, name, destination string, resources ...[]string) *unstructured.Unstructured {
	listed := make([]any, 0, len(resources))
	for _, r := range resources {
		entry := map[string]any{"kind": r[0], "name": r[1], "version": "v1"}
		if len(r) > 2 {
			entry["namespace"] = r[2]
		}
		listed = append(listed, entry)
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"spec": map[string]any{
			"project":     "default",
			"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": destination},
		},
		"status": map[string]any{"resources": listed},
	}}
}

func newFakeDynamic(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{applicationResource: "ApplicationList"}, objects...)
}

func TestParseApplication_KeepsPodOwnersInTheirNamespaces(t *testing.T) {
	t.Parallel()

	app, err := parseApplication(application("argocd", "shop", "shop",
		[]string{"Service", "web", "shop"},
		[]string{"Deployment", "web", "shop"},
		[]string{"Rollout", "api"},                 // deployed to the destination's default namespace
		[]string{"StatefulSet", "db", "shop-data"}, // another namespace
		[]string{"ConfigMap", "settings", "shop"},
	).Object)

	require.NoError(t, err)
	assert.Equal(t, models.AppRef{Namespace: "argocd", Name: "shop"}, app.Ref)
	assert.Equal(t, models.ManagedResources{
		{Kind: "Deployment", Name: "web", Namespace: "shop"},
		{Kind: "Rollout", Name: "api", Namespace: "shop"},
		{Kind: "StatefulSet", Name: "db", Namespace: "shop-data"},
	}, app.Workloads)
}

func TestParseApplication_DropsWorkloadsItCannotPlace(t *testing.T) {
	t.Parallel()

	// No destination namespace: a resource Argo CD lists without one has no namespace to match pods in.
	app, err := parseApplication(application("argocd", "infra", "",
		[]string{"Deployment", "web"},
		[]string{"Deployment", "api", "infra"},
		[]string{"Deployment", "api", "infra"}, // listed once per API group
	).Object)

	require.NoError(t, err)
	assert.Equal(t, models.ManagedResources{{Kind: "Deployment", Name: "api", Namespace: "infra"}}, app.Workloads)
}

func TestParseApplication_WithoutResources(t *testing.T) {
	t.Parallel()

	app, err := parseApplication(application("argocd", "empty", "empty").Object)

	require.NoError(t, err)
	assert.NotNil(t, app.Workloads)
	assert.Empty(t, app.Workloads)
}

func TestKubeApplicationLister_ListsEveryNamespaceSorted(t *testing.T) {
	t.Parallel()

	client := newFakeDynamic(
		application("team-b", "web", "team-b", []string{"Deployment", "web"}),
		application("argocd", "redis", "argocd", []string{"Deployment", "redis"}),
		application("team-a", "web", "team-a", []string{"Deployment", "web"}),
	)

	apps, err := NewKubeApplicationLister(client).List(context.Background())

	require.NoError(t, err)
	refs := make([]string, 0, len(apps))
	for _, app := range apps {
		refs = append(refs, app.Ref.String())
	}
	assert.Equal(t, []string{"argocd:redis", "team-a:web", "team-b:web"}, refs,
		"apps-in-any-namespace included, same-named apps kept apart")
	assert.Equal(t, models.ManagedResources{{Kind: "Deployment", Name: "web", Namespace: "team-a"}}, apps[1].Workloads)
}

func TestKubeApplicationLister_ReportsAFailure(t *testing.T) {
	t.Parallel()

	client := newFakeDynamic()
	client.PrependReactor("list", "applications", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(applicationResource.GroupResource(), "", errors.New("RBAC"))
	})

	_, err := NewKubeApplicationLister(client).List(context.Background())

	require.Error(t, err)
	assert.True(t, apierrors.IsForbidden(err))
}
