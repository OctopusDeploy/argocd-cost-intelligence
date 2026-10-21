package clusterscope

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

var (
	self   = Workload{Namespace: "cost-intel", Name: "argocd-cost-intelligence"}
	access = models.AppRef{Namespace: "argocd", Name: "cost-intelligence"}
)

// deployment is the service's Deployment, with trackingID as its tracking annotation when not empty.
func deployment(trackingID string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": self.Name, "namespace": self.Namespace},
	}}
	if trackingID != "" {
		obj.SetAnnotations(map[string]string{TrackingAnnotation: trackingID})
	}
	return obj
}

// application is an Argo CD Application deploying to server, managing resources ([group, kind,
// namespace, name]).
func application(ref models.AppRef, server string, labels map[string]string, resources ...[4]string) *unstructured.Unstructured {
	listed := make([]any, 0, len(resources))
	for _, r := range resources {
		listed = append(listed, map[string]any{"group": r[0], "kind": r[1], "namespace": r[2], "name": r[3], "version": "v1", "status": "Synced"})
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   map[string]any{"name": ref.Name, "namespace": ref.Namespace},
		"spec":       map[string]any{"destination": map[string]any{"server": server, "namespace": self.Namespace}},
		"status":     map[string]any{"resources": listed},
	}}
	obj.SetLabels(labels)
	return obj
}

var ownDeployment = [4]string{"apps", "Deployment", self.Namespace, self.Name}

const local = "https://kubernetes.default.svc"

func accessLabels() map[string]string { return map[string]string{models.ClusterAccessLabel: "true"} }

func newClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		applicationResource: "ApplicationList",
		deploymentResource:  "DeploymentList",
	}, objects...)
}

func newResolver(client *dynamicfake.FakeDynamicClient, accessApp *models.AppRef) (*Resolver, *bytes.Buffer) {
	var logs bytes.Buffer
	return NewResolver(client, self, "argocd", accessApp, log.New(&logs, "", 0)), &logs
}

func TestResolver_FindsTheApplicationThatInstalledIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		trackingID string
		app        models.AppRef
	}{
		{"in Argo CD's namespace", "cost:apps/Deployment:cost-intel/argocd-cost-intelligence", models.AppRef{Namespace: "argocd", Name: "cost"}},
		{"in any namespace", "platform_cost:apps/Deployment:cost-intel/argocd-cost-intelligence", models.AppRef{Namespace: "platform", Name: "cost"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := newClient(
				deployment(tc.trackingID),
				application(tc.app, local, nil, ownDeployment),
				application(access, local, accessLabels()),
			)
			resolver, logs := newResolver(client, &access)

			assert.Equal(t, &tc.app, resolver.App(context.Background()), "the installer wins over the access Application")
			assert.Contains(t, logs.String(), "which installed Cost Intelligence")
		})
	}
}

func TestResolver_FallsBackToTheAccessApplication(t *testing.T) {
	t.Parallel()

	installer := models.AppRef{Namespace: "argocd", Name: "cost"}
	tracked := "cost:apps/Deployment:cost-intel/argocd-cost-intelligence"
	for _, tc := range []struct {
		name    string
		objects []runtime.Object
	}{
		{"installed by Helm", []runtime.Object{deployment("")}},
		{"Deployment not found", nil},
		{"installer Application is in another Argo CD", []runtime.Object{deployment(tracked)}},
		{
			"installer Application deploys to another cluster",
			[]runtime.Object{deployment(tracked), application(installer, "https://hub.example.com", nil, ownDeployment)},
		},
		{
			"same-named Application manages something else",
			[]runtime.Object{deployment(tracked), application(installer, local, nil, [4]string{"apps", "Deployment", "shop", "web"})},
		},
		{
			"annotation copied from another resource",
			[]runtime.Object{deployment("cost:apps/Deployment:shop/web"), application(installer, local, nil, [4]string{"apps", "Deployment", "shop", "web"})},
		},
		{"annotation unreadable", []runtime.Object{deployment("cost")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := newClient(append(tc.objects, application(access, local, accessLabels()))...)
			resolver, logs := newResolver(client, &access)

			assert.Equal(t, &access, resolver.App(context.Background()))
			assert.Contains(t, logs.String(), "the chart's access Application")
		})
	}
}

