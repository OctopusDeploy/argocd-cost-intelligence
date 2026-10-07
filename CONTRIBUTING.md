# Contributing

Thanks for your interest in ArgoCD Cost Intelligence. Bug reports, fixes, docs and features are all welcome.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). To report a security problem, see [SECURITY.md](SECURITY.md) instead of opening an issue.

## Before you start

- **Bugs and small fixes:** open a pull request directly.
- **Features and larger changes:** open an issue first so we can agree on the approach before you invest time.
- **Questions:** use GitHub Discussions rather than issues.

## Development setup

You need Go (the version in `go.mod`), Node.js 22, and Docker.

```bash
git clone https://github.com/ppapapetrou76/argocd-cost-intelligence.git
cd argocd-cost-intelligence

docker-compose up -d    # service on :8080, PostgreSQL, and a mock OpenCost on :9003
make build              # backend binary in bin/
make test               # Go unit tests (make test-integration needs Docker; see docs/development.md)
make lint               # every linter: Go, UI, Helm, Prometheus rules, workflows, manifests, Dockerfiles, scripts
make lint-go            # one of them; `make help` lists the rest

cd ui
npm ci
npm run lint && npm run typecheck && npm test && npm run build
```

[docs/development.md](docs/development.md) explains each test level (unit, integration, Docker Compose, local Kubernetes) and how to run it, and [docs/architecture.md](docs/architecture.md) describes the design. [CLAUDE.md](CLAUDE.md) lists the rules that are easiest to break (cost scoping, authorization, UI constraints). It's written for AI assistants, but it's worth a read even if you don't use one.

## Making a change

1. Fork the repository and create a branch from `main`.
2. Make your change, with tests. A bug fix should include a test that fails without the fix.
3. Run the checks above. A change is ready when `make test`, `make lint` and the four `npm` commands pass.
4. Update documentation and `CHANGELOG.md` (under `[Unreleased]`) for anything a user would notice. Flag breaking changes under **Breaking**.
5. Open a pull request that explains what changed and why, and links the issue it addresses.

### Things to keep in mind

- **Costs.** Never multiply an OpenCost cost by a fixed 30, and never mix namespace-scoped and application-scoped figures in one comparison.
- **Authorization.** App routes look data up by the `AppRef` from the request context, not by the URL. New routes must go in the group matching their scope.
- **UI.** The plugin runs on the React that Argo CD provides (16.14 on Argo CD 3.4), so use only APIs React 16.14 has. Take colours from the theme tokens, never hardcode them.

### Commit messages

Write the subject in the imperative mood ("Fix daily history gap", not "Fixed"), keep it under about 72 characters, and explain the reason in the body when it is not obvious.

### Sign-off (DCO)

This project uses the [Developer Certificate of Origin](https://developercertificate.org/). Certify that you wrote the change, or have the right to submit it under the project's licence, by signing off each commit:

```bash
git commit -s -m "Fix daily history gap"
```

This adds a `Signed-off-by: Your Name <you@example.com>` line. If you forgot, run `git commit --amend -s` or `git rebase --signoff main`.

## Licence

Contributions are licensed under the [Apache License 2.0](LICENSE), the same as the project.
