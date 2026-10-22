// Package rightsizing reads the Vertical Pod Autoscaler's recommendations for an application's
// workloads (from Goldilocks or any VPA) and prices them with OpenCost.
package rightsizing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// ErrUnavailable means the VPA API cannot be read: the cluster has no VPA, or the service may not
// list it.
var ErrUnavailable = errors.New("the VerticalPodAutoscaler API is not available")

// VPA is the part of a VerticalPodAutoscaler the service reads.
type VPA struct {
	TargetKind string
	TargetName string
	// UpdateMode is "Off" when the VPA only recommends; the VPA's default, when unset, is "Auto".
	UpdateMode string
	// LowConfidence is the VPA's own flag for a recommendation based on too little history. The
	// upstream recommender never sets it, but the API defines it.
	LowConfidence bool
	// RecommendingSince is when the VPA's RecommendationProvided condition last turned true: how long
	// it has had a recommendation. It is zero when the VPA does not say.
	RecommendingSince time.Time
	Containers        []ContainerRecommendation
}

// Recommends reports whether the VPA leaves requests to people, only recommending them.
func (v VPA) Recommends() bool {
	return v.UpdateMode == "Off"
}

// ContainerRecommendation is the VPA's recommendation for one container.
type ContainerRecommendation struct {
	Name       string
	Target     models.Resources
	LowerBound models.Resources
	UpperBound models.Resources
}

// Lister lists the VPAs in a namespace.
type Lister interface {
	List(ctx context.Context, namespace string) ([]VPA, error)
}

var vpaResource = schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}

// KubeLister reads VPAs from the Kubernetes API.
type KubeLister struct {
	client dynamic.Interface
}

// NewKubeLister reads VPAs through client.
func NewKubeLister(client dynamic.Interface) *KubeLister {
	return &KubeLister{client: client}
}

// NewInClusterLister reads VPAs with the pod's service account. It fails outside a cluster.
func NewInClusterLister() (*KubeLister, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating Kubernetes client: %w", err)
	}
	return NewKubeLister(client), nil
}

// Check lists one VPA in any namespace, to tell whether the cluster serves the VPA API and the
// service may read it. It returns ErrUnavailable when the API is missing or forbidden.
func (l *KubeLister) Check(ctx context.Context) error {
	_, err := l.client.Resource(vpaResource).List(ctx, metav1.ListOptions{Limit: 1})
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err != nil {
		return fmt.Errorf("listing VerticalPodAutoscalers: %w", err)
	}
	return nil
}

// List returns the VPAs in namespace, or ErrUnavailable when the VPA API is missing or forbidden.
func (l *KubeLister) List(ctx context.Context, namespace string) ([]VPA, error) {
	list, err := l.client.Resource(vpaResource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err != nil {
		return nil, fmt.Errorf("listing VerticalPodAutoscalers in %s: %w", namespace, err)
	}

	vpas := make([]VPA, 0, len(list.Items))
	for _, item := range list.Items {
		vpa, err := parseVPA(item.Object)
		if err != nil {
			return nil, fmt.Errorf("reading VerticalPodAutoscaler %s/%s: %w", namespace, item.GetName(), err)
		}
		vpas = append(vpas, vpa)
	}
	return vpas, nil
}

// vpaObject is the shape of the fields read from a VerticalPodAutoscaler.
type vpaObject struct {
	Spec struct {
		TargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"targetRef"`
		UpdatePolicy *struct {
			UpdateMode string `json:"updateMode"`
		} `json:"updatePolicy"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type               string      `json:"type"`
			Status             string      `json:"status"`
			LastTransitionTime metav1.Time `json:"lastTransitionTime"`
		} `json:"conditions"`
		Recommendation *struct {
			ContainerRecommendations []struct {
				ContainerName string                       `json:"containerName"`
				Target        map[string]resource.Quantity `json:"target"`
				LowerBound    map[string]resource.Quantity `json:"lowerBound"`
				UpperBound    map[string]resource.Quantity `json:"upperBound"`
			} `json:"containerRecommendations"`
		} `json:"recommendation"`
	} `json:"status"`
}

func parseVPA(object map[string]any) (VPA, error) {
	encoded, err := json.Marshal(object)
	if err != nil {
		return VPA{}, err
	}
	var obj vpaObject
	if err := json.Unmarshal(encoded, &obj); err != nil {
		return VPA{}, err
	}

	vpa := VPA{TargetKind: obj.Spec.TargetRef.Kind, TargetName: obj.Spec.TargetRef.Name, UpdateMode: "Auto"}
	if obj.Spec.UpdatePolicy != nil && obj.Spec.UpdatePolicy.UpdateMode != "" {
		vpa.UpdateMode = obj.Spec.UpdatePolicy.UpdateMode
	}
	for _, condition := range obj.Status.Conditions {
		if !strings.EqualFold(condition.Status, "True") {
			continue
		}
		switch condition.Type {
		case "LowConfidence":
			vpa.LowConfidence = true
		case "RecommendationProvided":
			vpa.RecommendingSince = condition.LastTransitionTime.Time
		}
	}
	if obj.Status.Recommendation != nil {
		for _, rec := range obj.Status.Recommendation.ContainerRecommendations {
			vpa.Containers = append(vpa.Containers, ContainerRecommendation{
				Name:       rec.ContainerName,
				Target:     amounts(rec.Target),
				LowerBound: amounts(rec.LowerBound),
				UpperBound: amounts(rec.UpperBound),
			})
		}
	}
	return vpa, nil
}

func amounts(quantities map[string]resource.Quantity) models.Resources {
	var r models.Resources
	if cpu, ok := quantities["cpu"]; ok {
		r.CPUCores = cpu.AsApproximateFloat64()
	}
	if memory, ok := quantities["memory"]; ok {
		r.MemoryBytes = memory.AsApproximateFloat64()
	}
	return r
}

// CachedLister keeps each namespace's VPAs, or the error listing them, for ttl. Recommendations
// change slowly, and every viewer of an application would otherwise list them again.
type CachedLister struct {
	next    Lister
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	vpas    []VPA
	err     error
	expires time.Time
}

// NewCachedLister caches what next lists for ttl.
func NewCachedLister(next Lister, ttl time.Duration) *CachedLister {
	return &CachedLister{next: next, ttl: ttl, now: time.Now, entries: map[string]cacheEntry{}}
}

// List returns namespace's VPAs from the cache, or lists them when the cached answer has expired.
// Only ErrUnavailable is cached among errors, so a passing failure is retried on the next request.
func (c *CachedLister) List(ctx context.Context, namespace string) ([]VPA, error) {
	c.mu.Lock()
	entry, ok := c.entries[namespace]
	c.mu.Unlock()
	if ok && c.now().Before(entry.expires) {
		return entry.vpas, entry.err
	}

	vpas, err := c.next.List(ctx, namespace)
	if err == nil || errors.Is(err, ErrUnavailable) {
		c.mu.Lock()
		c.entries[namespace] = cacheEntry{vpas: vpas, err: err, expires: c.now().Add(c.ttl)}
		c.mu.Unlock()
	}
	return vpas, err
}
