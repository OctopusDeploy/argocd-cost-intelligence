# Contributing

Thanks for your interest in ArgoCD Cost Intelligence. Bug reports, fixes, docs and features are all welcome.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md). To report a security problem, see [SECURITY.md](SECURITY.md) instead of opening an issue.

## Before you start

- **Bugs and small fixes:** open a pull request directly.
- **Features and larger changes:** open an issue first so we can agree on the approach before you invest time.
- **Questions:** use GitHub Discussions rather than issues.

## Development setup

[docs/development.md](docs/development.md) covers the tools you need, running the project locally, the checks a change must pass and trying it in a real Argo CD. [docs/architecture.md](docs/architecture.md) describes the design; its [design decisions](docs/architecture.md#design-decisions) and [authorization](docs/architecture.md#authorization) sections hold the rules that are easiest to break. The Go conventions are in [docs/development.md](docs/development.md#go-conventions), and the UI plugin's in [ui/README.md](ui/README.md#conventions).

## Making a change

1. Fork the repository and create a branch from `main`.
2. Make your change, with tests (see [Writing tests](#writing-tests)). A bug fix should include a test that fails without the fix.
3. Run the checks in [Checking a change](docs/development.md#checking-a-change). A change is ready when `make test`, `make lint` and the four `npm` commands pass.
4. Update documentation and `CHANGELOG.md` (under `[Unreleased]`) for anything a user would notice. Flag breaking changes under **Breaking**.
5. Open a pull request that explains what changed and why, and links the issue it addresses.

### Things to keep in mind

- **Costs.** Never multiply an OpenCost cost by a fixed 30, and never mix namespace-scoped and application-scoped figures in one comparison.
- **Authorization.** App routes look data up by the `AppRef` from the request context, not by the URL. New routes must go in the group matching their scope.
- **UI.** The plugin runs on the React that Argo CD provides (16.14 on Argo CD 3.4), so use only APIs React 16.14 has. Take colours from the theme tokens, never hardcode them.

### Writing tests

Check that a new test can fail: break the behaviour, watch the test fail, then restore it.

**Go**

- Use table-driven tests, and `t.Parallel()` when the cases share no state.
- Put the test in the same package as the code.
- Unit tests use no network and no database. Fake OpenCost with `httptest` and storage with an in-memory fake (see `pkg/history/fakes_test.go`). The storage and OpenCost client code has integration tests for the real thing.
- Authorization changes are tested in `pkg/server/auth_test.go` and `app_scope_test.go`. For a new app route, test that a header naming another application is refused.

**UI**

- Test behaviour and logic. Test styling only where it guards accessibility.
- jest-dom is not installed: assert with `getAttribute` and `textContent`.
- jsdom has no `localStorage`: stub it (see `useTheme.test.tsx`).
- Call `vi.useFakeTimers()` before rendering anything that schedules a timer, and advance with `advanceTimersByTimeAsync`; `waitFor` hangs under fake timers.
- Test a layout that depends on width by stubbing `HTMLElement.prototype.getBoundingClientRect` (see `StatStrip.test.tsx`), and restore the mocks in `afterEach`.

### Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): start the subject with a type such as `feat`, `fix`, `docs`, `ci` or `deps`, with an optional scope (`feat(ui): ...`), and write the rest in the imperative mood ("fix: fill the daily history gap", not "fixed"). Keep it under about 72 characters, and explain the reason in the body when it is not obvious.

### Sign-off (DCO)

This project uses the [Developer Certificate of Origin](https://developercertificate.org/). Certify that you wrote the change, or have the right to submit it under the project's licence, by signing off each commit:

```bash
git commit -s -m "fix: fill the daily history gap"
```

This adds a `Signed-off-by: Your Name <you@example.com>` line. If you forgot, run `git commit --amend -s` or `git rebase --signoff main`.

## Licence

Contributions are licensed under the [Apache License 2.0](LICENSE), the same as the project.
