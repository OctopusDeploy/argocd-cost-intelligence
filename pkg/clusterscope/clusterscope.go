// Package clusterscope finds the Argo CD Application that gates cluster-wide cost data.
//
// Argo CD's extension proxy authorizes a request by the Application it names, so cluster-wide pages
// need one Application whose viewers may see them. Unless one is configured, the Resolver uses the
// Application that installed Cost Intelligence, read from Argo CD's tracking annotation on the
// service's own Deployment, and otherwise the access Application the Helm chart creates when Helm
// itself installs it.
package clusterscope

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// TrackingAnnotation is where Argo CD records which Application manages a resource, as
// "<app>:<group>/<kind>:<namespace>/<name>". <app> is the Application's name, prefixed with
// "<namespace>_" when it is outside Argo CD's own namespace.
const TrackingAnnotation = "argocd.argoproj.io/tracking-id"

// Source is how the gate Application was found.
type Source string

const (
	// SourceInstaller is the Application that installed Cost Intelligence.
	SourceInstaller Source = "installer"
	// SourceAccess is the access Application the Helm chart creates.
	SourceAccess Source = "access"
)

var (
	applicationResource = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
	deploymentResource  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// Workload is the service's own Deployment.
type Workload struct {
	Namespace string
	Name      string
}

// Resolver finds the gate Application and caches the answer for TTL, so a rename or a new
// Application is picked up without a restart.
type Resolver struct {
	client          dynamic.Interface
	self            Workload
	argoCDNamespace string
	access          *models.AppRef
	logger          *log.Logger

	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	app     *models.AppRef
	expires time.Time
	// answered is set once a lookup has succeeded; until then there is no last answer to serve.
	answered bool
	// lookup is closed when the running lookup ends; nil when none runs.
	lookup chan struct{}
	// reported is the last outcome logged, so each change is logged once.
	reported string
}

// TTL is how long a resolved gate Application is used before it is looked up again.
const TTL = time.Minute

// RetryAfter is how long a failed lookup waits before the next one.
const RetryAfter = 5 * time.Second

// lookupTimeout bounds one resolution, which runs on a request's behalf.
const lookupTimeout = 10 * time.Second

// errNoApplication means a source has no gate Application, so the next one is tried.
var errNoApplication = errors.New("no gate application")

// NewResolver reads through client. self is the service's Deployment; argoCDNamespace is where the
// Argo CD that may have installed it runs; access, when not nil, is the chart's access Application.
func NewResolver(client dynamic.Interface, self Workload, argoCDNamespace string, access *models.AppRef, logger *log.Logger) *Resolver {
	return &Resolver{
		client:          client,
		self:            self,
		argoCDNamespace: argoCDNamespace,
		access:          access,
		logger:          logger,
		ttl:             TTL,
		now:             time.Now,
	}
}

// App returns the gate Application, or nil when there is none. One request looks it up again once
// the answer expires; the others get the last answer meanwhile, and wait only when there is none
// yet. When a lookup fails, the last answer is kept and the lookup is retried after RetryAfter.
func (r *Resolver) App(ctx context.Context) *models.AppRef {
	r.mu.Lock()
	if r.now().Before(r.expires) {
		defer r.mu.Unlock()
		return r.app
	}
	if done := r.lookup; done != nil {
		if r.answered {
			defer r.mu.Unlock()
			return r.app
		}
		r.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.app
	}
	done := make(chan struct{})
	r.lookup = done
	r.mu.Unlock()
	return r.refresh(ctx, done)
}

// errLookupAborted is the outcome of a lookup that panicked.
var errLookupAborted = errors.New("lookup did not finish")

// refresh looks the gate Application up and records the outcome. Even if the lookup panics, the
// requests waiting for it are released and a later request looks again.
func (r *Resolver) refresh(ctx context.Context, done chan struct{}) (current *models.AppRef) {
	app, source, err := (*models.AppRef)(nil), Source(""), errLookupAborted
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.lookup = nil
		close(done)
		r.record(app, source, err)
		current = r.app
	}()

	// A request that is cancelled must not leave every other request without an answer.
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lookupTimeout)
	defer cancel()
	app, source, err = r.resolve(lookupCtx)
	return nil
}

// record stores a lookup's outcome; r.mu must be held.
func (r *Resolver) record(app *models.AppRef, source Source, err error) {
	if err != nil {
		r.expires = r.now().Add(RetryAfter)
		r.report("lookup failed: "+err.Error(), "Cluster-wide cost data: looking up the gate Application failed, keeping the last answer: %v\n", err)
		return
	}
	r.app, r.answered, r.expires = app, true, r.now().Add(r.ttl)
	switch {
	case app == nil:
		r.report("none", "Cluster-wide cost data off: no Argo CD Application installed Cost Intelligence and the chart's access Application does not exist; set CLUSTER_SCOPE_APP to choose one\n")
	case source == SourceInstaller:
		r.report(app.String(), "Cluster-wide cost data gated by application %s, which installed Cost Intelligence\n", app)
	default:
		r.report(app.String(), "Cluster-wide cost data gated by application %s, the chart's access Application\n", app)
	}
}

func (r *Resolver) report(outcome, format string, args ...any) {
	if outcome == r.reported {
		return
	}
	r.reported = outcome
	r.logger.Printf(format, args...)
}

