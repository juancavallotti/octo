# Repository Guidance

Before making code changes, read the files under `docs/` and follow them as the source of truth for coding standards, linting, review policy, and release expectations.

The active Go workspace lives under `runtime/`.

Required reading:

- [docs/coding-standards.md](docs/coding-standards.md)
- [docs/linting-policy.md](docs/linting-policy.md)
- [docs/commit-and-review-policy.md](docs/commit-and-review-policy.md)
- [docs/release-process.md](docs/release-process.md)

## Workflow rules (always apply)

- Break every implementation plan down into a sequence of small, logical commits.
- **Always stop before committing.** Do not run `git commit` or `git push` until the
  human has reviewed the staged increment and explicitly approved it. Present each
  increment (what changed, why, test coverage) and wait. This applies even to trivial
  changes. See [docs/commit-and-review-policy.md](docs/commit-and-review-policy.md).
- Use Conventional Commit messages — release automation depends on them.

The initial baseline is expected to be committed directly, not through a pull request.

## Refactoring policy

This project prefers **complete refactors over backwards compatibility.** When a change
improves the design, update every call site, test, and document in the same change rather
than introducing compatibility shims, deprecated aliases, or dual code paths. There is no
external API stability guarantee yet: prefer one clean, fully-migrated implementation
over preserving old behavior alongside the new.
