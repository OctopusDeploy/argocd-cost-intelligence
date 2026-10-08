# Development

How to build the project, run the tests and try a change locally. For the contribution process (branches, sign-off, pull requests), see [CONTRIBUTING.md](../CONTRIBUTING.md). For how the code is organised, see [architecture.md](architecture.md).

You need the Go version in `go.mod`, Node.js 22, and Docker with Compose. `make lint-go` installs golangci-lint if it's missing; the other linters run in containers.

## Ways to check a change

From fastest to most realistic:

| Level | What it covers | Needs | Command |
|---|---|---|---|
| 1. Go unit tests | Handlers, the sync hook, OpenCost parsing, rightsizing, history, config | Go | `make test` |
| 2. Go integration tests | PostgreSQL storage and migrations, and the OpenCost client against a live OpenCost | Docker, and a live OpenCost for the second | `make test-integration` |
| 3. Prometheus rules | Recording rules and alerts | Docker | see below |
| 4. UI tests | Components and hooks | Node 22 | `cd ui && npm test` |
| 5. Docker Compose | The whole backend against the mock OpenCost | Docker | `docker-compose up -d` |
| 6. Kubernetes (k3d) | A real Argo CD, Notifications, the extension proxy and the Helm chart | k3d, kubectl, Helm 3 | `make helm-deploy` |

CI (`.github/workflows/ci.yml`) runs levels 1, 3 and 4, plus `go vet`, golangci-lint, `helm lint` and `helm template`, `actionlint`, `kubeconform`, `hadolint`, `shellcheck`, a check that `deploy/mock-opencost.yaml` matches `mock-opencost-server.js`, `govulncheck`, `npm audit` and gitleaks. `make lint` runs every linter locally, and each has its own target (`make help`). Levels 2, 5 and 6 are manual.

## Go unit tests

```bash
make test                                      # go test -v ./...
go test -v ./pkg/server/...                    # one package
go test -v -run TestName ./pkg/server/...      # one test
```

Tests sit next to the code in `*_test.go` files. A few conventions:

- Use table-driven tests when there are several cases, and `t.Parallel()` when the test shares no state.
- Put tests in the same package as the code (`package server`), so they can reach unexported helpers.
- Fake OpenCost with `httptest` servers and storage with in-memory fakes (see `pkg/history/fakes_test.go`). Unit tests never need a network or a database.
- A bug fix needs a test that fails without the fix.
- Authorization changes belong in `pkg/server/auth_test.go` and `app_scope_test.go`. For a new route, test that a header naming another application is refused.

## Go integration tests

Files with `//go:build integration` only run with `-tags=integration`.

```bash
make test-integration                       # both suites below, 120s timeout (the OpenCost tests skip if none is running)
go test -tags=integration ./pkg/storage/... # storage only
make test-opencost                          # OpenCost client only
```

`pkg/storage` starts a throwaway PostgreSQL 16 with testcontainers, so it needs a running Docker daemon and nothing else. It applies the real migrations, up and down.

`pkg/opencost` talks to a real OpenCost at `OPENCOST_BASE_URL` (default `http://localhost:9003`), for the namespace in `TEST_NAMESPACE`. The tests skip when nothing is listening, so `make test-integration` passes without it. CI runs them against the mock with `REQUIRE_OPENCOST=1`, which turns that skip into a failure. To run them, either start the mock (`node mock-opencost-server.js`, port 9003) or port-forward a real OpenCost (`kubectl port-forward svc/opencost 9003:9003 -n opencost`), then run `make test-opencost`.

## Prometheus rules

The recording rules and alerts in `helm/files/prometheus-rules.yaml` have `promtool` tests in `test/prometheus/`:

```bash
docker run --rm -v "$PWD:/w" -w /w --entrypoint sh prom/prometheus:v3.15.0 -c \
  'promtool check rules helm/files/prometheus-rules.yaml && promtool test rules test/prometheus/*_test.yaml'
```

## UI tests

```bash
cd ui
npm ci
npm run lint && npm run typecheck && npm test && npm run build
```

All four must pass, and `find src -name '*.d.ts'` must print nothing. The testing conventions (jsdom limits, fake timers, stubbing widths) are in the UI plugin section of [CLAUDE.md](../CLAUDE.md), and the plugin's layout is in [ui/README.md](../ui/README.md). The tests run on React 19, but the bundle also has to run on the React 16.14 of Argo CD 3.4, which ESLint enforces.

## Docker Compose

This starts the backend, PostgreSQL and the mock OpenCost (`mock-opencost-server.js`, with canned allocation data). There's no Kubernetes involved. It's always the mock, because a real OpenCost needs a cluster to read kubelet metrics from.

```bash
docker-compose up -d
curl http://localhost:8080/health      # backend
curl http://localhost:9003/health      # mock OpenCost
```

Send a sync webhook, wait for the 5-second measurement delay, then read the result. Application routes need the header that Argo CD's proxy would add (`Argocd-Application-Name: <namespace>:<name>`). Without it they answer 403.

```bash
curl -s -X POST http://localhost:8080/api/cost-analysis/on-sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer local-dev-webhook-secret" \
  -d '{"applicationName":"my-app","applicationNamespace":"argocd","namespace":"default",
       "resources":[{"kind":"Deployment","name":"my-app","namespace":"default"}],
       "syncTimestamp":"2026-06-14T10:00:00Z","syncResult":"Succeeded"}'
# 202 at once; the record is written about 5 seconds later
sleep 8

curl -s http://localhost:8080/api/cost-analysis/my-app/actual \
  -H "Argocd-Application-Name: argocd:my-app" | jq
curl -s 'http://localhost:8080/api/cost-analysis/my-app/trend?days=30' \
  -H "Argocd-Application-Name: argocd:my-app" | jq .statistics
```

