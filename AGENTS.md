# Repository Guidelines

## Project Structure & Modules
- `cmd/obsync`: CLI entrypoint (main binary).
- `cmd/lambda`: AWS Lambda bootstrap entry.
- `internal/…`: Core packages (auth, services, storage, lambda, etc.).
- `test/`: `integration/`, `benchmark/`, and `testutil/` helpers.
- `docs/`: Deployment and operational docs.
- `build/`: Generated artifacts (e.g., Lambda zip).

## Build, Test, and Dev Commands
- `make build`: Compile `obsync` with version ldflags from Git.
- `make test`: Run unit tests with race detector and coverage; writes `coverage.out`.
- `make coverage-html`: Generate `coverage.html` from coverage profile.
- `make lint`: Run `golangci-lint` across the repo.
- `make fmt`: Format with `go fmt` and `goimports`.
- `make build-lambda`: Cross-compile ARM64 Lambda bootstrap and zip.
- `make test-lambda`: Run tests for Lambda packages.
- `make dev-setup`: Install local tooling (linters, goimports, gosec).

Examples:
```bash
GOFLAGS=-v make build
make test-integration           # runs ./test/integration with -tags=integration
./obsync --s3 sync <vault-id> --dest ./vault
```

## Coding Style & Naming
- Use `go fmt` and `goimports`; no manual formatting tweaks.
- Package names: lower-case, no underscores (e.g., `storage`, `services`).
- Files: `feature_name.go` where logical; tests as `*_test.go`.
- Exports: Go conventions (`CamelCase`), keep APIs minimal; prefer constructor funcs.
- Logging via internal logger; respect `log.level`/`log.format`.

## Testing Guidelines
- Framework: standard `go test` with `testify` assertions.
- Unit tests live next to code in `internal/.../*_test.go`.
- Integration tests under `test/integration` (run with `make test-integration`).
- Aim to keep `make test` green with race detector; maintain coverage (see `coverage.out`).

## Commit & PR Guidelines
- Commit style: Conventional Commits preferred (`feat:`, `fix:`, `docs:`, `refactor:`); emojis OK but optional.
- Write imperative, scoped subjects; include rationale in body when non-trivial.
- PRs: include description, linked issues, test evidence (outputs or coverage), and docs updates when behavior changes.
- Run `make fmt lint test` before pushing; include config/sample updates if flags or fields change.

## Security & Configuration
- Never commit secrets; use `config.example.json` and environment variables.
- Local state lives under `.obsync/` by default; mind permissions (0600 for secrets).
- For S3/Lambda, ensure `AWS_*` and `S3_*` env vars are set; see `docs/` and `README.md`.
