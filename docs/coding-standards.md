# Coding Standards

These rules are mandatory for all Go code in this repository. They exist to keep
the codebase readable, testable, and free of accidental complexity. When a change
cannot follow a rule, document the reason in the code or the pull request.

## Architecture rules

- Keep `types` dependency-free.
- Keep `core` focused on the public runtime contracts and registries — the thin,
  dependency-light surface connectors, processors, and the runtime build against.
- Keep the pipeline implementation (flow builder, composites, setters, worker pool)
  under `core/internal/*` so it stays un-importable across module boundaries.
- Keep orchestration and config loading in `core/runtime` (the application layer
  that wires connectors and flows into a running service).
- Keep connectors and processors isolated and self-registering.
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
- Keep functions short enough to read without scrolling. `funlen` in
  `runtime/.golangci.yml` draws the line at **60 lines / 45 statements** — cited
  rather than restated, so there is one number with one owner.

## File size and organization

**A split that leaves the code harder to follow is worse than the file being
long.** That governs everything below, and it is why the Go side gives no line
count: nothing enforces one, 38 of 355 files exceed the number that used to be
written here, and a rule the repository does not hold teaches you to read the
rest as aspirational too.

- No monstrous files. Split a file when it starts covering unrelated
  responsibilities — not when it hits a length.
- One primary type or concern per file where practical. File names should describe
  their contents.
- Keep packages small and purpose-driven.

## `init()` — exactly one per loadable module

`init()` has one sanctioned use in this codebase: a loadable module registers
itself, so a blank import in `runtime/octo/main.go` (or a build tag) decides what
the binary ships. See [extension-points.md](extension-points.md).

That contract is per **module**, so the rule is per module too:

- **Exactly one `func init()` per loadable module**, in the module's root file —
  the file carrying the package doc, usually named after the package. It registers
  everything the module contributes.
- **Per-block registration bodies stay beside their blocks**, as ordinary named
  `registerXxx()` functions called from that one `init()`. They keep owning their
  own metadata; they just stop being side effects.
- The `init()` is the module's **manifest**: the one place that says what importing
  this package puts into the runtime, in a deterministic order rather than in
  filename order. A new block is then a visible edit to a file a reviewer reads.
- Keep registration calls **in the module's own package directory**.
  `RegisterBlockMeta` / `RegisterConnectorMeta` / `RegisterExtension` bind metadata
  to the caller's source directory (`callerDir()` in `runtime/core/schema_meta.go`).
  Naming a function is safe; hoisting a shared registration helper into another
  package silently reattaches every palette group and icon default to the helper's
  directory.
- Do not use `init()` for anything else — no configuration, no globals that could
  be built lazily, no ordering games between files.

`gochecknoinits` enforces this. It is enabled in
[../runtime/.golangci.yml](../runtime/.golangci.yml) with an exclusion allowlist
naming the one permitted file per module; that allowlist *is* the enumeration of
loadable modules. Adding a module means adding an entry there. A second `init()`
inside a module fails CI rather than passing as a convention. Test files are
exempt: fixtures that register throwaway blocks ship in nothing.

The allowlist carries exactly one entry that is not a module manifest, and it is
there because the language leaves no alternative:
`runtime/octo/hosted_observability.go` stamps the version into the observability
service, which cannot import package `main` back to read it. Prefer wiring like
this at a call site; reach for `init()` only when an import cycle makes that
impossible, and say so in a comment on the `init()` and on its allowlist entry.

## Package organization

- Prefer small, cohesive packages over one large catch-all package. Each package
  should have a single, nameable responsibility; if you struggle to summarize a
  package in one sentence, it is doing too much and should be split.
- Make the public API deliberate and minimal. Export only what an out-of-package
  consumer (another module, a connector, a processor, the CLI) actually needs.
  Everything else is implementation detail.
- Put implementation that has no external consumer under an `internal/` package so
  it cannot be imported across module boundaries. The compiler then enforces the
  public/internal boundary instead of relying on convention.
- Keep the contract layer (shared interfaces and types) dependency-light and free of
  the implementations that depend on it, so it can be imported without dragging the
  engine along and without creating import cycles.
- Split a package the moment it mixes unrelated concerns or grows past what a reader
  can hold in their head — do not wait for a line count.

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

## Frontend (TypeScript / React)

See [editor-coding-standards.md](editor-coding-standards.md). The TypeScript rules
lived here as well for a while, in different words, which is two places to change
and two places to disagree.
