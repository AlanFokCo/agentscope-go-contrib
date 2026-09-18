# AGENTS.md

Handoff guide for coding agents (and humans) contributing to **agentscope-go**,
the Go implementation of the AgentScope framework. Read this first; then
`CLAUDE.md` for the architecture map and `STABILITY.md` for stability tiers and
what is shipped vs. open.

| Topic | Where |
|---|---|
| Architecture & code conventions | `CLAUDE.md` |
| Stability tiers, production-hardening status | `STABILITY.md` |
| Release notes | `CHANGELOG.md` (Keep a Changelog) |
| Contributing & PR flow | `CONTRIBUTING.md`, `.github/PULL_REQUEST_TEMPLATE.md` |
| Code of conduct | `CODE_OF_CONDUCT.md` |
| Security reports (never a public issue) | `SECURITY.md` → `security@agentscope.io` |
| Deep dives (deployment, edge, tools, …) | `docs/` |

## What this is

- Go port of the Python [AgentScope](https://github.com/agentscope-ai/agentscope)
  multi-agent LLM framework, developed in the `agentscope-ai` community org.
- Module path **`github.com/agentscope-ai/agentscope-go/v2`**; the `/v2` suffix
  is part of every import path. Library under `pkg/agentscope/`, runnable demos
  under `examples/` (54 directories, each its own `main` package).
- `go.mod` declares `go 1.25.0` — keep code **Go 1.25+ compatible**. The CI test
  matrix pins Go 1.25; cross-compile jobs use `go-version-file: go.mod`.
- Licensed Apache-2.0. Source files carry **no** SPDX/license headers by policy;
  do not add any.
- Design parity: when adding a feature that exists upstream, check the Python
  implementation first.

## Build / test / lint / coverage

```bash
make check                       # gofmt -s + go mod tidy + vet + build + test (-race)
go build ./... && go build ./examples/...
go vet ./...
go test -race -count=1 ./...     # what CI runs
golangci-lint run ./...          # v2; go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go test ./pkg/agentscope/tool -run TestName -v   # single test

make cover                       # statement-coverage totals for ./... and ./pkg/...
make cover-check                 # fails if ./pkg/... coverage < COVERAGE_MIN (default 65.0)
```

CI (`.github/workflows/ci.yml`) runs on every push to `main` and every PR: a
3-OS matrix (**ubuntu / macos / windows**) with build, `go vet` and
`go test -race -count=1`; the **coverage gate**, the 30 s fuzz smoke on the two
safety parsers and the loop benchmark run on the ubuntu / Go 1.25 cell only;
plus a linux cross-compile matrix
(arm64/arm/mips64le/riscv64) with an **18 MiB cap** on the stripped
`examples/edge_offline` arm64 binary; and a `golangci-lint` job. Windows
exercises the PowerShell/Cmd code paths, so:

- shell-specific Unix tests guard with
  `if runtime.GOOS == "windows" { t.Skip("requires Unix shell") }`;
- sandbox/workspace-relative paths use the `path` package (forward slash),
  **not** `filepath` (which is `\` on Windows).

### Coverage policy (mandatory)

- CI fails when statement coverage of `./pkg/...` drops below `COVERAGE_MIN`
  (**65.0 %**, set in `.github/workflows/ci.yml` and mirrored as the
  `make cover-check` default — keep the two in sync). The floor is a ratchet:
  raise it in the same PR that raises coverage; a PR that lowers it needs an
  explicit justification and is a blocking finding for reviewers.
- Baseline when the gate was introduced (2026-09, `go test -coverprofile`, no
  `-race`; reproduce with `make cover`): **≈59.6 %** for `./...` and **≈66.6 %**
  for `./pkg/...`. Coverage is attributed per tested package, so the gate runs
  `./pkg/...` separately rather than reusing the `./...` profile or
  `-coverpkg` (which would fold in cross-package attribution and report ≈68.3 %
  instead). `examples/` are demos with no tests by design and are excluded from
  the gate; they must still compile (`go build ./examples/...`).
- Every behavior change ships with a test (TDD: write the failing test, watch it
  fail, then implement). New exported API in a package that currently has no
  tests must add tests for it; the library packages without any test file
  today are `a2a`, `memory`, `realtime`, `session`, `tune`, `types`
  (declarations only) and `messagebus/mqtt` (behind `//go:build mqtt`, hence
  invisible to the default build/test/coverage run).
- PR descriptions state the coverage of touched packages before/after
  (`go tool cover -func=cover.out | grep <pkg>`); the PR checklist enforces it.
- Touching `tool/bash_parser.go` or the safety checks also requires
  `go test -run=x -fuzz=FuzzBashSafety -fuzztime=30s ./pkg/agentscope/tool/`
  (and `FuzzUnmarshalContentBlocks` for `message/` content-block parsing).

## Contributing workflow

Fork the repository, branch from `main` as `feat/…` or `fix/…`, and open a PR
against `main` with `.github/PULL_REQUEST_TEMPLATE.md`; `CONTRIBUTING.md` has
the full flow. Every PR must:

- pass the Quality Gate below and tick the PR checklist (build, vet, test,
  `-race`, lint, coverage, docs);
- declare breaking changes explicitly in the PR description, with a migration
  path (`STABILITY.md` defines which packages may change and how);
- update documentation in the same commit whenever public API or behavior
  changes: `README.md` (including the examples table), `README.es-ES.md` (same
  facts, same commit), `docs/`, the `CLAUDE.md` architecture map, and a
  `CHANGELOG.md` entry under `[Unreleased]`;
- use Conventional-Commit-style subjects, as this repo already does
  (`fix(tool): …`, `docs: …`, `refactor(model)!: …`, `!` marking breaking).

Commit messages must not mention AI assistants and must not carry
`Co-Authored-By` trailers for them.

Dependencies: `vendor/` is `.gitignore`d and never committed. Add a dependency
with `go get <pkg> && go mod tidy` and commit **only `go.mod` + `go.sum`**; CI
restores deps from the module proxy. Prefer Apache-2.0/MIT/BSD-compatible
licenses and call out every new dependency in the PR description.

## Releases, versions, security

- Tags are cut from `main` and follow semver on the `/v2` module line; each tag
  promotes the `[Unreleased]` section of `CHANGELOG.md` into a version section.
- Supported versions and the security process live in `SECURITY.md`: report
  vulnerabilities privately to `security@agentscope.io`, never as a public issue.
- Docs must not hard-code the release tag (use
  `git describe --tags --abbrev=0` when you need it); the only places that name
  a tag are the `SECURITY.md` supported-versions table and the `STABILITY.md`
  versioning note, which are refreshed when a tag is cut.

## Code conventions (summary; full list in `CLAUDE.md`)

- `context.Context` first arg; return `(T, error)` — don't panic (exceptions:
  `message.NewMsg`, `agent.NewUnifiedAgent` panic on programmer error).
