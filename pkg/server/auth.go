package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/telemetry"
)

// Argo CD's extension proxy checks these headers against the caller's RBAC before forwarding a
// request: the caller must be allowed to get the named Application, and the project must be the
// Application's own. The proxy forwards both unchanged, so the backend can trust them as long as
// only argocd-server can reach it (see the chart's NetworkPolicy).
//
// The proxy does not check that the request path refers to the same Application, and it only sets
// Argocd-Username and Argocd-User-Groups when they are non-empty, so a client-supplied value passes
// through for a user without groups. Authorization here therefore relies on the Application header
// alone.
const headerApplicationName = "Argocd-Application-Name"

// authorizedAppKey is the request context key under which requireApp stores the Application it
// admitted the request for.
type authorizedAppKey struct{}

func callerApp(r *http.Request) (models.AppRef, bool) {
	app, err := models.ParseAppRef(r.Header.Get(headerApplicationName))
	return app, err == nil
}

func forbidden(w http.ResponseWriter, r *http.Request, msg string) {
	render.Status(r, http.StatusForbidden)
	render.JSON(w, r, map[string]string{"error": msg})
}

// requireCaller admits any request that names an Application, which every request through the
// proxy does. It guards endpoints that reveal nothing about a particular application.
func requireCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := callerApp(r); !ok {
			forbidden(w, r, "missing or invalid "+headerApplicationName+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireApp admits a request for /{appName}/... only when appName is the Application the proxy
// authorized. Without it, a user who can see one application could read any other by changing the
// path.
//
// The path carries only the name, so it cannot tell team-a:web from team-b:web. requireApp passes
// the header's full reference on in the request context, and handlers read it through appScoped
// to look up data by namespace and name.
func requireApp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app, ok := callerApp(r)
		if !ok {
			forbidden(w, r, "missing or invalid "+headerApplicationName+" header")
			return
		}
		if app.Name != chi.URLParam(r, "appName") {
			forbidden(w, r, headerApplicationName+" does not match the requested application")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authorizedAppKey{}, app)))
	})
}

// appScoped adapts a handler that serves one Application to take the Application requireApp
// admitted. It fails closed, so a route mounted without requireApp answers nothing.
func appScoped(h func(http.ResponseWriter, *http.Request, models.AppRef)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		app, ok := r.Context().Value(authorizedAppKey{}).(models.AppRef)
		if !ok {
			forbidden(w, r, "no authorized application for this request")
			return
		}
		h(w, r, app)
	}
}

// requireClusterScope admits a request only when it names the gate Application. Cluster-wide cost
// data is visible to whoever Argo CD RBAC allows to get that Application, and to no one when there is
// none.
func (s *CostAPIServer) requireClusterScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gate := s.clusterScopeApp(r.Context())
		if gate == nil {
			forbidden(w, r, "cluster-wide cost data is disabled: no Argo CD Application gates it; set CLUSTER_SCOPE_APP to choose one")
			return
		}
		if app, ok := callerApp(r); !ok || app != *gate {
			forbidden(w, r, "cluster-wide cost data requires access to the application "+gate.String())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireWebhookToken admits POST /on-sync only with the shared secret Argo CD Notifications sends
// as "Authorization: Bearer <token>". The webhook is not proxied, so no Argo CD header vouches for
// it: without the token, any pod that can reach the service could record measurements for any
// application. It fails closed when no secret is configured.
func (s *CostAPIServer) requireWebhookToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.webhookSecret == "" || !validBearer(r.Header.Get("Authorization"), s.webhookSecret) {
			s.metrics.ObserveWebhook(telemetry.WebhookUnauthorized)
			s.logger.Printf("Refused POST /on-sync from %s: missing or invalid webhook token. Argo CD Notifications "+
				"sends the cost-intel-webhook-token key of argocd-notifications-secret, which must equal WEBHOOK_SECRET\n", r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", "Bearer")
			render.Status(r, http.StatusUnauthorized)
			render.JSON(w, r, map[string]string{"error": "missing or invalid webhook token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validBearer reports whether header carries secret as a bearer token. Comparing digests keeps the
// comparison constant-time whatever the length of the presented token.
func validBearer(header, secret string) bool {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	got := sha256.Sum256([]byte(token))
	want := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}
