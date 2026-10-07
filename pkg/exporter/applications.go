package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// Application is an Argo CD Application and the workloads it manages now.
type Application struct {
	Ref models.AppRef
	// Workloads are the pod-owning resources in the Application's status, each with its namespace.
	Workloads models.ManagedResources
}

// ApplicationLister lists every Argo CD Application in the cluster.
type ApplicationLister interface {
	List(ctx context.Context) ([]Application, error)
}

var applicationResource = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

// listPageSize bounds each request to the Kubernetes API, so a cluster with thousands of
// Applications is listed in pages.
const listPageSize = 500

// KubeApplicationLister reads Applications from the Kubernetes API, in every namespace, so
// apps-in-any-namespace are included.
type KubeApplicationLister struct {
	client dynamic.Interface
}

// NewKubeApplicationLister reads Applications through client.
func NewKubeApplicationLister(client dynamic.Interface) *KubeApplicationLister {
	return &KubeApplicationLister{client: client}
}

// List returns every Application, sorted by namespace and name.
func (l *KubeApplicationLister) List(ctx context.Context) ([]Application, error) {
	apps := []Application{}
	opts := metav1.ListOptions{Limit: listPageSize}
	for {
		list, err := l.client.Resource(applicationResource).Namespace(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("listing Argo CD Applications: %w", err)
		}
		for _, item := range list.Items {
			app, err := parseApplication(item.Object)
			if err != nil {
				return nil, fmt.Errorf("reading Application %s/%s: %w", item.GetNamespace(), item.GetName(), err)
			}
			apps = append(apps, app)
		}
		if list.GetContinue() == "" {
			break
		}
		opts.Continue = list.GetContinue()
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Ref.Namespace != apps[j].Ref.Namespace {
			return apps[i].Ref.Namespace < apps[j].Ref.Namespace
		}
		return apps[i].Ref.Name < apps[j].Ref.Name
	})
	return apps, nil
}

// applicationObject is the part of an Application the exporter reads.
type applicationObject struct {
	Metadata struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Destination struct {
			Namespace string `json:"namespace"`
		} `json:"destination"`
	} `json:"spec"`
	Status struct {
		Resources models.ManagedResources `json:"resources"`
	} `json:"status"`
}

// parseApplication keeps the Application's pod owners. Argo CD leaves out the namespace of a
// resource it deploys to the destination's default; such a resource is in the destination namespace.
// A pod owner with neither is dropped, and one listed twice is kept once.
func parseApplication(object map[string]any) (Application, error) {
	encoded, err := json.Marshal(object)
	if err != nil {
		return Application{}, err
	}
	var obj applicationObject
	if err := json.Unmarshal(encoded, &obj); err != nil {
		return Application{}, err
	}

	workloads := models.ManagedResources{}
	seen := map[models.ManagedResource]bool{}
	for _, w := range obj.Status.Resources.PodOwners() {
		if w.Namespace == "" {
			w.Namespace = obj.Spec.Destination.Namespace
		}
		// Without a namespace, the workload's pods cannot be told from another namespace's. Argo CD
		// lists a kind once per API group, so the same workload can appear twice.
		if w.Namespace == "" || seen[w] {
			continue
		}
		seen[w] = true
		workloads = append(workloads, w)
	}
	return Application{
		Ref:       models.AppRef{Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name},
		Workloads: workloads,
	}, nil
}
