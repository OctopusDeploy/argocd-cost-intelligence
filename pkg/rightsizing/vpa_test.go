package rightsizing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

const mi = 1024 * 1024

// goldilocksVPA is a VPA as Goldilocks creates it, in recommendation-only mode, after the
// recommender has filled in its status.
func goldilocksVPA(namespace, name, targetKind, target string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.k8s.io/v1",
		"kind":       "VerticalPodAutoscaler",
		"metadata":   map[string]any{"name": name, "namespace": namespace, "labels": map[string]any{"creator": "Fairwinds", "source": "goldilocks"}},
		"spec": map[string]any{
			"targetRef":    map[string]any{"apiVersion": "apps/v1", "kind": targetKind, "name": target},
			"updatePolicy": map[string]any{"updateMode": "Off"},
		},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "RecommendationProvided", "status": "True"},
				map[string]any{"type": "LowConfidence", "status": "True"},
			},
			"recommendation": map[string]any{"containerRecommendations": []any{
				map[string]any{
					"containerName":  "web",
					"target":         map[string]any{"cpu": "114m", "memory": "371Mi"},
					"lowerBound":     map[string]any{"cpu": "80m", "memory": "260Mi"},
					"upperBound":     map[string]any{"cpu": "1500m", "memory": "1Gi"},
					"uncappedTarget": map[string]any{"cpu": "114m", "memory": "371Mi"},
				},
			}},
		},
	}}
}

func TestParseVPA_ReadsTargetModeConfidenceAndRecommendations(t *testing.T) {
	t.Parallel()

	vpa, err := parseVPA(goldilocksVPA("shop", "goldilocks-web", "Deployment", "web").Object)
	require.NoError(t, err)

	assert.Equal(t, "Deployment", vpa.TargetKind)
	assert.Equal(t, "web", vpa.TargetName)
	assert.Equal(t, "Off", vpa.UpdateMode)
	assert.True(t, vpa.Recommends())
	assert.True(t, vpa.LowConfidence)
	require.Len(t, vpa.Containers, 1)
	rec := vpa.Containers[0]
	assert.Equal(t, "web", rec.Name)
	assert.InDelta(t, 0.114, rec.Target.CPUCores, 1e-9)
	assert.InDelta(t, 371*mi, rec.Target.MemoryBytes, 1)
	assert.InDelta(t, 0.08, rec.LowerBound.CPUCores, 1e-9)
	assert.InDelta(t, 1.5, rec.UpperBound.CPUCores, 1e-9)
	assert.InDelta(t, 1024*mi, rec.UpperBound.MemoryBytes, 1)
}

func TestParseVPA_DefaultsToAutoWithoutAnUpdatePolicy(t *testing.T) {
	t.Parallel()

	obj := goldilocksVPA("shop", "web", "Deployment", "web")
	unstructured.RemoveNestedField(obj.Object, "spec", "updatePolicy")
	unstructured.RemoveNestedField(obj.Object, "status")

	vpa, err := parseVPA(obj.Object)
	require.NoError(t, err)
	assert.Equal(t, "Auto", vpa.UpdateMode)
	assert.False(t, vpa.Recommends(), "a VPA in Auto mode sets requests itself")
	assert.False(t, vpa.LowConfidence)
	assert.Empty(t, vpa.Containers)
}

func newFakeDynamic(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{vpaResource: "VerticalPodAutoscalerList"}, objects...)
}

func TestVPA_OnlyRecommendsInOffMode(t *testing.T) {
	t.Parallel()

	// Every other mode sets requests itself, when pods are created or by evicting them.
	for mode, recommends := range map[string]bool{"Off": true, "Auto": false, "Initial": false, "Recreate": false, "InPlaceOrRecreate": false} {
		assert.Equal(t, recommends, VPA{UpdateMode: mode}.Recommends(), mode)
	}
}

func TestKubeLister_ListsTheNamespacesVPAs(t *testing.T) {
	t.Parallel()

	client := newFakeDynamic(
		goldilocksVPA("shop", "goldilocks-web", "Deployment", "web"),
		goldilocksVPA("other", "goldilocks-api", "Deployment", "api"),
	)

	vpas, err := NewKubeLister(client).List(context.Background(), "shop")

	require.NoError(t, err)
	require.Len(t, vpas, 1)
	assert.Equal(t, "web", vpas[0].TargetName)
}

func TestKubeLister_AMissingOrForbiddenVPAAPIIsUnavailable(t *testing.T) {
	t.Parallel()

	for name, apiErr := range map[string]error{
		"no VPA installed":      apierrors.NewNotFound(vpaResource.GroupResource(), ""),
		"no permission to list": apierrors.NewForbidden(vpaResource.GroupResource(), "", errors.New("RBAC")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newFakeDynamic()
			client.PrependReactor("list", "verticalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apiErr
			})

			_, err := NewKubeLister(client).List(context.Background(), "shop")
			assert.ErrorIs(t, err, ErrUnavailable)
		})
	}
}

// countingLister counts its calls and answers with err.
type countingLister struct {
	calls int
	err   error
}

func (l *countingLister) List(context.Context, string) ([]VPA, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return []VPA{{TargetName: "web"}}, nil
}

func TestCachedLister_KeepsAnswersForTheTTL(t *testing.T) {
	t.Parallel()

	next := &countingLister{}
	cached := NewCachedLister(next, time.Minute)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cached.now = func() time.Time { return now }

	for range 3 {
		vpas, err := cached.List(context.Background(), "shop")
		require.NoError(t, err)
		assert.Len(t, vpas, 1)
	}
	assert.Equal(t, 1, next.calls)

	_, _ = cached.List(context.Background(), "other")
	assert.Equal(t, 2, next.calls, "namespaces are cached apart")

	now = now.Add(time.Minute)
	_, _ = cached.List(context.Background(), "shop")
	assert.Equal(t, 3, next.calls, "an expired answer is listed again")
}

func TestCachedLister_CachesUnavailableButRetriesOtherErrors(t *testing.T) {
	t.Parallel()

	unavailable := &countingLister{err: ErrUnavailable}
	cached := NewCachedLister(unavailable, time.Minute)
	_, err := cached.List(context.Background(), "shop")
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = cached.List(context.Background(), "shop")
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, 1, unavailable.calls)

	failing := &countingLister{err: errors.New("timeout")}
	cached = NewCachedLister(failing, time.Minute)
	_, _ = cached.List(context.Background(), "shop")
	_, _ = cached.List(context.Background(), "shop")
	assert.Equal(t, 2, failing.calls)
}