- Interfaces + embeddable `BaseXxx` defaults; functional options
  (`opts ...XxxOption`).
- Streaming = `<-chan T`: deltas then a final `IsLast=true`, `defer close(ch)`;
  sends must be ctx-aware.
- Errors: structured `errors.AgentError` + sentinels (`errors.Is`/`As` via
  `AgentError.Is()` matching by `Code`); `IsRetryableError` honors the typed
  retryable flag; `AgentMessage()` for LLM-facing messages.
- Log through `agentscope.Log()`; `internal/httpx` already logs transport
  retries — don't double-log around it.

## Quality Gate: evaluator adversarial review (MANDATORY)

Before any commit and push, the following MUST be verified through an evaluator
(adversarial reviewer):

1. **Code changes** — review all new/modified code for logic bugs, race
   conditions, nil panics, missing edge cases and error handling, API misuse or
   design flaws, and security vulnerabilities.
2. **Documentation changes** — verify every code example compiles against the
   real source, every API reference (names, signatures, struct fields) matches
   reality, every numeric claim (counts, versions, coverage) is accurate, and
   no link or reference is stale.
3. **Commit criteria** — a commit is allowed ONLY when:
   - `go build ./...` and `go build ./examples/...` pass
   - `go vet ./...` passes
   - `go test -race -count=1 ./...` passes (or the affected packages)
   - `golangci-lint run ./...` shows 0 issues
   - `make cover-check` passes and touched packages lost no coverage
   - the fuzz smoke was re-run when safety parsers changed
   - the evaluator returns PASS (no HIGH-severity findings)

Skipping the evaluator review is NOT acceptable. If time is constrained, at
minimum run the evaluator on the specific packages modified.
