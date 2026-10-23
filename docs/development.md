# Development

How to run the project locally, check a change and try it in a real Argo CD. For the contribution process and the test conventions, see [CONTRIBUTING.md](../CONTRIBUTING.md). For how the service works inside, see [Architecture](architecture.md).

## Prerequisites

- Go, the version in `go.mod`
- Node.js 22
- Docker with Compose v2 (`docker compose`)
- jq
- Helm 3, for `make lint` and the cluster
- k3d and kubectl, for the cluster
- Python 3, for the docs site

## Quick start

This runs the backend, PostgreSQL and a mock OpenCost with made-up cost data. No Kubernetes is involved.

```bash
git clone https://github.com/OctopusDeploy/argocd-cost-intelligence.git
cd argocd-cost-intelligence
docker compose up -d --wait
scripts/test-api.sh
```

The script sends a sync webhook the way Argo CD Notifications would, waits for the cost record and reads it back through the API. `docker compose down -v` stops everything and wipes the database.

To run the backend from source instead, start only its dependencies and use `make run`:

```bash
docker compose up -d postgres mock-opencost
make run
```

## Checking a change

```bash
make test               # Go unit tests
make test-integration   # storage against a throwaway PostgreSQL, and the OpenCost client
make lint               # every linter: Go, UI, Helm, Prometheus rules, workflows, manifests, Dockerfiles, scripts

cd ui
npm ci
npm run lint && npm run typecheck && npm test && npm run build
```

To run one Go test, use `go test -run TestName ./pkg/server/...`.

The integration tests need a running Docker daemon. The OpenCost client tests skip unless something answers on port 9003, so start the mock first (`docker compose up -d mock-opencost`) if you changed the client.

`golangci-lint fmt` formats Go code the way the linter checks it.

CI runs all of these on every pull request, and also builds the images and scans for vulnerabilities and leaked secrets.

## Running it in Argo CD

This is the end-to-end check: a real Argo CD with its Notifications controller, the extension proxy and the Helm chart. You need a k3d cluster with Argo CD installed, and your kubectl context pointing at it.

```bash
make helm-deploy K3D_CLUSTER=<your-cluster>        # with the mock OpenCost
make helm-deploy-real K3D_CLUSTER=<your-cluster>   # with Prometheus and a real OpenCost
make helm-undeploy                                 # remove either
```

Both build the images, import them into k3d, deploy OpenCost and install the chart. The chart registers the extension and sets up Argo CD Notifications. `make help` lists the other settings you can override.

The mock reads the pods that actually run in each namespace and gives them made-up but stable costs, scaled up so small local pods show realistic amounts. Use it for UI and feature work. The real OpenCost measures actual usage at its default prices, so local costs are tiny. Use it to check that figures follow real consumption.

The database starts empty. Sync any application in the Argo CD UI to record its cost, then open its Cost Intelligence tab.

To try a UI change without redeploying, swap the bundle into the running `argocd-server`; see [ui/README.md](../ui/README.md#try-a-build-in-a-running-argo-cd).

## Troubleshooting

Follow the backend log while you reproduce the problem: `docker compose logs -f argocd-cost-intelligence`, or `make logs` in the cluster.

| You see | What it means |
|---|---|
| `401` from `/on-sync` | The bearer token is missing or wrong. Locally it is `local-dev-webhook-secret`; in the cluster it is in the `argocd-cost-intelligence-webhook` Secret. |
| `403` from an application route | The `Argocd-Application-Name` header is missing or names another application. Argo CD's proxy adds it; by hand, send `<namespace>:<name>`. |
| `403` from a cluster-wide route outside a cluster | The service can't find the Application that installed it. Set `CLUSTER_SCOPE_APP` (for example `argocd:nginx`) in `docker-compose.yml`. |
| `Sync failed or unknown state ... skipping` | The webhook's `syncResult` wasn't `Succeeded`. |
| `failed to query OpenCost` | OpenCost, or the mock, can't be reached. |
| `failed to save actual costs` | The database can't be reached. |
| The namespace has a cost but the application shows zero | The webhook's `resources` don't own any pod OpenCost reports. Outside a cluster the mock only knows a fixed set of pods, such as those of the `nginx` Deployment in `default`. |
| `OpenCost has no data for namespace ... yet` | OpenCost has less than five minutes of data for the namespace, or no cost for it yet, so nothing is recorded. The service measures it again after 1, 2, 4 and 8 minutes. |
| A sync in Argo CD records nothing | Check that the chart's hook set up Notifications: `kubectl get cm argocd-notifications-cm -n argocd -o yaml` should list a `cost-intel` trigger and template. |

[Operations](operations.md#troubleshooting) covers problems in a real installation.

## Documentation site

The pages under `docs/`, the README and `SECURITY.md` are published on Read the Docs with MkDocs. Write ordinary relative links that work on GitHub: links to files outside `docs/` are rewritten to GitHub at build time. A new page needs an entry in the `nav` of `mkdocs.yml`.

`make docs` builds the site and fails on a broken link or anchor, as CI does. `make docs-serve` serves it with live reload.

When you write or change a page:

- Keep to one idea per paragraph.
- Link to a page by its title, or a section by its heading, never by the file name: write `[Configuration](configuration.md)`, not `[configuration.md](configuration.md)`. Files that aren't on the site, such as `CONTRIBUTING.md`, keep their names.
- Take screenshots in Argo CD's dark theme, on a local cluster with the mock OpenCost (see [Running it in Argo CD](#running-it-in-argo-cd)), and save them as WebP in `docs/images/`. Update the alt text with what the new screenshot shows.
