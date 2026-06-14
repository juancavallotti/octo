# Linting Policy

## Rules

- Lint failures block merges and releases.
- No disabled warnings without a documented reason.
- New code must not introduce avoidable complexity or dead code.
- Formatting must be handled by `gofmt` or `go fmt`.

## Expectations

- Add or update tests when behavior changes.
- Keep exported identifiers documented when they are part of the public surface.
- Keep lint configuration checked into the repository.
