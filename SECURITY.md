# Security Policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through GitHub: open the repository's Security tab and choose Report a vulnerability. If that isn't available, email pat.papapetrou@octopus.com with "SECURITY" in the subject.

Include what you found, the affected version, and steps to reproduce. You can expect an acknowledgement within 5 working days. We will keep you informed while we work on a fix and credit you in the release notes unless you prefer not to be named.

## Supported versions

Security fixes go into the latest release. While the project is below 1.0, older minor versions are not patched.

## Trust model

The service relies on Argo CD for authentication. Knowing what it does and doesn't defend against helps you judge a report and deploy it safely.

- Application data is scoped by the `Argocd-Application-Name` header, which Argo CD's extension proxy sets after checking the viewer's RBAC. The proxy doesn't check the request path, so the service requires `{appName}` in the path to match the header, and it looks data up by the header's namespace and name.
- Cluster-wide data (namespaces, chargeback, cluster overhead) is only available to requests that name the Application set in `CLUSTER_SCOPE_APP`. It's off when that's unset.
- `POST /api/cost-analysis/on-sync` is authenticated with a bearer token (`WEBHOOK_SECRET`, at least 16 characters), compared in constant time.
- The headers can only be trusted if nothing but `argocd-server` and the notifications controller can reach the service. The Helm chart installs NetworkPolicies for this, but they only take effect on a CNI that enforces NetworkPolicy. If you deploy without the chart, restrict access yourself.
- Prometheus metrics (`/metrics` on the exporter) carry every Application's cost, and Argo CD RBAC doesn't cover them. Anyone who can query your Prometheus can read them. See the Security section of [docs/metrics.md](docs/metrics.md#security).
- The service holds no cloud credentials. It reads OpenCost and, optionally, `VerticalPodAutoscaler` objects (`get` and `list` only).

Reports about the following are in scope: bypassing the application or cluster scoping, reading another application's data, an unauthenticated `/on-sync`, injection, and secrets exposed by the chart or manifests. Issues that require access to the database, the cluster, or the webhook secret are out of scope.

Security-related changes are listed under Security in the [changelog](CHANGELOG.md).