Cluster-wide routes (`/namespaces`, `/namespace/{ns}/...`, `/chargeback`) answer 403 unless `CLUSTER_SCOPE_APP` is set. Add `CLUSTER_SCOPE_APP: "argocd:my-app"` to the service's `environment` in `docker-compose.yml` (or an override file) and send the same header.

These log lines (`docker-compose logs -f argocd-cost-intelligence`) tell you what happened:

| Line | Meaning |
|---|---|
| `Waiting 5s for metrics to propagate...`, then `Measured costs...` and `Successfully recorded actual cost` | It worked |
| `failed to query OpenCost` | OpenCost (the mock) can't be reached |
| `failed to save actual costs` | The database connection is broken |
| `Sync failed or unknown state ... skipping` | `syncResult` wasn't `Succeeded` |

To look at the database or clean up:

```bash
docker-compose exec postgres psql -U cost_user -d cost_intelligence \
  -c 'SELECT application_name, total_monthly_cost, measured_at FROM cost_actuals ORDER BY measured_at DESC LIMIT 10'
docker-compose down       # stop, keep data
docker-compose down -v    # stop and wipe the database
```

To run the binary without Docker, use `make run`. It needs a PostgreSQL reachable with the `DB_*` defaults.

## Kubernetes (k3d)

This is the end-to-end check, with a real Argo CD. You need a k3d cluster with Argo CD and its Notifications controller installed. [installation.md](installation.md) lists what the chart expects, and [configuration.md](configuration.md) covers its values. The Makefile deploys to the cluster named by `K3D_CLUSTER` (default `mycluster`), in your current kubectl context:

```bash
make helm-deploy K3D_CLUSTER=<your-k3d-cluster>        # mock OpenCost
make helm-deploy-real K3D_CLUSTER=<your-k3d-cluster>   # Prometheus + real OpenCost
make helm-undeploy                                     # remove either
```

You can also override `HELM_RELEASE`, `HELM_VALUES` (default `helm/values-local.yaml`), `ARGOCD_NS` (`argocd`), `APP_NS` (`argocd-cost-intel`) and `OPENCOST_NS` (`opencost`). `make help` shows the current values.

Both targets build the backend and UI images, import them into k3d, deploy OpenCost (mock or real) into `OPENCOST_NS` under the service name `opencost`, run `helm upgrade --install`, and restart the backend so it uses the rebuilt image. The chart's post-install hook registers the extension and patches Argo CD Notifications. Switching from one mode to the other removes the first.

| | Mock | Real |
|---|---|---|
| Command | `make helm-deploy` | `make helm-deploy-real` |
| OpenCost | `deploy/mock-opencost.yaml`, an embedded copy of `mock-opencost-server.js` (edit the script, then run `make sync-mock`; CI fails if they differ) | `opencost/opencost` with `prometheus-community/prometheus` (an existing Prometheus release is reused) |
| Costs | Predictable, and high | Tiny but real: actual usage at OpenCost's default pricing |
| Good for | Feature and UI work, the webhook pipeline | Checking that costs follow real consumption, efficiency figures, pre-release checks |

With real OpenCost, wait about 2 minutes after the install before you trigger a sync, or the costs read zero. A tiny workload costing `$0.02/month` is correct.

The database starts empty, and a record is written only when a sync is processed. Force-sync any application in the Argo CD UI, or post a webhook by hand:

```bash
kubectl port-forward svc/argocd-cost-intelligence 18080:8080 -n argocd-cost-intel &
TOKEN=$(kubectl get secret argocd-cost-intelligence-webhook -n argocd-cost-intel \
  -o jsonpath='{.data.token}' | base64 -d)
curl -s -X POST http://localhost:18080/api/cost-analysis/on-sync \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d "{\"applicationName\":\"redis\",\"applicationNamespace\":\"argocd\",\"namespace\":\"argocd\",\"syncTimestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"syncResult\":\"Succeeded\"}"
sleep 12
curl -s http://localhost:18080/api/cost-analysis/redis/actual -H "Argocd-Application-Name: argocd:redis" | jq
```

If a sync returns 202 but no data appears, follow the backend log while you send it (`kubectl logs -f deploy/argocd-cost-intelligence -n argocd-cost-intel`). The error shows within about 10 seconds. A 401 means the bearer token is wrong or missing. [operations.md](operations.md#troubleshooting) has more.

To check the wiring:

```bash
kubectl get pods -n argocd-cost-intel
kubectl get cm argocd-notifications-cm -n argocd -o yaml | grep -A5 cost-intel     # webhook and trigger
kubectl get cm argocd-cm -n argocd -o yaml | grep -A3 extension.config             # UI extension
make logs                                                                          # backend logs
```

To try a UI build without redeploying, see "Trying a build in a running Argo CD" in [CLAUDE.md](../CLAUDE.md). To look at OpenCost itself, `kubectl port-forward svc/opencost 9090:9090 -n opencost` serves its UI, and `http://localhost:9090/allocation?window=1d&aggregate=namespace` serves its API.

`make deploy-k8s` applies only the raw manifests in `deploy/`. It builds and imports nothing and doesn't wire up Argo CD, so use it for manifest changes, not as a shortcut for `helm-deploy`.

## Code style

Run `make fmt` before you commit. Formatting is `gofumpt` and `goimports`, and linting is golangci-lint v2 (`.golangci.yaml`; [CLAUDE.md](../CLAUDE.md) summarises the rules). Write comments only where the reason isn't obvious.
