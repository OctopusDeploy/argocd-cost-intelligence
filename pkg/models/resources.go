package models

import (
	"regexp"
	"strings"
)

// ManagedResource is a resource Argo CD manages for an application, as its status lists it.
type ManagedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// ManagedResources are the resources of one application.
type ManagedResources []ManagedResource

// podOwnerKinds are the kinds whose pods an application's cost is made of.
var podOwnerKinds = map[string]bool{
	"deployment":  true,
	"statefulset": true,
	"daemonset":   true,
	"replicaset":  true,
	"job":         true,
	"cronjob":     true,
	"rollout":     true,
	"pod":         true,
}

// resourceName is a Kubernetes object name (DNS-1123 subdomain).
var resourceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)

// PodOwners keeps the resources that own pods, dropping everything else Argo CD lists (Services,
// ConfigMaps, ...). Returns an empty, non-nil list when there are none.
func (rs ManagedResources) PodOwners() ManagedResources {
	owners := ManagedResources{}
	for _, r := range rs {
		if podOwnerKinds[strings.ToLower(r.Kind)] {
			owners = append(owners, r)
		}
	}
	return owners
}

// Valid reports whether every resource has a valid Kubernetes object name, so names taken from a
// notification can safely be stored and matched.
func (rs ManagedResources) Valid() bool {
	for _, r := range rs {
		if !resourceName.MatchString(r.Name) {
			return false
		}
	}
	return true
}

// cronJobRun and rolloutReplicaSet match the suffixes of the controllers OpenCost reports for pods
// created on behalf of a CronJob (its Jobs, named <cronjob>-<scheduled time in minutes>) and of an Argo
// Rollout (its ReplicaSets, named <rollout>-<pod template hash>), which Argo CD lists under the parent
// only. The hash uses Kubernetes' safe alphabet, which has no vowels, so a Rollout named web does not
// claim a ReplicaSet named web-api.
var (
	cronJobRun        = regexp.MustCompile(`^-\d+$`)
	rolloutReplicaSet = regexp.MustCompile(`^-[bcdfghjklmnpqrstvwxz2456789]{5,10}$`)
)

// OwnsPod reports whether one of the resources owns a pod, given the namespace, controller kind and
// controller name OpenCost reports for it, and the pod's name. OpenCost reports a Deployment's pods
// under the Deployment, and a pod without a controller under no controller at all. A resource with no
// namespace (one Argo CD lists without it) matches in any namespace.
func (rs ManagedResources) OwnsPod(namespace, controllerKind, controller, pod string) bool {
	kind := strings.ToLower(controllerKind)
	for _, r := range rs {
		if r.Namespace != "" && r.Namespace != namespace {
			continue
		}
		switch strings.ToLower(r.Kind) {
		case "pod":
			if controller == "" && pod == r.Name {
				return true
			}
		case "cronjob":
			if (kind == "cronjob" && controller == r.Name) ||
				(kind == "job" && strings.HasPrefix(controller, r.Name) && cronJobRun.MatchString(controller[len(r.Name):])) {
				return true
			}
		case "rollout":
			if (kind == "rollout" && controller == r.Name) ||
				(kind == "replicaset" && strings.HasPrefix(controller, r.Name) && rolloutReplicaSet.MatchString(controller[len(r.Name):])) {
				return true
			}
		default:
			if strings.EqualFold(kind, r.Kind) && controller == r.Name {
				return true
			}
		}
	}
	return false
}
