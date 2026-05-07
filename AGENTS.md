# Repository Guidelines

## Project Structure & Module Organization

`cmd/` contains runnable binaries: `relay`, `mcp-server`, `billing-admin`, and `dashboard`. Reusable Go packages live in `pkg/`, including messaging, transport, registry, billing, OAuth, MCP, telemetry, and crypto. Protocol adapters are in `adapters/a2a` and `adapters/mcp`. Integration and E2E tests live in `test/`, with fixtures in `testdata/`. Browser assets are colocated with their serving command under `cmd/*/web`; deployment manifests, Grafana dashboards, and Compose variants live in `infrastructure/` and `docker-compose*.yml`. Use `scripts/` for bootstrap, smoke, deployment, and scenario helpers.

## Build, Test, and Development Commands

- `make build` builds all binaries into `build/`.
- `make test-unit` runs race-enabled unit tests for `pkg/...` and `cmd/...`.
- `make test-integration` runs tagged integration tests under `test/`.
- `make test-e2e` builds binaries and runs tagged E2E tests.
- `make lint`, `make vet`, and `make check` run quality checks; `make check` also formats with `gofmt -w -s`.
- `make dev-up`, `make dev-logs`, and `make dev-down` manage the local Docker Compose stack.
- `make smoke` runs the post-deploy smoke test using `RELAY_URL`, `MCP_URL`, and `API_KEY`.

## Coding Style & Naming Conventions

Use idiomatic Go formatted with `gofmt -s`. Keep package names short and lowercase. Name tests `*_test.go`; prefer table-driven tests for protocol, billing, transport, and validation behavior. Keep JavaScript and CSS assets beside the command that serves them.

## Testing Guidelines

Add unit tests with new Go behavior. Before opening a PR, run `go test -v -race ./pkg/... ./cmd/...` or `make test-unit`. Use build tags for broader suites: `integration`, `e2e`, `security`, and `integration_pg`. Run `make test-coverage` when changing shared packages or security-sensitive paths.

## Commit & Pull Request Guidelines

Follow Conventional Commits, matching existing history: `feat(dashboard): ...`, `fix(transport): ...`, `test(mcp-server): ...`, `chore: ...`. Keep PRs focused, describe behavior changes, link related issues or plan items, and include screenshots for dashboard or web asset changes.

## Security & Configuration Tips

Do not commit secrets, API keys, OAuth credentials, Stripe keys, local databases, or generated certificates. Prefer documented `MSG2AGENT_*` environment variables, and review `SECURITY.md` plus `docs/operations/configuration.md` when touching auth, billing, TLS, or DID verification.
