# Linting Policy

## Rules

- Lint failures block merges and releases.
- No disabled warnings without a documented reason.
- New code must not introduce avoidable complexity or dead code.
- Formatting must be handled by `gofmt` or `go fmt`.

## Expectations

- Keep exported identifiers documented when they are part of the public surface.
- Keep lint configuration checked into the repository.

## Which linter set runs where

`runtime` has its own `.golangci.yml` and runs a strict set (`funlen`, `cyclop`,
`wrapcheck`, `mnd`, `lll`, `ireturn`, and a `gochecknoinits` allowlist that
enumerates the loadable modules).

`orchestrator`, `logs` and `sidecars` have no config and run golangci's defaults.
That is a deliberate deferral, not an oversight: the strict set turns the service
modules' current state into hundreds of findings, and a backlog that large gets
skimmed past exactly the way the one it replaced did. Adopting it there is its own
piece of work, to be done module by module with the findings actually addressed.

Every module is linted in CI, and CI fails on any finding. The value is in staying
at zero — a permanently non-empty lint output trains everyone to skim it, which is
precisely when the next real finding goes unnoticed.
