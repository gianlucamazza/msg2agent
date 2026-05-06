# Contributing

## Dev Setup

```bash
go build ./...
go test ./... -race
```

Requires Go 1.24+. No other runtime dependencies are needed for the core packages.

## Workflow

1. Fork the repository and create a branch from `main`.
2. Make your changes with tests.
3. Open a pull request against `main`.

## Branch Naming

| Prefix    | When to use                          |
| --------- | ------------------------------------ |
| `feat/`   | New feature                          |
| `fix/`    | Bug fix                              |
| `chore/`  | Tooling, deps, CI, docs              |

## Commit Convention

This project follows [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add offline queue persistence
fix: correct DID resolution timeout
docs: update billing-setup guide
chore: bump stripe-go to v82.5.1
test: add fuzz tests for message parser
```

## Tests

- Write unit tests for all new code.
- The race detector must pass: `go test ./... -race`.
- Integration tests live in `test/`; run them with `go test ./test/... -tags integration`.

## Pull Requests

- Reference the plan item in the PR description (e.g., "implements B3").
- Keep PRs focused — one concern per PR.
- At least 1 approval is required before merging.
- Squash or rebase before merging to keep history clean.

## Reporting Issues

Use GitHub Issues. For security vulnerabilities, follow [SECURITY.md](SECURITY.md) instead.
