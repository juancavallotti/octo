# Coding Standards

These rules are mandatory for all Go code in this repository. They exist to keep
the codebase readable, testable, and free of accidental complexity. When a change
cannot follow a rule, document the reason in the code or the pull request.

## Architecture rules

- Keep `types` dependency-free.
- Keep `core` focused on runtime contracts, orchestration, and config loading.
- Keep connectors isolated and self-registering.
- Prefer explicit dependencies over global state, but allow registry bootstrapping at startup.
- Use `context.Context` for all long-running operations.
- Return wrapped errors with enough context to diagnose failures.

## Interfaces

- Do not pollute the codebase with interfaces. Define an interface only when there
  is a real need: more than one implementation, a test boundary that cannot be met
  otherwise, or a published extension point.
- Accept interfaces, return concrete types. Let the consumer declare the interface
  it needs rather than the producer exporting one speculatively.
- Keep interfaces small — ideally one to three methods. Large interfaces are a sign
  the abstraction is doing too much.
- Never add an interface "just in case" or to mirror a struct one-to-one.

## Constants and magic numbers

- No magic numbers or magic strings in logic. Any literal with meaning must be a
  named constant. Loop bounds of `0`/`1` and obvious identity values are the only
  exceptions.
- Declare constants (and package-level `var` defaults) at the **top of the file**,
  grouped in a `const (...)` block, before the types and functions that use them.
- Give constants names that explain intent, not value (`defaultTimeout`, not `thirtySeconds`).

## Functions and clarity

- Break logic into small, focused functions. Each function should do one thing and
  have a name that describes that thing.
- Extract a helper when a block of code needs a comment to explain what it does —
  the function name should carry that explanation instead.
- Keep nesting shallow. Prefer early returns (guard clauses) over deep `if`/`else`
  pyramids.
- Keep functions short enough to read without scrolling. If a function spans more
  than roughly 50 lines, look for a natural split.

## File size and organization

- No monstrous files. Keep files focused on a single concern; split a file once it
  grows past roughly 300–400 lines or starts covering unrelated responsibilities.
- One primary type or concern per file where practical. File names should describe
  their contents.
- Keep packages small and purpose-driven.

## Testing

- Build tests as you go, not at the end. When planning work, include the tests for
  each behavior in the same step (and the same commit) as that behavior.
- Add or update tests whenever behavior changes.
- Prefer table-driven tests.
- Test behavior and edge cases, not implementation details.

## General Go style

- Avoid unnecessary abstractions.
- Use short, descriptive names.
- Do not add new dependencies unless they solve a real problem.
- Keep exported identifiers documented when they are part of the public surface.
- Formatting is handled by `gofmt` / `go fmt`; do not hand-format.
- Standardize structured logging on the Go standard library `log/slog` package.