func TestResolver_FallsBackWhenTheDeploymentMayNotBeRead(t *testing.T) {
	t.Parallel()

	client := newClient(application(access, local, accessLabels()))
	client.PrependReactor("get", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(deploymentResource.GroupResource(), self.Name, errors.New("RBAC"))
	})
	resolver, _ := newResolver(client, &access)

	assert.Equal(t, &access, resolver.App(context.Background()))
}

func TestResolver_None(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		accessApp *models.AppRef
		objects   []runtime.Object
	}{
		{"no access Application configured", nil, []runtime.Object{application(access, local, accessLabels())}},
		{"access Application not created", &access, nil},
		{"access Application without the label", &access, []runtime.Object{application(access, local, nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver, logs := newResolver(newClient(append(tc.objects, deployment(""))...), tc.accessApp)

			assert.Nil(t, resolver.App(context.Background()))
			assert.Contains(t, logs.String(), "Cluster-wide cost data off")
		})
	}
}

func TestResolver_CachesAndPicksUpChanges(t *testing.T) {
	t.Parallel()

	client := newClient(deployment(""))
	resolver, logs := newResolver(client, &access)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	ctx := context.Background()

	require.Nil(t, resolver.App(ctx))
	_, err := client.Resource(applicationResource).Namespace(access.Namespace).
		Create(ctx, application(access, local, accessLabels()), metav1.CreateOptions{})
	require.NoError(t, err)

	assert.Nil(t, resolver.App(ctx), "cached until the TTL passes")
	now = now.Add(TTL)
	assert.Equal(t, &access, resolver.App(ctx), "a new access Application is picked up")
	now = now.Add(TTL)
	resolver.App(ctx)
	assert.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("gated by application argocd:cost-intelligence")), "each change logged once")
}

func TestResolver_KeepsTheLastAnswerWhenALookupFails(t *testing.T) {
	t.Parallel()

	client := newClient(deployment(""), application(access, local, accessLabels()))
	resolver, logs := newResolver(client, &access)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	ctx := context.Background()
	require.Equal(t, &access, resolver.App(ctx))

	client.PrependReactor("get", "applications", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("API server restarting")
	})
	now = now.Add(TTL)

	assert.Equal(t, &access, resolver.App(ctx))
	assert.Contains(t, logs.String(), "keeping the last answer")
}

func TestResolver_RetriesSoonAfterAFailedLookup(t *testing.T) {
	t.Parallel()

	client := newClient(deployment(""), application(access, local, accessLabels()))
	failing := true
	client.PrependReactor("get", "applications", func(k8stesting.Action) (bool, runtime.Object, error) {
		if failing {
			return true, nil, apierrors.NewServiceUnavailable("API server starting")
		}
		return false, nil, nil
	})
	resolver, _ := newResolver(client, &access)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	ctx := context.Background()

	require.Nil(t, resolver.App(ctx))
	failing = false
	assert.Nil(t, resolver.App(ctx), "not looked up again at once")
	now = now.Add(RetryAfter)
	assert.Equal(t, &access, resolver.App(ctx), "looked up again well before the TTL")
}

// blockApplications makes each Get of an Application wait for release, signalling entered first,
// and counts the Gets.
func blockApplications(client *dynamicfake.FakeDynamicClient) (entered, release chan struct{}, gets *atomic.Int32) {
	entered, release, gets = make(chan struct{}, 1), make(chan struct{}), &atomic.Int32{}
	client.PrependReactor("get", "applications", func(k8stesting.Action) (bool, runtime.Object, error) {
		gets.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return false, nil, nil
	})
	return entered, release, gets
}

