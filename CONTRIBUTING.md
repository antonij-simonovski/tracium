# Contributing to Tracium

Thanks for helping. Tracium is Apache 2.0 licensed.

## Repo layout

One repository, five components: `collector/` and `api/` (Go), `dashboard/`
(React/TS), `spec/` (schemas + codegen), `deploy/` (Helm, migrations, infra).
Each has a `README.md` documenting its conventions. Read the relevant one before
changing that component.

## Build & test

```bash
make test            # go tests (collector, api) + dashboard tests
make smoke           # isolated end-to-end Docker check (Python 3 required)
make up              # run the full stack from source
make build-collector # assemble the collector via OCB
```

The Go modules are wired with `go.work`, so cross-module builds resolve locally
with no version pins.

## Ground rules (from the design docs)

- **Program against interfaces**, wire concrete types in one place (`main.go` /
  provider setup).
- **Typed errors** from `internal/errors`, never bare `fmt.Errorf`/`new Error` out
  of a pipeline stage or repository.
- **`spec/` is the source of truth.** Change a shared type there first, then
  regenerate consumers; never hand-edit generated files.
- **Every read must be time- or trace-bounded.** Nothing may scan the span table
  unbounded; cost scales with the query window, not total rows stored.

## Pull requests

Keep PRs focused. Include tests (unit tests use mocks, never a real DB). CI runs
the affected component's tests and checks that generated code is in sync.

## Conduct and reporting

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md). Report
bugs and request features through the issue templates; report vulnerabilities
privately per [SECURITY.md](SECURITY.md).