// resolve returns a nil Application, and no error, when no source has one.
func (r *Resolver) resolve(ctx context.Context) (*models.AppRef, Source, error) {
	app, err := r.installer(ctx)
	if err == nil {
		return app, SourceInstaller, nil
	}
	if !errors.Is(err, errNoApplication) {
		return nil, "", err
	}
	app, err = r.accessApp(ctx)
	if err == nil {
		return app, SourceAccess, nil
	}
	if !errors.Is(err, errNoApplication) {
		return nil, "", err
	}
	return nil, "", nil
}

// installer returns the Application that manages the service's Deployment in this cluster's Argo
// CD, or nil when none does: installed without Argo CD, tracked by label only, or managed by an Argo
// CD elsewhere, whose Application is not here.
func (r *Resolver) installer(ctx context.Context) (*models.AppRef, error) {
	if r.self.Name == "" || r.self.Namespace == "" {
		return nil, errNoApplication
	}
	deployment, err := r.client.Resource(deploymentResource).Namespace(r.self.Namespace).Get(ctx, r.self.Name, metav1.GetOptions{})
	if absent(err) {
		return nil, errNoApplication
	}
	if err != nil {
		return nil, fmt.Errorf("reading Deployment %s/%s: %w", r.self.Namespace, r.self.Name, err)
	}
	ref, ok := ParseTrackingID(deployment.GetAnnotations()[TrackingAnnotation], r.argoCDNamespace)
	if !ok || ref.resource != (trackedResource{Group: "apps", Kind: "Deployment", Namespace: r.self.Namespace, Name: r.self.Name}) {
		return nil, errNoApplication
	}
	app, err := r.application(ctx, ref.app)
	if err != nil {
		return nil, err
	}
	// The annotation is copied along with the manifests, and a hub's Argo CD may name an Application
	// that only exists there, or a same-named one here that deploys elsewhere; the Application must be
	// here, deploy to this cluster and manage this Deployment.
	if !app.Spec.Destination.Local() {
		return nil, errNoApplication
	}
	for _, res := range app.Status.Resources {
		if res.Namespace == "" {
			res.Namespace = app.Spec.Destination.Namespace
		}
		if res == ref.resource {
			return &ref.app, nil
		}
	}
	return nil, errNoApplication
}

// accessApp returns the chart's access Application when it exists and carries the access label.
func (r *Resolver) accessApp(ctx context.Context) (*models.AppRef, error) {
	if r.access == nil {
		return nil, errNoApplication
	}
	app, err := r.application(ctx, *r.access)
	if err != nil {
		return nil, err
	}
	if app.Metadata.Labels[models.ClusterAccessLabel] != "true" {
		return nil, errNoApplication
	}
	return r.access, nil
}

// applicationObject is the part of an Application the resolver reads.
type applicationObject struct {
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Destination exporter.Destination `json:"destination"`
	} `json:"spec"`
	Status struct {
		Resources []trackedResource `json:"resources"`
	} `json:"status"`
}

// application reads ref, or returns errNoApplication when it does not exist or may not be read.
func (r *Resolver) application(ctx context.Context, ref models.AppRef) (*applicationObject, error) {
	obj, err := r.client.Resource(applicationResource).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if absent(err) {
		return nil, errNoApplication
	}
	if err != nil {
		return nil, fmt.Errorf("reading Application %s: %w", ref, err)
	}
	encoded, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, err
	}
	var app applicationObject
	if err := json.Unmarshal(encoded, &app); err != nil {
		return nil, fmt.Errorf("reading Application %s: %w", ref, err)
	}
	return &app, nil
}

// absent reports an error that means the object is not there for the service: missing, or hidden
// by RBAC. Neither changes until an operator acts, so the next source is tried instead.
func absent(err error) bool {
	return apierrors.IsNotFound(err) || apierrors.IsForbidden(err)
}

// trackedResource is a resource as a tracking ID and an Application's status name it.
type trackedResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// TrackingID is a parsed tracking annotation.
type TrackingID struct {
	app      models.AppRef
	resource trackedResource
}

// App is the Application the annotation names.
func (t TrackingID) App() models.AppRef { return t.app }

// ParseTrackingID reads a tracking annotation. A bare Application name is in argoCDNamespace.
func ParseTrackingID(id, argoCDNamespace string) (TrackingID, bool) {
	appPart, rest, ok := strings.Cut(id, ":")
	if !ok || appPart == "" {
		return TrackingID{}, false
	}
	groupKind, nsName, ok := strings.Cut(rest, ":")
	if !ok {
		return TrackingID{}, false
	}
	group, kind, ok := strings.Cut(groupKind, "/")
	if !ok || kind == "" {
		return TrackingID{}, false
	}
	ns, name, ok := strings.Cut(nsName, "/")
	if !ok || name == "" {
		return TrackingID{}, false
	}
	app := models.AppRef{Namespace: argoCDNamespace, Name: appPart}
	// Application names are DNS subdomains, which cannot contain "_".
	if appNs, appName, found := strings.Cut(appPart, "_"); found {
		if appNs == "" || appName == "" {
			return TrackingID{}, false
		}
		app = models.AppRef{Namespace: appNs, Name: appName}
	}
	if app.Namespace == "" {
		return TrackingID{}, false
	}
	return TrackingID{app: app, resource: trackedResource{Group: group, Kind: kind, Namespace: ns, Name: name}}, true
}