func TestResolver_ServesTheLastAnswerDuringALookup(t *testing.T) {
	t.Parallel()

	client := newClient(deployment(""), application(access, local, accessLabels()))
	resolver, _ := newResolver(client, &access)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	ctx := context.Background()
	require.Equal(t, &access, resolver.App(ctx))

	entered, release, _ := blockApplications(client)
	now = now.Add(TTL)
	looking := make(chan *models.AppRef)
	go func() { looking <- resolver.App(ctx) }()
	<-entered

	assert.Equal(t, &access, resolver.App(ctx), "answered without waiting for the running lookup")
	close(release)
	assert.Equal(t, &access, <-looking)
}

func TestResolver_FirstRequestsWaitForTheFirstLookup(t *testing.T) {
	t.Parallel()

	client := newClient(deployment(""), application(access, local, accessLabels()))
	resolver, _ := newResolver(client, &access)
	entered, release, gets := blockApplications(client)
	looking := make(chan *models.AppRef)
	go func() { looking <- resolver.App(context.Background()) }()
	<-entered

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Nil(t, resolver.App(cancelled), "a request that gives up has no answer yet")

	waiting := make(chan *models.AppRef)
	go func() { waiting <- resolver.App(context.Background()) }()
	close(release)
	assert.Equal(t, &access, <-looking)
	assert.Equal(t, &access, <-waiting)
	assert.Equal(t, int32(1), gets.Load(), "one lookup for every request")
}

func TestResolver_RecoversFromALookupThatPanics(t *testing.T) {
	t.Parallel()

	client := newClient(deployment(""), application(access, local, accessLabels()))
	panicking := true
	client.PrependReactor("get", "applications", func(k8stesting.Action) (bool, runtime.Object, error) {
		if panicking {
			panic("broken client")
		}
		return false, nil, nil
	})
	resolver, _ := newResolver(client, &access)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }
	ctx := context.Background()

	require.Panics(t, func() { resolver.App(ctx) })
	panicking = false
	assert.Nil(t, resolver.App(ctx), "no answer, and no wait for the lookup that panicked")
	now = now.Add(RetryAfter)
	assert.Equal(t, &access, resolver.App(ctx), "looked up again")
}

func TestResolver_SurvivesACancelledRequest(t *testing.T) {
	t.Parallel()

	client := cancellable{newClient(deployment(""), application(access, local, accessLabels()))}
	resolver := NewResolver(client, self, "argocd", &access, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.Equal(t, &access, resolver.App(ctx))
}

// cancellable fails a Get whose context is done, as a real client does; the fake ignores it.
type cancellable struct{ dynamic.Interface }

func (c cancellable) Resource(r schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return cancellableResource{c.Interface.Resource(r)}
}

type cancellableResource struct {
	dynamic.NamespaceableResourceInterface
}

func (r cancellableResource) Namespace(ns string) dynamic.ResourceInterface {
	return cancellableNamespaced{r.NamespaceableResourceInterface.Namespace(ns)}
}

type cancellableNamespaced struct{ dynamic.ResourceInterface }

func (r cancellableNamespaced) Get(ctx context.Context, name string, opts metav1.GetOptions, sub ...string) (*unstructured.Unstructured, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.ResourceInterface.Get(ctx, name, opts, sub...)
}

func TestParseTrackingID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		id     string
		want   models.AppRef
		wantOK bool
	}{
		{"cost:apps/Deployment:cost-intel/web", models.AppRef{Namespace: "argocd", Name: "cost"}, true},
		{"team_cost:apps/Deployment:cost-intel/web", models.AppRef{Namespace: "team", Name: "cost"}, true},
		{":/Namespace:/cost-intel", models.AppRef{}, false}, // the cluster-scoped empty group is fine, the app is not
		{"cost:/Namespace:/cost-intel", models.AppRef{Namespace: "argocd", Name: "cost"}, true},
		{"_cost:apps/Deployment:cost-intel/web", models.AppRef{}, false},
		{"team_:apps/Deployment:cost-intel/web", models.AppRef{}, false},
		{"cost", models.AppRef{}, false},
		{"cost:apps/Deployment", models.AppRef{}, false},
		{"cost:apps/Deployment:web", models.AppRef{}, false},
		{"", models.AppRef{}, false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseTrackingID(tc.id, "argocd")
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got.App())
		})
	}
}
